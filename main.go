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
	"go.opentelemetry.io/otel/exporters/otlp/otlphttp"
	"go.opentelemetry.io/otel/label"
	"go.opentelemetry.io/otel/metric"
	controller "go.opentelemetry.io/otel/sdk/metric/controller/basic"
	processor "go.opentelemetry.io/otel/sdk/metric/processor/basic"
	"go.opentelemetry.io/otel/sdk/metric/selector/simple"
)

func main() {
	logger.Info().Msg("starting GPU metrics server")
	d, err := newGPUDevices()
	if err != nil {
		logger.Fatal().Msgf("failed to get new GPU device instance: %v", err)
	}

	ctx := context.Background()
	driver := otlphttp.NewDriver(
		otlphttp.WithInsecure(),
		otlphttp.WithEndpoint("localhost:55681"),
	)
	exporter, err := otlp.NewExporter(ctx, driver)
	if err != nil {
		logger.Fatal().Msgf("failed to initialize OTLP exporter: %v", err)
	}
	defer func(ctx context.Context) {
		err := exporter.Shutdown(ctx)
		if err != nil {
			logger.Fatal().Msgf("failed to shutdown OTLP exporter: %v", err)
		}
	}(ctx)

	cont := controller.New(
		processor.New(simple.NewWithExactDistribution(), exporter),
		controller.WithPusher(exporter),
	)

	otel.SetMeterProvider(cont.MeterProvider())
	cont.Start(ctx)
	defer cont.Stop(ctx)

	d.startScraping(ctx)
	defer d.stopScraping()
}

type devices struct {
	d              map[string]*nvml.Device
	scrapeInterval time.Duration
	done           chan struct{}
}

func newGPUDevices() (*devices, error) {
	err := nvml.Init()
	if err != nil {
		return nil, err
	}

	count, err := nvml.GetDeviceCount()
	if err != nil {
		return nil, err
	}
	logger.Info().Msgf("found %v GPU devices", count)
	gpuDevices := make(map[string]*nvml.Device)
	for i := uint(0); i < count; i++ {
		device, err := nvml.NewDevice(i)
		if err != nil {
			return nil, err
		}
		gpuDevices[device.UUID] = device
	}

	return &devices{
		d:              gpuDevices,
		scrapeInterval: 20 * time.Second,
		done:           make(chan struct{}),
	}, nil
}

var (
	temp    metric.Int64ValueRecorder
	pu      metric.Int64ValueRecorder
	memu    metric.Int64ValueRecorder
	memf    metric.Int64ValueRecorder
	gpuUtil metric.Int64ValueRecorder
	memUtil metric.Int64ValueRecorder
	encUtil metric.Int64ValueRecorder
	decUtil metric.Int64ValueRecorder
)

func (d *devices) startScraping(ctx context.Context) {
	meter := otel.Meter("gpumetric/basic")
	initValueRecorders(meter)

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

func (d *devices) stopScraping() {
	err := nvml.Shutdown()
	if err != nil {
		logger.Fatal().Msgf("failed to shutdown NVML: %v", err)
		return
	}
	close(d.done)
}

func (d *devices) scrapeAndExport(ctx context.Context) {
	for uuid, device := range d.d {
		labels := []label.KeyValue{
			label.String("devide", uuid),
		}
		status, err := device.Status()
		if err != nil {
			logger.Error().Msgf("error on getting device status: %v", err)
		}

		recordValue(ctx, status, labels)
	}
}

func initValueRecorders(meter metric.Meter) {
	temp = metric.Must(meter).NewInt64ValueRecorder("gpu/temperature",
		metric.WithDescription("GPU temperature"),
		metric.WithUnit("C"))
	pu = metric.Must(meter).NewInt64ValueRecorder("gpu/power/usage",
		metric.WithDescription("GPU power usage"),
		metric.WithUnit("mW"))
	memu = metric.Must(meter).NewInt64ValueRecorder("gpu/memory/usage",
		metric.WithDescription("GPU memory usage"),
		metric.WithUnit("MiB"))
	memf = metric.Must(meter).NewInt64ValueRecorder("gpu/memory/free",
		metric.WithDescription("GPU memory free size"),
		metric.WithUnit("MiB"))
	gpuUtil = metric.Must(meter).NewInt64ValueRecorder("gpu/percent_used",
		metric.WithDescription("GPU utilization"),
		metric.WithUnit("%"))
	memUtil = metric.Must(meter).NewInt64ValueRecorder("gpu/memory/percent_used",
		metric.WithDescription("GPU Memory utilization"),
		metric.WithUnit("%"))
	decUtil = metric.Must(meter).NewInt64ValueRecorder("gpu/decoder/utilization",
		metric.WithDescription("The current utilization and sampling size in microseconds for the Decoder"),
		metric.WithUnit("ms"))
	encUtil = metric.Must(meter).NewInt64ValueRecorder("gpu/encoder/utilization",
		metric.WithDescription("The current utilization and sampling size in microseconds for the Encoder"),
		metric.WithUnit("ms"))
}

func recordValue(ctx context.Context, status *nvml.DeviceStatus, labels []label.KeyValue) {
	temp.Record(ctx, int64(*status.Temperature), labels...)
	pu.Record(ctx, int64(*status.Power), labels...)
	memu.Record(ctx, int64(*status.Memory.Global.Used), labels...)
	memf.Record(ctx, int64(*status.Memory.Global.Free), labels...)
	gpuUtil.Record(ctx, int64(*status.Utilization.GPU), labels...)
	memUtil.Record(ctx, int64(*status.Utilization.Memory), labels...)
	decUtil.Record(ctx, int64(*status.Utilization.Decoder), labels...)
	encUtil.Record(ctx, int64(*status.Utilization.Encoder), labels...)
}
