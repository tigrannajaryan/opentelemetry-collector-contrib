// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package stefexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stefexporter"

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	stefgrpc "github.com/splunk/stef/go/grpc"
	"github.com/splunk/stef/go/grpc/stef_proto"
	"github.com/splunk/stef/go/otel/oteltef"
	stefpdatametrics "github.com/splunk/stef/go/pdata/metrics"
	"github.com/splunk/stef/go/pkg"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stefexporter/internal"
)

// stefExporter implements sending metrics over STEF/gRPC stream.
// The exporter uses a single stream and accepts concurrent exportMetrics calls,
// sequencing the metric data as needed over a single stream.
// The exporter will block exportMetrics call until an acknowledgement is
// received from destination. To achieve high throughput, appropriate level of
// concurrency is necessary, so make sure to configure the exporter helper Queue
// with sufficiently high number of consumers.
// The exporter relies on a preceding Retry helper to retry sending data that is
// not acknowledged or otherwise fails to be sent. The exporter will not retry
// sending the data itself.
type stefExporter struct {
	set         component.TelemetrySettings
	host        component.Host
	logger      *zap.Logger
	cfg         *Config
	compression pkg.Compression

	// How long to wait for ack to be received from server. Configurable for testing purposes.
	maxAckWaitTime time.Duration

	connector *internal.Connector
	grpcConn  *grpc.ClientConn

	// The STEF writer we write metrics to and which in turns sends them over gRPC.
	stefWriter      atomic.Pointer[oteltef.MetricsWriter]
	stefWriterMutex sync.Mutex // protects stefWriter

	// Channel to stop flusher() goroutine.
	shutdownSignal chan struct{}

	sync2async *internal.Sync2Async

	ackDataMutex sync.Mutex
	pendingAcks  []pendingAck
	lastAckedID  uint64

	jobs sync.WaitGroup
}

type pendingAck struct {
	dataID internal.DataID
	ch     internal.AckChan
}

type loggerWrapper struct {
	logger *zap.Logger
}

func (w *loggerWrapper) Debugf(_ context.Context, format string, v ...any) {
	w.logger.Debug(fmt.Sprintf(format, v...))
}

func (w *loggerWrapper) Errorf(_ context.Context, format string, v ...any) {
	w.logger.Error(fmt.Sprintf(format, v...))
}

func newStefExporter(set component.TelemetrySettings, cfg *Config) *stefExporter {
	exp := &stefExporter{
		set:            set,
		logger:         set.Logger,
		cfg:            cfg,
		maxAckWaitTime: cfg.maxAckWaitTime,
	}
	exp.sync2async = internal.NewSync2Async(set.Logger, cfg.NumConsumers, exp.asyncExportMetrics)
	exp.connector = internal.NewConnector(set.Logger, exp.connect, exp.disconnect)

	exp.compression = pkg.CompressionNone
	if cfg.Compression == "zstd" {
		exp.compression = pkg.CompressionZstd
	}
	return exp
}

func (s *stefExporter) Start(ctx context.Context, host component.Host) error {
	s.host = host
	s.shutdownSignal = make(chan struct{})

	if err := s.connector.Start(ctx); err != nil {
		return err
	}

	s.jobs.Add(1)
	go s.flusher()

	return nil
}

func (s *stefExporter) Shutdown(ctx context.Context) error {
	close(s.shutdownSignal)

	if err := s.connector.Shutdown(ctx); err != nil {
		return err
	}

	s.jobs.Wait()
	return nil
}

func (s *stefExporter) connect(ctx context.Context) error {
	s.logger.Debug("Connecting to destination", zap.String("endpoint", s.cfg.Endpoint))

	// Connect to the server.
	var err error
	s.grpcConn, err = s.cfg.ClientConfig.ToClientConn(ctx, s.host, s.set)
	if err != nil {
		return err
	}

	// Open a STEF/gRPC stream to the server.
	grpcClient := stef_proto.NewSTEFDestinationClient(s.grpcConn)

	// Let server know about our schema.
	schema, err := oteltef.MetricsWireSchema()
	if err != nil {
		return err
	}

	settings := stefgrpc.ClientSettings{
		Logger:       &loggerWrapper{s.logger},
		GrpcClient:   grpcClient,
		ClientSchema: schema,
		Callbacks: stefgrpc.ClientCallbacks{
			OnAck: s.onGrpcAck,
		},
	}
	client := stefgrpc.NewClient(settings)

	grpcWriter, opts, err := client.Connect(context.Background())
	if err != nil {
		s.grpcConn.Close()
		return err
	}

	opts.Compression = s.compression

	// Create record writer over gRPC stream.
	stefWriter, err := oteltef.NewMetricsWriter(grpcWriter, opts)
	if err != nil {
		return err
	}
	s.stefWriter.Store(stefWriter)

	return nil
}

func (s *stefExporter) disconnect() {
	// If there is an existing connection close it.
	if s.grpcConn != nil {
		if err := s.grpcConn.Close(); err != nil {
			s.logger.Error("failed to close grpc connection", zap.Error(err))
		}
		s.grpcConn = nil
	}
}

func (s *stefExporter) exportMetrics(ctx context.Context, md pmetric.Metrics) error {
	return s.sync2async.Sync(ctx, md)
}

func (s *stefExporter) asyncExportMetrics(
	ctx context.Context,
	data any,
	ackCh internal.AckChan,
) (internal.DataID, error) {
	if err := s.connector.Wait(ctx); err != nil {
		return 0, err
	}

	converter := stefpdatametrics.OtlpToTEFUnsorted{}
	md := data.(pmetric.Metrics)

	// stefWriter is not safe for concurrent writing, protect it.
	s.stefWriterMutex.Lock()
	stefWriter := s.stefWriter.Load()
	err := converter.WriteMetrics(md, stefWriter)
	if err != nil {
		s.stefWriterMutex.Unlock()

		// TODO: check if err is because STEF encoding failed. If so we must not
		// try to re-encode the same data. Return consumererror.NewPermanent(err)
		// to the caller.
		s.disconnect()

		// Return an error to retry sending these metrics again next time.
		return 0, err
	}

	// According to STEF gRPC spec the destination ack IDs match written record number.
	// When the data we have just written is received by destination it will send us
	// back and ack ID that numerically matches the last written record number.
	expectedAckID := internal.DataID(stefWriter.RecordCount())
	s.stefWriterMutex.Unlock()

	s.ackDataMutex.Lock()
	s.pendingAcks = append(s.pendingAcks, pendingAck{dataID: expectedAckID, ch: ackCh})
	s.ackDataMutex.Unlock()

	return expectedAckID, nil
}

func (s *stefExporter) onGrpcAck(ackID uint64) error {
	s.ackDataMutex.Lock()
	defer s.ackDataMutex.Unlock()

	if s.lastAckedID < ackID {
		s.lastAckedID = ackID
		i := 0
		for ; i < len(s.pendingAcks); i++ {
			if s.pendingAcks[i].dataID <= internal.DataID(ackID) {
				//fmt.Printf("Signal %04d\n", s.pendingAcks[i].dataID)
				s.pendingAcks[i].ch <- s.pendingAcks[i].dataID
			} else {
				break
			}
		}
		s.pendingAcks = slices.Delete(s.pendingAcks, 0, i)
	}
	return nil
}

func (s *stefExporter) flusher() {
	defer s.jobs.Done()

	// This goroutine monitors 2 timeouts:
	// 1. To flush accumulated data.
	// 2. To notify waiters when ack waiting time expires.

	flushTimer := time.NewTicker(100 * time.Millisecond)
	defer flushTimer.Stop()

	for {
		select {
		case <-flushTimer.C:
			s.stefWriterMutex.Lock()
			stefWriter := s.stefWriter.Load()
			if stefWriter == nil {
				continue
			}
			err := stefWriter.Flush()
			s.stefWriterMutex.Unlock()
			if err != nil {
				// If Flush() fails something is wrong with the connection.
				// We need to reconnect.
				s.logger.Error("Error flushing data.", zap.Error(err))
				s.connector.Reconnect(context.Background())
			}

		case <-s.shutdownSignal:
			return
		}
	}
}
