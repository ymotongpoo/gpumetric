// Copyright 2021 Yoshi Yamaguchi
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

package main

import (
	"context"
	"time"

	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric/controller/push"
	"go.opentelemetry.io/otel/sdk/metric/processor/basic"
	"go.opentelemetry.io/otel/sdk/metric/selector/simple"
)

var (
	temperature           metric.Int64ValueRecorder
	powerUsage            metric.Int64ValueRecorder
	pcieThroughputRxBytes metric.Int64ValueRecorder
	pcieThroughputTxBytes metric.Int64ValueRecorder
	pcieThroughputCount   metric.Int64ValueRecorder
)

func main() {
	d, err := newGPUDevice()

	ctx := context.Background()
	exporter, err := otlp.NewExporter(ctx, otlp.WithInsecure())
	if err != nil {
		logger.Fatal().Msgf("failed to initialize OTLP exporter: %v", err)
	}
	defer func(ctx context.Context) {
		err := exporter.Shutdown(ctx)
		if err != nil {
			logger.Fatal().Msgf("failed to shutdown OTLP exporter: %v", err)
		}
	}(ctx)

	pusher := push.New(
		basic.New(simple.NewWithExactDistribution(), exporter),
		exporter,
		push.WithPeriod(1*time.Minute),
	)

	mp := pusher.MeterProvider()
	otel.SetMeterProvider(mp)
	pusher.Start()
	defer pusher.Stop()

	meter := mp.Meter("google-cloud-metrics-agent/gpumetric")
	temperature = metric.Must(meter).NewInt64ValueRecorder("temperature")
	powerUsage = metric.Must(meter).NewInt64ValueRecorder("powerusage")
	pcieThroughputRxBytes = metric.Must(meter).NewInt64ValueRecorder("throughput.rx")
	pcieThroughputTxBytes = metric.Must(meter).NewInt64ValueRecorder("throughput.tx")
	pcieThroughputCount = metric.Must(meter).NewInt64ValueRecorder("throughput.count")

	d.startScraping(ctx)
	defer d.stopScraping()
}

type device struct {
	d              *nvml.Device
	scrapeInterval time.Duration
	done           chan struct{}
}

func newGPUDevice() (*device, error) {
	err := nvml.Init()
	if err != nil {
		return nil, err
	}
	d, err := nvml.NewDeviceLite(uint(0))
	if err != nil {
		return nil, err
	}
	return &device{
		d:              d,
		scrapeInterval: 5 * time.Second,
		done:           make(chan struct{}),
	}, nil
}

func (d *device) startScraping(ctx context.Context) {
	ticker := time.NewTicker(d.scrapeInterval)
	for {
		select {
		case <-ticker.C:
			d.scrapeAndExport(ctx)
		case <-d.done:
			return
		}
	}
}

func (d *device) stopScraping() {
	err := nvml.Shutdown()
	if err != nil {
		logger.Fatal().Msgf("failed to shutdown NVML: %v", err)
		return
	}
	close(d.done)
}

func (d *device) scrapeAndExport(ctx context.Context) {
	status, err := d.d.Status()
	if err != nil {
		logger.Error().Msgf("error on getting device status: %v", err)
	}
	temperature.Record(ctx, int64(*status.Temperature))
	powerUsage.Record(ctx, int64(*status.Power))
	pcieThroughputRxBytes.Record(ctx, int64(*status.PCI.Throughput.RX))
	pcieThroughputTxBytes.Record(ctx, int64(*status.PCI.Throughput.TX))
	pcieThroughputCount.Record(ctx, int64(*status.PCI.BAR1Used))
}
