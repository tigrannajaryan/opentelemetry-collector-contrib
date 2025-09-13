// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package stefexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stefexporter"

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	stefpdatametrics "github.com/splunk/stef/go/pdata/metrics"
	stefpkg "github.com/splunk/stef/go/pkg"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stefexporter/internal"
)

// stefExporter implements sending metrics over STEF/gRPC stream.
//
// The exporter uses a single stream and accepts concurrent exportMetrics calls,
// sequencing the metric data as needed over a single stream.
//
// The exporter will block exportMetrics call until an acknowledgement is
// received from destination.
//
// The exporter relies on a preceding Retry helper to retry sending data that is
// not acknowledged or otherwise fails to be sent. The exporter will not retry
// sending the data itself.
type stefExporter struct {
	set         component.TelemetrySettings
	cfg         Config
	compression stefpkg.Compression
	started     bool

	grpcConn *grpc.ClientConn

	connMan    *internal.ConnManager
	sync2Async *internal.Sync2Async

	exportCount atomic.Uint64
}

const (
	flushPeriod = 100 * time.Millisecond
)

// TODO: make connection count configurable.
const connCount = 1

func newStefExporter(set component.TelemetrySettings, cfg *Config) *stefExporter {
	exp := &stefExporter{
		set: set,
		cfg: *cfg,
	}

	exp.compression = stefpkg.CompressionNone
	if exp.cfg.Compression == "zstd" {
		exp.compression = stefpkg.CompressionZstd
	}
	// Disable built-in grpc compression. STEF has its own zstd compression support.
	exp.cfg.Compression = ""
	return exp
}

func (s *stefExporter) Start(ctx context.Context, host component.Host) error {
	// Prepare gRPC connection.
	var err error
	s.grpcConn, err = s.cfg.ToClientConn(ctx, host, s.set)
	if err != nil {
		return err
	}

	// Create a connection creator and manager to take care of the connections.
	connCreator := internal.NewStefConnCreator(s.set.Logger, s.grpcConn, s.compression)
	set := internal.ConnManagerSettings{
		Logger:          s.set.Logger,
		Creator:         connCreator,
		TargetConnCount: connCount,
		FlushPeriod:     flushPeriod,
		ReconnectPeriod: s.cfg.ReconnectPeriod,
	}
	s.connMan, err = internal.NewConnManager(set)
	if err != nil {
		return err
	}

	s.connMan.Start()

	// Wrap async implementation of sendMetricsAsync into a sync-callable API.
	s.sync2Async = internal.NewSync2Async(s.set.Logger, s.cfg.QueueConfig.NumConsumers, s.sendMetricsAsync)

	// Begin connection attempt in a goroutine to avoid blocking Start().
	go func() {
		// Acquire() triggers a connection attempt and blocks until it succeeds or fails.
		conn, err := s.connMan.Acquire(ctx)
		if err != nil {
			s.set.Logger.Error("Error connecting to destination", zap.Error(err))
			// This is not a fatal error. Next sending attempt will try to
			// connect again as needed.
			return
		}
		// Connection is established. Return it, this is all we needed for now.
		s.connMan.Release(ctx, conn)
	}()

	s.started = true
	return nil
}

func (s *stefExporter) Shutdown(ctx context.Context) error {
	if !s.started {
		return nil
	}

	if err := s.connMan.Stop(ctx); err != nil {
		s.set.Logger.Error("Failed to stop connection manager", zap.Error(err))
	}

	if s.grpcConn != nil {
		if err := s.grpcConn.Close(); err != nil {
			s.set.Logger.Error("Failed to close grpc connection", zap.Error(err))
		}
		s.grpcConn = nil
	}

	s.started = false
	return nil
}

func getBatchRange(md pmetric.Metrics) string {
	resourceMetrics := md.ResourceMetrics()
	if resourceMetrics.Len() == 0 {
		return "unknown"
	}
	scopeMetrics := resourceMetrics.At(0).ScopeMetrics()
	if scopeMetrics.Len() == 0 {
		return "unknown"
	}
	metrics := scopeMetrics.At(0).Metrics()
	if metrics.Len() == 0 {
		return "unknown"
	}
	metric := metrics.At(0)
	if metric.Type() != pmetric.MetricTypeGauge {
		return "unknown"
	}
	dataPoints := metric.Gauge().DataPoints()
	if dataPoints.Len() == 0 {
		return "unknown"
	}
	startIndex, exists := dataPoints.At(0).Attributes().Get("batch_index")
	if !exists {
		return "unknown"
	}

	scopeMetrics = resourceMetrics.At(resourceMetrics.Len() - 1).ScopeMetrics()
	if scopeMetrics.Len() == 0 {
		return "unknown"
	}
	metrics = scopeMetrics.At(scopeMetrics.Len() - 1).Metrics()
	if metrics.Len() == 0 {
		return "unknown"
	}
	metric = metrics.At(metrics.Len() - 1)
	if metric.Type() != pmetric.MetricTypeGauge {
		return "unknown"
	}
	dataPoints = metric.Gauge().DataPoints()
	if dataPoints.Len() == 0 {
		return "unknown"
	}
	lastIndex, exists := dataPoints.At(dataPoints.Len() - 1).Attributes().Get("batch_index")
	if !exists {
		return "unknown"
	}
	return fmt.Sprintf("%s:%s", startIndex.Str(), lastIndex.Str())
}

func (s *stefExporter) exportMetrics(ctx context.Context, data pmetric.Metrics) error {
	exportCount := s.exportCount.Add(1)
	batchRange := getBatchRange(data)

	s.set.Logger.Debug(
		fmt.Sprintf(
			"exportMetrics start,  index=%d, range=%s", exportCount, batchRange,
		),
	)

	err := s.sync2Async.DoSync(ctx, data)

	var errStr string
	if err != nil {
		errStr = err.Error()
	}
	s.set.Logger.Debug(
		fmt.Sprintf(
			"exportMetrics finish, index=%d, range=%s, err=%s", exportCount, batchRange, errStr,
		),
	)
	return err
}

// sendMetricsAsync is an async implementation of sending metric data.
// The result of sending will be reported via resultChan.
func (s *stefExporter) sendMetricsAsync(
	ctx context.Context,
	data any,
	resultChan internal.ResultChan,
) (internal.DataID, error) {
	md := data.(pmetric.Metrics)

	batchRange := getBatchRange(md)

	s.set.Logger.Debug("Preparing to send metrics async, range=" + batchRange)

	// Acquire a connection to send the data over.
	conn, err := s.connMan.Acquire(ctx)
	if err != nil {
		return 0, err
	}

	s.set.Logger.Debug("Connection acquired, range=" + batchRange)

	// It must be a StefConn with a Writer.
	stefConn := conn.Conn().(*internal.StefConn)
	stefWriter := stefConn.Writer()

	// Convert and write the data to the Writer.
	converter := stefpdatametrics.OtlpToSTEFUnsorted{}
	err = converter.WriteMetrics(md, stefWriter)
	if err != nil {
		s.set.Logger.Debug("WriteMetrics failed", zap.Error(err))

		// Error to write to STEF stream typically indicates either:
		// 1) A problem with the connection. We need to reconnect.
		// 2) Encoding failure, possibly due to encoder bug. In this case
		//    we need to reconnect too, to make sure encoders start from
		//    initial state, which is our best chance to succeed next time.
		//
		// We need to reconnect. Disconnect here and the next exportMetrics()
		// call will connect again.
		s.connMan.DiscardAndClose(conn)

		// TODO: check if err is because STEF encoding failed. If so we must not
		// try to re-encode the same data. Return consumererror.NewPermanent(err)
		// to the caller. This requires changes in STEF Go library.

		// Return an error to retry sending these metrics again next time.
		return 0, err
	}

	// According to STEF gRPC spec the destination ack IDs match written record number.
	// When the data we have just written is received by destination it will send us
	// back an ack ID that numerically matches the last written record number.
	expectedAckID := stefWriter.RecordCount()

	s.set.Logger.Debug(
		fmt.Sprintf(
			"Metrics are written to connection. range=%s, expectedAckID=%v", batchRange, expectedAckID,
		),
	)

	// Register to be notified via resultChan when the ack of the
	// written record is received.
	stefConn.OnAck(expectedAckID, resultChan)

	// We are done with the connection.
	s.connMan.Release(ctx, conn)

	return internal.DataID(expectedAckID), nil
}
