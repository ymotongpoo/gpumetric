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
	"sync"
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
		controller.WithCollectPeriod(2*time.Second),
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
	tempObservers map[string]*observedInt64
	puObservers   map[string]*observedInt64
	temp          metric.Int64ValueObserver
	pu            metric.Int64ValueObserver
)

type observedInt64 struct {
	mu sync.RWMutex
	i  int64
}

func (oi *observedInt64) set(v int64) {
	oi.mu.Lock()
	defer oi.mu.Unlock()
	oi.i = v
}

func (oi *observedInt64) get() int64 {
	oi.mu.RLock()
	defer oi.mu.RUnlock()
	return oi.i
}

func newObservedInt64(v int64) *observedInt64 {
	return &observedInt64{
		i: v,
	}
}

func newInt64ObserverCallback(ctx context.Context, ois map[string]*observedInt64) metric.Int64ObserverFunc {
	return func(ctx context.Context, r metric.Int64ObserverResult) {
		for k, v := range ois {
			i := v.get()
			r.Observe(i, label.String("device", k))
		}
	}
}

func (d *devices) startScraping(ctx context.Context) {
	meter := otel.Meter("gpumetric/basic")

	tempObservers = make(map[string]*observedInt64)
	puObservers = make(map[string]*observedInt64)
	for k := range d.d {
		tempObservers[k] = newObservedInt64(0)
		puObservers[k] = newObservedInt64(0)
	}

	tempCb := newInt64ObserverCallback(ctx, tempObservers)
	puCb := newInt64ObserverCallback(ctx, puObservers)

	temp = metric.Must(meter).NewInt64ValueObserver("gpu/temperature", tempCb,
		metric.WithDescription("GPU temperature"),
		metric.WithUnit("C"))
	pu = metric.Must(meter).NewInt64ValueObserver("gpu/powerusage", puCb,
		metric.WithDescription("GPU power usage"),
		metric.WithUnit("mW"))

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
	for k, v := range d.d {
		status, err := v.Status()
		if err != nil {
			logger.Error().Msgf("error on getting device status: %v", err)
		}

		tempObservers[k].set(int64(*status.Temperature))
		puObservers[k].set(int64(*status.Power))
	}
}
