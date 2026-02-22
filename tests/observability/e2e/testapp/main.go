// Copyright 2026 Naadir Jeewa
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
//
// SPDX-License-Identifier: Apache-2.0

// Test OTel application for E2E validation of the observability stack.
//
// This binary uses the OpenTelemetry Go SDK to emit a test metric and span.
// It reads OTEL_EXPORTER_OTLP_* environment variables injected by the OTel
// Operator's mutating webhook (via the Instrumentation CR). This validates
// the full pipeline: operator injection → SDK configuration → backend ingestion.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func main() {
	ctx := context.Background()

	// Log injected OTEL_* env vars for debugging.
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "OTEL_") {
			fmt.Println(env)
		}
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName("e2e-test")),
	)
	if err != nil {
		log.Fatalf("creating resource: %v", err)
	}

	// Metrics: OTLP HTTP/protobuf (reads OTEL_EXPORTER_OTLP_METRICS_ENDPOINT).
	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		log.Fatalf("creating metric exporter: %v", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(1*time.Second),
		)),
		sdkmetric.WithResource(res),
	)
	defer func() { _ = mp.Shutdown(ctx) }()

	otel.SetMeterProvider(mp)

	// Traces: OTLP HTTP/protobuf (reads OTEL_EXPORTER_OTLP_TRACES_ENDPOINT).
	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		log.Fatalf("creating trace exporter: %v", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	defer func() { _ = tp.Shutdown(ctx) }()

	otel.SetTracerProvider(tp)

	// Emit test metric.
	counter, err := otel.Meter("e2e-test").Int64Counter("e2e_test_counter",
		metric.WithDescription("E2E test counter metric"),
	)
	if err != nil {
		log.Fatalf("creating counter: %v", err)
	}

	counter.Add(ctx, 42, metric.WithAttributes(attribute.String("test", "e2e")))
	log.Println("Metric emitted: e2e_test_counter=42")

	// Emit test span.
	_, span := otel.Tracer("e2e-test").Start(ctx, "e2e-test-span")
	span.SetAttributes(attribute.String("test", "e2e"))
	span.End()

	log.Println("Span emitted: e2e-test-span")

	// Print a log message for Vector to collect via container stdout.
	fmt.Println("E2E test log message")

	// Wait for the periodic metric reader and trace batcher to flush.
	log.Println("Waiting for telemetry export...")
	time.Sleep(5 * time.Second)
	log.Println("Test telemetry app completed successfully")
}
