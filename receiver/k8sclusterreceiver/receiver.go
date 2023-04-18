// Copyright 2020, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sclusterreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver"

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/obsreport"
	"go.opentelemetry.io/collector/receiver"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/sharedcomponent"
)

const (
	transport = "http"

	defaultInitialSyncTimeout = 10 * time.Minute
)

var _ receiver.Metrics = (*kubernetesReceiver)(nil)

type kubernetesReceiver struct {
	resourceWatcher *resourceWatcher

	config            *Config
	settings          receiver.CreateSettings
	metricConsumer    consumer.Metrics
	entityLogConsumer consumer.Logs
	cancel            context.CancelFunc
	obsrecv           *obsreport.Receiver
}

func (kr *kubernetesReceiver) Start(ctx context.Context, host component.Host) error {
	ctx, kr.cancel = context.WithCancel(ctx)

	if err := kr.resourceWatcher.initialize(); err != nil {
		return err
	}

	exporters := host.GetExporters()
	if err := kr.resourceWatcher.setupMetadataExporters(
		exporters[component.DataTypeMetrics], kr.config.MetadataExporters,
	); err != nil {
		return err
	}

	kr.resourceWatcher.entityLogConsumer = kr.entityLogConsumer

	go func() {
		kr.settings.Logger.Info("Starting shared informers and wait for initial cache sync.")
		for _, informer := range kr.resourceWatcher.informerFactories {
			if informer == nil {
				continue
			}
			timedContextForInitialSync := kr.resourceWatcher.startWatchingResources(ctx, informer)

			// Wait till either the initial cache sync times out or until the cancel method
			// corresponding to this context is called.
			<-timedContextForInitialSync.Done()

			// If the context times out, set initialSyncTimedOut and report a fatal error. Currently
			// this timeout is 10 minutes, which appears to be long enough.
			if errors.Is(timedContextForInitialSync.Err(), context.DeadlineExceeded) {
				kr.resourceWatcher.initialSyncTimedOut.Store(true)
				kr.settings.Logger.Error("Timed out waiting for initial cache sync.")
				host.ReportFatalError(errors.New("failed to start receiver"))
				return
			}
		}

		kr.settings.Logger.Info("Completed syncing shared informer caches.")
		kr.resourceWatcher.initialSyncDone.Store(true)

		ticker := time.NewTicker(kr.config.CollectionInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				kr.dispatchMetrics(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

func (kr *kubernetesReceiver) Shutdown(context.Context) error {
	if kr.cancel == nil {
		return nil
	}
	kr.cancel()
	return nil
}

func (kr *kubernetesReceiver) dispatchMetrics(ctx context.Context) {
	now := time.Now()
	mds := kr.resourceWatcher.dataCollector.CollectMetricData(now)

	c := kr.obsrecv.StartMetricsOp(ctx)

	numPoints := mds.DataPointCount()
	err := kr.metricConsumer.ConsumeMetrics(c, mds)
	kr.obsrecv.EndMetricsOp(c, typeStr, numPoints, err)
}

// This is the map of already created OTLP receivers for particular configurations.
// We maintain this map because the Factory is asked trace and metric receivers separately
// when it gets CreateTracesReceiver() and CreateMetricsReceiver() but they must not
// create separate objects, they must use one otlpReceiver object per configuration.
// When the receiver is shutdown it should be removed from this map so the same configuration
// can be recreated successfully.
var receivers = sharedcomponent.NewSharedComponents()

func newKubernetesReceiver(rCfg *Config, set receiver.CreateSettings) (*kubernetesReceiver, error) {
	obsrecv, err := obsreport.NewReceiver(
		obsreport.ReceiverSettings{
			ReceiverID:             set.ID,
			Transport:              transport,
			ReceiverCreateSettings: set,
		},
	)
	if err != nil {
		return nil, err
	}
	return &kubernetesReceiver{
		resourceWatcher: newResourceWatcher(set.Logger, rCfg),
		settings:        set,
		config:          rCfg,
		obsrecv:         obsrecv,
	}, nil
}

// newMetricReceiver creates the Kubernetes cluster receiver with the given configuration.
func newMetricReceiver(
	_ context.Context, set receiver.CreateSettings, cfg component.Config, consumer consumer.Metrics,
) (receiver.Metrics, error) {
	oCfg := cfg.(*Config)
	var err error
	var rcv *kubernetesReceiver
	receivers.GetOrAdd(
		oCfg, func() component.Component {
			rcv, err = newKubernetesReceiver(oCfg, set)
			return rcv
		},
	)
	if err != nil {
		return nil, err
	}

	rcv.metricConsumer = consumer
	return rcv, nil
}

func newEntityLogReceiver(
	_ context.Context, set receiver.CreateSettings, cfg component.Config, consumer consumer.Logs,
) (receiver.Logs, error) {
	oCfg := cfg.(*Config)
	var err error
	var rcv *kubernetesReceiver
	receivers.GetOrAdd(
		oCfg, func() component.Component {
			rcv, err = newKubernetesReceiver(oCfg, set)
			return rcv
		},
	)
	if err != nil {
		return nil, err
	}

	rcv.entityLogConsumer = consumer
	return rcv, nil
}
