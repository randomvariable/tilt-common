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

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/marcboeker/go-duckdb"
)

// jaegerServicesResponse represents the JSON response from the Jaeger-compatible
// /api/services endpoint.
type jaegerServicesResponse struct {
	Data []string `json:"data"`
}

// jaegerTracesResponse represents the top-level JSON response from the
// Jaeger-compatible /api/traces endpoint.
type jaegerTracesResponse struct {
	Data []jaegerTrace `json:"data"`
}

// jaegerTrace represents a single trace in Jaeger JSON format.
type jaegerTrace struct {
	TraceID   string                   `json:"traceID"`
	Spans     []jaegerSpan             `json:"spans"`
	Processes map[string]jaegerProcess `json:"processes"`
}

// jaegerSpan represents a single span within a Jaeger trace.
type jaegerSpan struct {
	TraceID       string      `json:"traceID"`
	SpanID        string      `json:"spanID"`
	OperationName string      `json:"operationName"`
	References    []jaegerRef `json:"references"`
	StartTime     int64       `json:"startTime"`
	Duration      int64       `json:"duration"`
	Tags          []jaegerTag `json:"tags"`
	ProcessID     string      `json:"processID"`
}

// jaegerRef represents a span reference (e.g., CHILD_OF).
type jaegerRef struct {
	RefType string `json:"refType"`
	TraceID string `json:"traceID"`
	SpanID  string `json:"spanID"`
}

// jaegerTag represents a key-value tag on a span or process.
type jaegerTag struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// jaegerProcess represents the process that produced the spans.
type jaegerProcess struct {
	ServiceName string      `json:"serviceName"`
	Tags        []jaegerTag `json:"tags"`
}

// flatSpan is an intermediate representation of a span flattened from the
// Jaeger JSON format, ready for insertion into DuckDB.
type flatSpan struct {
	TraceID       string
	SpanID        string
	ParentSpanID  string
	Operation     string
	ServiceName   string
	SpanKind      string
	StartTime     time.Time
	EndTime       time.Time
	DurationUS    int64
	StatusCode    string
	StatusMessage string
	Attributes    duckdb.Map
	ResourceAttrs duckdb.Map
}

// ExportTraces fetches all traces from VictoriaTraces via its Jaeger-compatible
// HTTP API and writes them to the spans table in the DuckDB database.
func ExportTraces(ctx context.Context, database *sql.DB, tracesURL string, client *http.Client) error {
	services, err := fetchServices(ctx, client, tracesURL)
	if err != nil {
		return fmt.Errorf("listing services: %w", err)
	}

	if len(services) == 0 {
		slog.Info("no services found, skipping traces export")

		return nil
	}

	slog.Info("discovered services", "count", len(services), "services", services)

	var allSpans []flatSpan

	for _, svc := range services {
		spans, fetchErr := fetchTracesForService(ctx, client, tracesURL, svc)
		if fetchErr != nil {
			slog.Warn("failed to fetch traces for service", "service", svc, "error", fetchErr)

			continue
		}

		allSpans = append(allSpans, spans...)
	}

	if len(allSpans) == 0 {
		slog.Info("no spans collected from any service")

		return nil
	}

	if err := writeSpans(ctx, database, allSpans); err != nil {
		return fmt.Errorf("writing spans: %w", err)
	}

	slog.Info("spans rows written", "count", len(allSpans))

	return nil
}

// fetchServices retrieves the list of service names from the Jaeger-compatible
// /api/services endpoint.
func fetchServices(ctx context.Context, client *http.Client, tracesURL string) ([]string, error) {
	resp, err := fetchJSON[jaegerServicesResponse](ctx, client, tracesURL, servicesAPIPath)
	if err != nil {
		return nil, fmt.Errorf("fetching services: %w", err)
	}

	return resp.Data, nil
}

// fetchTracesForService retrieves traces for a given service and returns them
// as flattened spans.
func fetchTracesForService(ctx context.Context, client *http.Client, tracesURL, serviceName string) ([]flatSpan, error) {
	parsedURL, err := url.Parse(tracesURL)
	if err != nil {
		return nil, fmt.Errorf("parsing traces URL: %w", err)
	}

	parsedURL.Path = tracesAPIPath

	query := parsedURL.Query()
	query.Set("service", serviceName)
	parsedURL.RawQuery = query.Encode()

	resp, err := httpGetStream(ctx, client, parsedURL.String())
	if err != nil {
		return nil, fmt.Errorf("fetching traces for service %q: %w", serviceName, err)
	}

	defer closeResource("traces response body", resp.Body)

	var tracesResp jaegerTracesResponse
	if err := json.NewDecoder(resp.Body).Decode(&tracesResp); err != nil {
		return nil, fmt.Errorf("decoding traces response for service %q: %w", serviceName, err)
	}

	return FlattenTraces(tracesResp.Data), nil
}

// FlattenTraces converts Jaeger trace data into flat span rows suitable for
// DuckDB insertion. This is exported for testing purposes.
func FlattenTraces(traces []jaegerTrace) []flatSpan {
	var totalSpans int

	for i := range traces {
		totalSpans += len(traces[i].Spans)
	}

	spans := make([]flatSpan, 0, totalSpans)

	for i := range traces {
		for j := range traces[i].Spans {
			span := &traces[i].Spans[j]
			flattenedSpan := flatSpan{
				TraceID:   span.TraceID,
				SpanID:    span.SpanID,
				Operation: span.OperationName,
				// Jaeger startTime is in microseconds.
				StartTime:  time.UnixMicro(span.StartTime).UTC(),
				DurationUS: span.Duration,
				Attributes: make(duckdb.Map),
			}

			// Duration is in microseconds. Compute end time.
			flattenedSpan.EndTime = flattenedSpan.StartTime.Add(time.Duration(span.Duration) * time.Microsecond)

			extractSpanMetadata(span, &flattenedSpan)
			resolveProcess(&traces[i], span, &flattenedSpan)

			spans = append(spans, flattenedSpan)
		}
	}

	return spans
}

// extractSpanMetadata populates parent span ID, span kind, status, and
// attributes from the Jaeger span's references and tags.
func extractSpanMetadata(span *jaegerSpan, flattenedSpan *flatSpan) {
	for _, ref := range span.References {
		if ref.RefType == "CHILD_OF" {
			flattenedSpan.ParentSpanID = ref.SpanID

			break
		}
	}

	for _, tag := range span.Tags {
		tagValue := fmt.Sprintf("%v", tag.Value)

		switch tag.Key {
		case "span.kind":
			flattenedSpan.SpanKind = tagValue
		case "otel.status_code":
			flattenedSpan.StatusCode = tagValue
		case "otel.status_description":
			flattenedSpan.StatusMessage = tagValue
		default:
			flattenedSpan.Attributes[tag.Key] = tagValue
		}
	}
}

// resolveProcess populates service name and resource attributes from the
// trace's process map.
func resolveProcess(trace *jaegerTrace, span *jaegerSpan, flattenedSpan *flatSpan) {
	proc, ok := trace.Processes[span.ProcessID]
	if !ok {
		return
	}

	flattenedSpan.ServiceName = proc.ServiceName
	flattenedSpan.ResourceAttrs = make(duckdb.Map)

	for _, tag := range proc.Tags {
		flattenedSpan.ResourceAttrs[tag.Key] = fmt.Sprintf("%v", tag.Value)
	}
}

// writeSpans bulk-inserts the flattened spans into the DuckDB spans table
// using the Appender API for high throughput.
func writeSpans(ctx context.Context, database *sql.DB, spans []flatSpan) error {
	conn, appender, err := newTableAppender(ctx, database, "spans")
	if err != nil {
		return err
	}

	defer closeResource("spans connection", conn)
	defer closeResource("spans appender", appender)

	exportTime := time.Now().UTC()

	for i := range spans {
		s := &spans[i]

		err := appender.AppendRow(
			s.TraceID,
			s.SpanID,
			nilIfEmpty(s.ParentSpanID),
			s.Operation,
			s.ServiceName,
			nilIfEmpty(s.SpanKind),
			s.StartTime,
			s.EndTime,
			s.DurationUS,
			nilIfEmpty(s.StatusCode),
			nilIfEmpty(s.StatusMessage),
			s.Attributes,
			s.ResourceAttrs,
			exportTime,
		)
		if err != nil {
			return fmt.Errorf("appending span row: %w", err)
		}
	}

	if err := appender.Flush(); err != nil {
		return fmt.Errorf("flushing spans appender: %w", err)
	}

	return nil
}

// nilIfEmpty returns nil if the string is empty, otherwise returns the string.
// This allows DuckDB to store NULL for optional VARCHAR columns.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}

	return s
}
