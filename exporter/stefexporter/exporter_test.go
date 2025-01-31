// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package stefexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stefexporter"

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	stefgrpc "github.com/splunk/stef/go/grpc"
	"github.com/splunk/stef/go/grpc/stef_proto"
	"github.com/splunk/stef/go/otel/oteltef"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/testdata"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/common/testutil"
)

func newGrpcServer(listener net.Listener) (*grpc.Server, int) {
	serverPort := listener.Addr().(*net.TCPAddr).Port
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	return grpcServer, serverPort
}

type mockMetricDestServer struct {
	stef_proto.UnimplementedSTEFDestinationServer
	logger          *zap.Logger
	grpcServer      *grpc.Server
	recordsReceived atomic.Int64
	acksSent        atomic.Int64
	endpoint        string
	failAckCount    atomic.Int64
}

func newMockMetricDestServer(t *testing.T, logger *zap.Logger) *mockMetricDestServer {
	m := &mockMetricDestServer{logger: logger}
	tcpAddr := testutil.GetAvailableLocalAddress(t)
	m.endpoint = tcpAddr
	return m
}

func (m *mockMetricDestServer) start() {
	listener, err := net.Listen("tcp", m.endpoint)
	if err != nil {
		m.logger.Fatal("Failed to find an available address to run the gRPC server", zap.Error(err))
	}

	grpcServer, serverPort := newGrpcServer(listener)
	m.logger.Info("Listening for connections", zap.Int("port", serverPort))

	m.grpcServer = grpcServer

	schema, err := oteltef.MetricsWireSchema()
	if err != nil {
		m.logger.Fatal("Failed to load schema", zap.Error(err))
	}

	settings := stefgrpc.ServerSettings{
		Logger:       nil,
		ServerSchema: schema,
		MaxDictBytes: 0,
		OnStream:     m.onStream,
	}
	mockServer := stefgrpc.NewStreamServer(settings)
	stef_proto.RegisterSTEFDestinationServer(grpcServer, mockServer)
	go func() {
		err := grpcServer.Serve(listener)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			m.logger.Fatal("Failed to start STEF server", zap.Error(err))
		}
	}()
}

func (m *mockMetricDestServer) stop() {
	m.grpcServer.Stop()
}

func (m *mockMetricDestServer) onStream(grpcReader stefgrpc.GrpcReader, ackFunc func(sequenceId uint64) error) error {
	m.logger.Info("Incoming TEF/gRPC connection.")

	reader, err := oteltef.NewMetricsReader(grpcReader)
	if err != nil {
		m.logger.Error("Error creating metrics reader from connection", zap.Error(err))
		return err
	}

	m.failAckCount.Add(-1)

	for {
		_, err = reader.Read()
		if err != nil {
			m.logger.Debug("Error reading from connection", zap.Error(err))
			return err
		}
		m.recordsReceived.Add(1)

		if m.failAckCount.Load() >= 0 {
			// This connection must fail to ack.
			continue
		}

		if err = ackFunc(reader.RecordCount()); err != nil {
			return err
		}
		m.acksSent.Add(1)
	}
}

func runTest(
	t *testing.T,
	cfg *Config,
	f func(cfg *Config, mockSrv *mockMetricDestServer, exp exporter.Metrics),
) {
	logCfg := zap.NewDevelopmentConfig()
	logCfg.DisableStacktrace = true
	logger, _ := logCfg.Build()

	mockSrv := newMockMetricDestServer(t, logger)

	mockSrv.start()
	defer mockSrv.stop()

	// Start an exporter and point to the server.
	factory := NewFactory()
	if cfg == nil {
		cfg = factory.CreateDefaultConfig().(*Config)
	}
	cfg.ClientConfig = configgrpc.ClientConfig{
		Endpoint: mockSrv.endpoint,
		// Use insecure mode for tests so that we don't bother with certificates.
		TLSSetting: configtls.ClientConfig{Insecure: true},
	}

	// Make retries quick. We will be testing failure modes and don't want test to take too long.
	cfg.RetryConfig.InitialInterval = 10 * time.Millisecond

	set := exportertest.NewNopSettings()
	set.TelemetrySettings.Logger = logger

	exp, err := factory.CreateMetrics(context.Background(), set, cfg)
	require.NoError(t, err)
	require.NotNil(t, exp)

	defer func() {
		assert.NoError(t, exp.Shutdown(context.Background()))
	}()

	host := componenttest.NewNopHost()
	require.NoError(t, exp.Start(context.Background(), host))

	f(cfg, mockSrv, exp)
}

func TestExport(t *testing.T) {
	runTest(
		t,
		nil,
		func(cfg *Config, mockSrv *mockMetricDestServer, exp exporter.Metrics) {
			// Send some metrics. Make sure the count of batches exceeds the number of consumers
			// so that we can hit the case where exporter begins to forcedly flush encoded data.
			pointCount := int64(0)
			for i := 0; i < 2*cfg.QueueConfig.NumConsumers; i++ {
				md := testdata.GenerateMetrics(1)
				pointCount += int64(md.DataPointCount())
				err := exp.ConsumeMetrics(context.Background(), md)
				require.NoError(t, err)
			}

			// Wait for data to be received.
			assert.Eventually(
				t, func() bool { return mockSrv.recordsReceived.Load() == pointCount },
				5*time.Second, 5*time.Millisecond,
			)
		},
	)
}

func TestReconnect(t *testing.T) {
	runTest(
		t,
		nil,
		func(_ *Config, mockSrv *mockMetricDestServer, exp exporter.Metrics) {
			md := testdata.GenerateMetrics(1)
			pointCount := int64(md.DataPointCount())
			err := exp.ConsumeMetrics(context.Background(), md)
			require.NoError(t, err)

			// Wait for data to be received.
			assert.Eventually(
				t, func() bool { return mockSrv.recordsReceived.Load() == pointCount },
				5*time.Second, 5*time.Millisecond,
			)

			mockSrv.logger.Debug("First set of data received.")

			// Disconnect from server side to verify that the exporter will reconnect
			mockSrv.logger.Debug("Restarting mock STEF server.")
			mockSrv.stop()
			mockSrv.start()

			// Send more data
			md = testdata.GenerateMetrics(1)
			pointCount += int64(md.DataPointCount())
			err = exp.ConsumeMetrics(context.Background(), md)
			require.NoError(t, err)

			// Wait for data to be received.
			assert.Eventually(
				t, func() bool { return mockSrv.recordsReceived.Load() == pointCount },
				500*time.Second, 5*time.Millisecond,
			)

			mockSrv.logger.Debug("Second set of data received after reconnection.")
		},
	)
}

func TestAckTimeout(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)

	// Shorten max ack waiting time so that tests run fast.
	// Increase this if the second eventually() below fails sporadically.
	cfg.maxAckWaitTime = 300 * time.Millisecond

	runTest(
		t,
		cfg,
		func(_ *Config, mockSrv *mockMetricDestServer, exp exporter.Metrics) {
			// Server will fail to ack all points that the exporter sends for the first connection.
			mockSrv.failAckCount.Store(1)

			pointCount := int64(0)
			for i := 0; i < cfg.QueueConfig.NumConsumers; i++ {
				md := testdata.GenerateMetrics(1)
				pointCount += int64(md.DataPointCount())
				err := exp.ConsumeMetrics(context.Background(), md)
				require.NoError(t, err)
			}

			// Wait for data to be received.
			assert.Eventually(
				t, func() bool { return mockSrv.recordsReceived.Load() == pointCount },
				5*time.Second, 5*time.Millisecond,
			)

			mockSrv.logger.Debug("First set of data received. Should remain unacknowledged.")
			// Verify that no acks were sent.
			assert.EqualValues(t, 0, mockSrv.acksSent.Load())

			// Because ack was not made by the server, the exporter is going to timeout,
			// reconnect and send the data again. The same data will be delivered again,
			// so recordsReceived counter will be twice the point count.
			assert.Eventually(
				t, func() bool { return mockSrv.recordsReceived.Load() == 2*pointCount },
				5*time.Second, 5*time.Millisecond,
			)

			mockSrv.logger.Debug("Second set of data received after reconnection. Should be acknowledged.")
			// Verify that acks were sent.
			assert.EqualValues(t, pointCount, mockSrv.acksSent.Load())
		},
	)
}
