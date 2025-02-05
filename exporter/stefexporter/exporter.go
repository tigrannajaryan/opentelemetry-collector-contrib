// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package stefexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stefexporter"

import (
	"context"
	"fmt"
	"sync"

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
// received from destination.
// The exporter relies on a preceding Retry helper to retry sending data that is
// not acknowledged or otherwise fails to be sent. The exporter will not retry
// sending the data itself.
type stefExporter struct {
	set         component.TelemetrySettings
	host        component.Host
	logger      *zap.Logger
	cfg         *Config
	compression pkg.Compression

	// connMutex is taken when connecting, disconnecting or checking connection status.
	connMutex   sync.Mutex
	isConnected bool
	grpcConn    *grpc.ClientConn

	// The STEF writer we write metrics to and which in turns sends them over gRPC.
	stefWriter      *oteltef.MetricsWriter
	stefWriterMutex sync.Mutex // protects stefWriter

	// lastAckID is the maximum ack ID received so far.
	lastAckID uint64
	// Cond to protect and signal lastAckID.
	ackCond *internal.CancellableCond
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
		set:     set,
		logger:  set.Logger,
		cfg:     cfg,
		ackCond: internal.NewCancellableCond(),
	}

	exp.compression = pkg.CompressionNone
	if cfg.Compression == "zstd" {
		exp.compression = pkg.CompressionZstd
	}
	return exp
}

func (s *stefExporter) Start(ctx context.Context, host component.Host) error {
	s.host = host

	// Prepare gRPC connection.
	var err error
	s.grpcConn, err = s.cfg.ClientConfig.ToClientConn(ctx, host, s.set)
	if err != nil {
		return err
	}

	// No need to block Start(), we will begin connection attempt in a goroutine.
	go func() {
		if err := s.ensureConnected(); err != nil {
			s.logger.Error("Error connecting to destination", zap.Error(err))
			// exportMetrics() will try to connect again as needed.
		}
	}()
	return nil
}

func (s *stefExporter) Shutdown(_ context.Context) error {
	s.disconnect()
	if s.grpcConn != nil {
		if err := s.grpcConn.Close(); err != nil {
			s.logger.Error("failed to close grpc connection", zap.Error(err))
		}
		s.grpcConn = nil
	}
	return nil
}

func (s *stefExporter) ensureConnected() error {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()

	if s.isConnected {
		return nil
	}

	s.logger.Debug("Connecting to destination", zap.String("endpoint", s.cfg.Endpoint))

	// Prepare to open a STEF/gRPC stream to the server.
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
		return fmt.Errorf("failed to connect to destination: %w", err)
	}

	opts.Compression = s.compression

	// Create record writer over gRPC stream.
	s.stefWriter, err = oteltef.NewMetricsWriter(grpcWriter, opts)
	if err != nil {
		return err
	}

	s.isConnected = true
	s.logger.Debug("Connected to destination", zap.String("endpoint", s.cfg.Endpoint))

	return nil
}

func (s *stefExporter) disconnect() {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()

	if !s.isConnected {
		return
	}

	s.logger.Debug("Disconnecting...")
	s.isConnected = false
}

func (s *stefExporter) exportMetrics(ctx context.Context, md pmetric.Metrics) error {
	if err := s.ensureConnected(); err != nil {
		return err
	}

	// stefWriter is not safe for concurrent writing, protect it.
	s.stefWriterMutex.Lock()
	defer s.stefWriterMutex.Unlock()

	converter := stefpdatametrics.OtlpToTEFUnsorted{}
	err := converter.WriteMetrics(md, s.stefWriter)
	if err != nil {
		// Error to write to STEF stream typically indicates either:
		// 1) A problem with the connection. We need to reconnect.
		// 2) Encoding failure, possibly due to encoder bug. In this case
		//    we need to reconnect too, to make sure encoders start from
		//    initial state, which is our best chance to succeed next time.

		s.disconnect()

		// TODO: check if err is because STEF encoding failed. If so we must not
		// try to re-encode the same data. Return consumererror.NewPermanent(err)
		// to the caller. This requires changes in STEF Go library.

		// Return an error to retry sending these metrics again next time.
		return err
	}

	// According to STEF gRPC spec the destination ack IDs match written record number.
	// When the data we have just written is received by destination it will send us
	// back and ack ID that numerically matches the last written record number.
	expectedAckID := s.stefWriter.RecordCount()

	if err = s.stefWriter.Flush(); err != nil {
		// Failure to write the gRPC stream normally means something is
		// wrong with the connection. We will reconnect.
		s.disconnect()

		// Return an error to retry sending these metrics again next time.
		return err
	}

	// Wait for acknowledgement.
	err = s.ackCond.Wait(ctx, func() bool { return s.lastAckID >= expectedAckID })
	if err != nil {
		return fmt.Errorf("error waiting for ack ID %d: %w", expectedAckID, err)
	}

	return nil
}

func (s *stefExporter) onGrpcAck(ackID uint64) error {
	s.ackCond.Cond.L.Lock()
	if s.lastAckID < ackID {
		s.lastAckID = ackID
		s.ackCond.Cond.Broadcast()
	}
	s.ackCond.Cond.L.Unlock()
	return nil
}
