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
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcboeker/go-duckdb"
	"google.golang.org/protobuf/encoding/protowire"
)

// testJSONResponse is a minimal struct used to exercise the fetchJSON helper
// without coupling tests to any particular production response type.
type testJSONResponse struct {
	Value string `json:"value"`
}

// columnSpec defines the expected name and data type for a DuckDB column.
type columnSpec struct {
	name     string
	dataType string
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("opening in-memory DuckDB: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := database.Close(); closeErr != nil {
			t.Errorf("closing test DB: %v", closeErr)
		}
	})

	return database
}

// queryCount executes a COUNT query and returns the integer result.
func queryCount(t *testing.T, database *sql.DB, query string) int {
	t.Helper()

	var count int
	if err := database.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}

	return count
}

// assertMalformedLineSkipped verifies that an exporter skips malformed JSON lines
// and still inserts valid ones. It creates an HTTP server serving one malformed
// and one valid line, runs the exporter, and asserts exactly 1 row exists.
func assertMalformedLineSkipped(t *testing.T, validLine, tableName string, exportFn func(ctx context.Context, db *sql.DB, baseURL string, client *http.Client) error) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		lines := []string{`not-valid-json`, validLine}
		for _, line := range lines {
			if _, err := fmt.Fprintln(writer, line); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)

				return
			}
		}
	}))
	defer srv.Close()

	database := openTestDB(t)
	ctx := context.Background()

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	if err := exportFn(ctx, database, srv.URL, nil); err != nil {
		t.Fatalf("export: %v", err)
	}

	count := queryCount(t, database, "SELECT COUNT(*) FROM "+tableName)
	if count != 1 {
		t.Errorf("got %d rows, want 1 (malformed line should be skipped)", count)
	}
}

// verifyTableColumns checks that a DuckDB table has the expected columns in order.
func verifyTableColumns(t *testing.T, database *sql.DB, tableName string, expected []columnSpec) {
	t.Helper()

	rows, err := database.Query(
		"SELECT column_name, data_type FROM information_schema.columns WHERE table_name = $1 ORDER BY ordinal_position",
		tableName,
	)
	if err != nil {
		t.Fatalf("querying %s columns: %v", tableName, err)
	}

	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Errorf("closing rows: %v", closeErr)
		}
	}()

	var idx int

	for rows.Next() {
		var colName, colType string

		if err := rows.Scan(&colName, &colType); err != nil {
			t.Fatalf("scanning column info: %v", err)
		}

		if idx >= len(expected) {
			t.Fatalf("more columns than expected: got %s (%s)", colName, colType)
		}

		if colName != expected[idx].name {
			t.Errorf("column %d: got name %q, want %q", idx, colName, expected[idx].name)
		}

		if colType != expected[idx].dataType {
			t.Errorf("column %q: got type %q, want %q", colName, colType, expected[idx].dataType)
		}

		idx++
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterating rows: %v", err)
	}

	if idx != len(expected) {
		t.Errorf("got %d columns, want %d", idx, len(expected))
	}
}

func TestCreateSchema(t *testing.T) {
	t.Parallel()

	metricsColumns := []columnSpec{
		{"timestamp", "TIMESTAMP"},
		{"metric_name", "VARCHAR"},
		{"labels", "MAP(VARCHAR, VARCHAR)"},
		{"value", "DOUBLE"},
		{"export_time", "TIMESTAMP"},
	}

	spansColumns := []columnSpec{
		{"trace_id", "VARCHAR"},
		{"span_id", "VARCHAR"},
		{"parent_span_id", "VARCHAR"},
		{"operation", "VARCHAR"},
		{"service_name", "VARCHAR"},
		{"span_kind", "VARCHAR"},
		{"start_time", "TIMESTAMP"},
		{"end_time", "TIMESTAMP"},
		{"duration_us", "BIGINT"},
		{"status_code", "VARCHAR"},
		{"status_message", "VARCHAR"},
		{"attributes", "MAP(VARCHAR, VARCHAR)"},
		{"resource_attrs", "MAP(VARCHAR, VARCHAR)"},
		{"export_time", "TIMESTAMP"},
	}

	logsColumns := []columnSpec{
		{"timestamp", "TIMESTAMP"},
		{"message", "VARCHAR"},
		{"stream", "VARCHAR"},
		{"severity", "VARCHAR"},
		{"fields", "MAP(VARCHAR, VARCHAR)"},
		{"export_time", "TIMESTAMP"},
	}

	tests := []struct {
		name       string
		verifyFunc func(t *testing.T, database *sql.DB)
	}{
		{
			name: "metrics table has expected columns",
			verifyFunc: func(t *testing.T, database *sql.DB) {
				t.Helper()
				verifyTableColumns(t, database, "metrics", metricsColumns)
			},
		},
		{
			name: "spans table has expected columns",
			verifyFunc: func(t *testing.T, database *sql.DB) {
				t.Helper()
				verifyTableColumns(t, database, "spans", spansColumns)
			},
		},
		{
			name: "logs table has expected columns",
			verifyFunc: func(t *testing.T, database *sql.DB) {
				t.Helper()
				verifyTableColumns(t, database, "logs", logsColumns)
			},
		},
		{
			name: "tables accept insert via SQL",
			verifyFunc: func(t *testing.T, database *sql.DB) {
				t.Helper()

				_, err := database.Exec(`INSERT INTO metrics (timestamp, metric_name, labels, value, export_time)
					VALUES ('2026-01-15 10:30:00', 'cpu_usage', MAP {'host': 'node1'}, 0.75, '2026-01-15 11:00:00')`)
				if err != nil {
					t.Fatalf("inserting into metrics: %v", err)
				}

				count := queryCount(t, database, "SELECT COUNT(*) FROM metrics")
				if count != 1 {
					t.Errorf("got %d metrics rows, want 1", count)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			database := openTestDB(t)
			ctx := context.Background()

			if err := CreateSchema(ctx, database); err != nil {
				t.Fatalf("CreateSchema: %v", err)
			}

			tt.verifyFunc(t, database)
		})
	}
}

func TestDBFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "standard timestamp",
			time:     time.Date(2026, 2, 14, 15, 30, 0, 0, time.UTC),
			expected: "telemetry-2026-02-14T153000.duckdb",
		},
		{
			name:     "midnight",
			time:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "telemetry-2026-01-01T000000.duckdb",
		},
		{
			name:     "end of day",
			time:     time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			expected: "telemetry-2026-12-31T235959.duckdb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := DBFilename(tt.time)
			if got != tt.expected {
				t.Errorf("DBFilename(%v) = %q, want %q", tt.time, got, tt.expected)
			}
		})
	}
}

func TestParseMetricsLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantName   string
		wantLabels int
		wantValues int
		wantErr    bool
	}{
		{
			name:       "single value metric",
			input:      `{"metric":{"__name__":"up","job":"test"},"values":[1],"timestamps":[1700000000000]}`,
			wantName:   "up",
			wantLabels: 1,
			wantValues: 1,
		},
		{
			name:       "multiple values",
			input:      `{"metric":{"__name__":"cpu_usage","host":"node1","region":"us-east"},"values":[0.5,0.7,0.9],"timestamps":[1700000000000,1700000015000,1700000030000]}`,
			wantName:   "cpu_usage",
			wantLabels: 2,
			wantValues: 3,
		},
		{
			name:       "metric with no extra labels",
			input:      `{"metric":{"__name__":"simple_counter"},"values":[42],"timestamps":[1700000000000]}`,
			wantName:   "simple_counter",
			wantLabels: 0,
			wantValues: 1,
		},
		{
			name:    "invalid JSON",
			input:   `{broken`,
			wantErr: true,
		},
		{
			name:       "empty values arrays",
			input:      `{"metric":{"__name__":"empty"},"values":[],"timestamps":[]}`,
			wantName:   "empty",
			wantLabels: 0,
			wantValues: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			line, err := ParseMetricsLine([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotName := line.Metric["__name__"]
			if gotName != tt.wantName {
				t.Errorf("metric name = %q, want %q", gotName, tt.wantName)
			}

			// Count labels excluding __name__.
			labelCount := max(len(line.Metric)-1, 0)

			if labelCount != tt.wantLabels {
				t.Errorf("label count = %d, want %d", labelCount, tt.wantLabels)
			}

			if len(line.Values) != tt.wantValues {
				t.Errorf("values count = %d, want %d", len(line.Values), tt.wantValues)
			}

			if len(line.Timestamps) != tt.wantValues {
				t.Errorf("timestamps count = %d, want %d", len(line.Timestamps), tt.wantValues)
			}
		})
	}
}

// verifySingleSpan checks the fields of a single span with known values.
func verifySingleSpan(t *testing.T, spans []flatSpan) {
	t.Helper()

	s := spans[0]
	if s.TraceID != "trace1" {
		t.Errorf("TraceID = %q, want %q", s.TraceID, "trace1")
	}

	if s.SpanID != "span1" {
		t.Errorf("SpanID = %q, want %q", s.SpanID, "span1")
	}

	if s.Operation != "HTTP GET /api" {
		t.Errorf("Operation = %q, want %q", s.Operation, "HTTP GET /api")
	}

	if s.ServiceName != "my-service" {
		t.Errorf("ServiceName = %q, want %q", s.ServiceName, "my-service")
	}

	if s.SpanKind != "server" {
		t.Errorf("SpanKind = %q, want %q", s.SpanKind, "server")
	}

	if s.DurationUS != 5000 {
		t.Errorf("DurationUS = %d, want %d", s.DurationUS, 5000)
	}

	expectedEnd := s.StartTime.Add(5000 * time.Microsecond)
	if !s.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime = %v, want %v", s.EndTime, expectedEnd)
	}

	if val, ok := s.Attributes["http.status_code"]; !ok || val != "200" {
		t.Errorf("attributes[http.status_code] = %v, want %q", val, "200")
	}

	if val, ok := s.ResourceAttrs["host.name"]; !ok || val != "node1" {
		t.Errorf("resource_attrs[host.name] = %v, want %q", val, "node1")
	}
}

// verifyChildSpan checks the fields of a child span with CHILD_OF reference.
func verifyChildSpan(t *testing.T, spans []flatSpan) {
	t.Helper()

	s := spans[0]
	if s.ParentSpanID != "parent1" {
		t.Errorf("ParentSpanID = %q, want %q", s.ParentSpanID, "parent1")
	}

	if s.StatusCode != "OK" {
		t.Errorf("StatusCode = %q, want %q", s.StatusCode, "OK")
	}

	if s.StatusMessage != "success" {
		t.Errorf("StatusMessage = %q, want %q", s.StatusMessage, "success")
	}
}

func TestFlattenTraces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		traces     []jaegerTrace
		wantSpans  int
		verifyFunc func(t *testing.T, spans []flatSpan)
	}{
		{
			name:      "empty traces",
			traces:    nil,
			wantSpans: 0,
		},
		{
			name: "single span with parent",
			traces: []jaegerTrace{
				{
					TraceID: "trace1",
					Spans: []jaegerSpan{
						{
							TraceID:       "trace1",
							SpanID:        "span1",
							OperationName: "HTTP GET /api",
							StartTime:     1700000000000000,
							Duration:      5000,
							ProcessID:     "p1",
							Tags: []jaegerTag{
								{Key: "span.kind", Type: "string", Value: "server"},
								{Key: "http.status_code", Type: "int64", Value: float64(200)},
							},
						},
					},
					Processes: map[string]jaegerProcess{
						"p1": {
							ServiceName: "my-service",
							Tags: []jaegerTag{
								{Key: "host.name", Type: "string", Value: "node1"},
							},
						},
					},
				},
			},
			wantSpans:  1,
			verifyFunc: verifySingleSpan,
		},
		{
			name: "child span with CHILD_OF reference",
			traces: []jaegerTrace{
				{
					TraceID: "trace2",
					Spans: []jaegerSpan{
						{
							TraceID:       "trace2",
							SpanID:        "child1",
							OperationName: "db.query",
							StartTime:     1700000001000000,
							Duration:      2000,
							ProcessID:     "p1",
							References: []jaegerRef{
								{RefType: "CHILD_OF", TraceID: "trace2", SpanID: "parent1"},
							},
							Tags: []jaegerTag{
								{Key: "otel.status_code", Type: "string", Value: "OK"},
								{Key: "otel.status_description", Type: "string", Value: "success"},
							},
						},
					},
					Processes: map[string]jaegerProcess{
						"p1": {ServiceName: "db-service"},
					},
				},
			},
			wantSpans:  1,
			verifyFunc: verifyChildSpan,
		},
		{
			name: "multiple spans across traces",
			traces: []jaegerTrace{
				{
					TraceID: "t1",
					Spans: []jaegerSpan{
						{TraceID: "t1", SpanID: "s1", OperationName: "op1", StartTime: 1000000, Duration: 100, ProcessID: "p1"},
						{TraceID: "t1", SpanID: "s2", OperationName: "op2", StartTime: 2000000, Duration: 200, ProcessID: "p1"},
					},
					Processes: map[string]jaegerProcess{
						"p1": {ServiceName: "svc-a"},
					},
				},
				{
					TraceID: "t2",
					Spans: []jaegerSpan{
						{TraceID: "t2", SpanID: "s3", OperationName: "op3", StartTime: 3000000, Duration: 300, ProcessID: "p1"},
					},
					Processes: map[string]jaegerProcess{
						"p1": {ServiceName: "svc-b"},
					},
				},
			},
			wantSpans: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spans := FlattenTraces(tt.traces)

			if len(spans) != tt.wantSpans {
				t.Fatalf("got %d spans, want %d", len(spans), tt.wantSpans)
			}

			if tt.verifyFunc != nil {
				tt.verifyFunc(t, spans)
			}
		})
	}
}

func TestExportMetrics_Integration(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"metric":{"__name__":"http_requests_total","method":"GET","status":"200"},"values":[10,20,30],"timestamps":[1700000000000,1700000015000,1700000030000]}`,
		`{"metric":{"__name__":"go_goroutines"},"values":[42],"timestamps":[1700000000000]}`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != metricsExportPath {
			http.Error(writer, "not found", http.StatusNotFound)

			return
		}

		for _, line := range lines {
			if _, err := fmt.Fprintln(writer, line); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)

				return
			}
		}
	}))
	defer srv.Close()

	database := openTestDB(t)
	ctx := context.Background()

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	if err := ExportMetrics(ctx, database, srv.URL, nil); err != nil {
		t.Fatalf("ExportMetrics: %v", err)
	}

	// Verify row count: first metric has 3 values, second has 1 = 4 total.
	count := queryCount(t, database, "SELECT COUNT(*) FROM metrics")
	if count != 4 {
		t.Errorf("got %d metrics rows, want 4", count)
	}

	httpCount := queryCount(t, database, "SELECT COUNT(*) FROM metrics WHERE metric_name = 'http_requests_total'")
	if httpCount != 3 {
		t.Errorf("http_requests_total rows = %d, want 3", httpCount)
	}

	goCount := queryCount(t, database, "SELECT COUNT(*) FROM metrics WHERE metric_name = 'go_goroutines'")
	if goCount != 1 {
		t.Errorf("go_goroutines rows = %d, want 1", goCount)
	}

	var val float64
	if err := database.QueryRow("SELECT value FROM metrics WHERE metric_name = 'go_goroutines'").Scan(&val); err != nil {
		t.Fatalf("querying go_goroutines value: %v", err)
	}

	if val != 42 {
		t.Errorf("go_goroutines value = %f, want 42", val)
	}
}

func TestExportTraces_Integration(t *testing.T) {
	t.Parallel()

	servicesResp := jaegerServicesResponse{Data: []string{"test-service"}}
	tracesResp := jaegerTracesResponse{
		Data: []jaegerTrace{
			{
				TraceID: "abc123",
				Spans: []jaegerSpan{
					{
						TraceID:       "abc123",
						SpanID:        "span001",
						OperationName: "HTTP GET /health",
						StartTime:     1700000000000000,
						Duration:      1500,
						ProcessID:     "p1",
						Tags: []jaegerTag{
							{Key: "span.kind", Type: "string", Value: "server"},
						},
					},
				},
				Processes: map[string]jaegerProcess{
					"p1": {ServiceName: "test-service"},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case servicesAPIPath:
			if err := json.NewEncoder(writer).Encode(servicesResp); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		case tracesAPIPath:
			if err := json.NewEncoder(writer).Encode(tracesResp); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	database := openTestDB(t)
	ctx := context.Background()

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	if err := ExportTraces(ctx, database, srv.URL, nil); err != nil {
		t.Fatalf("ExportTraces: %v", err)
	}

	count := queryCount(t, database, "SELECT COUNT(*) FROM spans")
	if count != 1 {
		t.Errorf("got %d span rows, want 1", count)
	}

	verifyExportedSpan(t, database)
}

// verifyExportedSpan checks that the exported span row matches expected values.
func verifyExportedSpan(t *testing.T, database *sql.DB) {
	t.Helper()

	var traceID, spanID, operation, serviceName string

	err := database.QueryRow("SELECT trace_id, span_id, operation, service_name FROM spans").
		Scan(&traceID, &spanID, &operation, &serviceName)
	if err != nil {
		t.Fatalf("querying span: %v", err)
	}

	if traceID != "abc123" {
		t.Errorf("trace_id = %q, want %q", traceID, "abc123")
	}

	if spanID != "span001" {
		t.Errorf("span_id = %q, want %q", spanID, "span001")
	}

	if operation != "HTTP GET /health" {
		t.Errorf("operation = %q, want %q", operation, "HTTP GET /health")
	}

	if serviceName != "test-service" {
		t.Errorf("service_name = %q, want %q", serviceName, "test-service")
	}

	var durationUS int64
	if err := database.QueryRow("SELECT duration_us FROM spans").Scan(&durationUS); err != nil {
		t.Fatalf("querying duration: %v", err)
	}

	if durationUS != 1500 {
		t.Errorf("duration_us = %d, want 1500", durationUS)
	}
}

// emptyServicesHandler returns a Jaeger services response with no services.
func emptyServicesHandler(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case servicesAPIPath:
		if err := json.NewEncoder(writer).Encode(jaegerServicesResponse{Data: []string{}}); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	default:
		http.Error(writer, "not found", http.StatusNotFound)
	}
}

func TestPartialFailureHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		metricsHandler http.HandlerFunc
		tracesHandler  http.HandlerFunc
		skipMetrics    bool
		skipTraces     bool
		skipLogs       bool
		skipProfiles   bool
		wantStatus     ExportStatus
		wantErrCount   int
	}{
		{
			name: "all succeed",
			metricsHandler: func(writer http.ResponseWriter, _ *http.Request) {
				if _, err := fmt.Fprintln(writer, `{"metric":{"__name__":"up"},"values":[1],"timestamps":[1700000000000]}`); err != nil {
					http.Error(writer, err.Error(), http.StatusInternalServerError)
				}
			},
			tracesHandler: emptyServicesHandler,
			skipLogs:      true,
			skipProfiles:  true,
			wantStatus:    StatusSuccess,
			wantErrCount:  0,
		},
		{
			name: "metrics fails traces succeeds",
			metricsHandler: func(writer http.ResponseWriter, _ *http.Request) {
				http.Error(writer, "internal error", http.StatusInternalServerError)
			},
			tracesHandler: emptyServicesHandler,
			skipLogs:      true,
			skipProfiles:  true,
			wantStatus:    StatusPartial,
			wantErrCount:  1,
		},
		{
			name: "all skip produces success",
			metricsHandler: func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("metrics handler should not be called")
			},
			tracesHandler: func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("traces handler should not be called")
			},
			skipMetrics:  true,
			skipTraces:   true,
			skipLogs:     true,
			skipProfiles: true,
			wantStatus:   StatusSuccess,
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			metricsSrv := httptest.NewServer(tt.metricsHandler)
			defer metricsSrv.Close()

			tracesSrv := httptest.NewServer(tt.tracesHandler)
			defer tracesSrv.Close()

			outputDir := t.TempDir()

			opts := ExportOptions{
				OutputDir:    outputDir,
				MetricsURL:   metricsSrv.URL,
				TracesURL:    tracesSrv.URL,
				LogsURL:      "http://127.0.0.1:1",
				ParcaURL:     "http://127.0.0.1:1",
				SkipMetrics:  tt.skipMetrics,
				SkipTraces:   tt.skipTraces,
				SkipLogs:     tt.skipLogs,
				SkipProfiles: tt.skipProfiles,
			}

			ctx := context.Background()
			result := RunExport(ctx, &opts)

			if result.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", result.Status, tt.wantStatus)
			}

			if len(result.Errors) != tt.wantErrCount {
				t.Errorf("error count = %d, want %d; errors: %v", len(result.Errors), tt.wantErrCount, result.Errors)
			}
		})
	}
}

func TestSanitizeProfileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
			expected: "process_cpu_cpu_nanoseconds_cpu_nanoseconds",
		},
		{
			input:    "memory/alloc_space",
			expected: "memory_alloc_space",
		},
		{
			input:    "simple",
			expected: "simple",
		},
		{
			input:    "with spaces:and/mixed",
			expected: "with_spaces_and_mixed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got := sanitizeProfileName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeProfileName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNilIfEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantNil bool
	}{
		{name: "empty string returns nil", input: "", wantNil: true},
		{name: "non-empty string returns string", input: "value", wantNil: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := nilIfEmpty(tt.input)
			if tt.wantNil && got != nil {
				t.Errorf("nilIfEmpty(%q) = %v, want nil", tt.input, got)
			}

			if !tt.wantNil && got != tt.input {
				t.Errorf("nilIfEmpty(%q) = %v, want %q", tt.input, got, tt.input)
			}
		})
	}
}

// verifyStandardExportURL validates the fully constructed metrics export URL.
func verifyStandardExportURL(t *testing.T, result string) {
	t.Helper()

	if result == "" {
		t.Fatal("empty result")
	}

	parsedURL, err := url.Parse(result)
	if err != nil {
		t.Fatalf("parsing result URL: %v", err)
	}

	if parsedURL.Path != metricsExportPath {
		t.Errorf("path = %q, want %q", parsedURL.Path, metricsExportPath)
	}

	matchParam := parsedURL.Query().Get("match[]")
	if matchParam != `{__name__!=""}` {
		t.Errorf("match[] = %q, want %q", matchParam, `{__name__!=""}`)
	}

	startParam := parsedURL.Query().Get("start")
	if startParam != "0" {
		t.Errorf("start = %q, want %q", startParam, "0")
	}
}

// verifyExportURLPath validates that the URL has the expected path.
func verifyExportURLPath(t *testing.T, result, expectedPath string) {
	t.Helper()

	parsedURL, err := url.Parse(result)
	if err != nil {
		t.Fatalf("parsing result URL: %v", err)
	}

	if parsedURL.Path != expectedPath {
		t.Errorf("path = %q, want %q", parsedURL.Path, expectedPath)
	}
}

func TestBuildMetricsExportURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		wantErr bool
		check   func(t *testing.T, result string)
	}{
		{
			name:    "standard URL",
			baseURL: "http://localhost:8428",
			check:   verifyStandardExportURL,
		},
		{
			name:    "URL with trailing slash",
			baseURL: "http://localhost:8428/",
			check: func(t *testing.T, result string) {
				t.Helper()
				verifyExportURLPath(t, result, metricsExportPath)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := buildMetricsExportURL(tt.baseURL)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestExportProfiles_Success(t *testing.T) {
	t.Parallel()

	profileData := []byte("fake-pprof-data")

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case parcaProfileTypesPath:
			if err := json.NewEncoder(writer).Encode(map[string]any{
				"profileTypes": []string{"process_cpu:cpu:nanoseconds:cpu:nanoseconds"},
			}); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		case parcaQueryPath:
			if err := json.NewEncoder(writer).Encode(map[string]any{
				"report": map[string]string{"pprof": base64.StdEncoding.EncodeToString(profileData)},
			}); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	outputDir := t.TempDir()

	err := ExportProfiles(context.Background(), srv.URL, outputDir, nil)
	if err != nil {
		t.Fatalf("ExportProfiles: %v", err)
	}

	verifyProfileOutput(t, filepath.Join(outputDir, "profiles"), profileData)
}

// verifyProfileOutput checks that exactly one profile file exists with the expected content.
func verifyProfileOutput(t *testing.T, profilesDir string, want []byte) {
	t.Helper()

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		t.Fatalf("reading profiles dir: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d profile files, want 1", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(profilesDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("reading profile file: %v", err)
	}

	if !bytes.Equal(data, want) {
		t.Errorf("profile data = %q, want %q", data, want)
	}
}

func TestExportProfiles_ParcaUnreachable(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()

	err := ExportProfiles(context.Background(), "http://127.0.0.1:1", outputDir, nil)
	if err != nil {
		t.Errorf("expected nil error for unreachable Parca, got: %v", err)
	}
}

func TestExportProfiles_NoProfileTypes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(writer).Encode(map[string]any{"profileTypes": []string{}}); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	outputDir := t.TempDir()

	err := ExportProfiles(context.Background(), srv.URL, outputDir, nil)
	if err != nil {
		t.Fatalf("ExportProfiles: %v", err)
	}
}

func TestExportProfiles_DownloadFailureContinues(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case parcaProfileTypesPath:
			if err := json.NewEncoder(writer).Encode(map[string]any{
				"profileTypes": []string{"cpu", "memory"},
			}); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		case parcaQueryPath:
			http.Error(writer, "server error", http.StatusInternalServerError)
		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	outputDir := t.TempDir()

	err := ExportProfiles(context.Background(), srv.URL, outputDir, nil)
	if err != nil {
		t.Fatalf("ExportProfiles should not return error on download failure: %v", err)
	}
}

func TestFetchProfileTypes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"profileTypes": []string{"cpu", "memory", "goroutine"},
		}); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	types, err := fetchProfileTypes(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatalf("fetchProfileTypes: %v", err)
	}

	if len(types) != 3 {
		t.Errorf("got %d types, want 3", len(types))
	}
}

func TestFetchProfileTypes_RejectsHTML(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := fmt.Fprintln(writer, "<!doctype html><html><body>spa</body></html>"); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	_, err := fetchProfileTypes(context.Background(), nil, srv.URL)
	if err == nil {
		t.Fatal("expected error for HTML response, got nil")
	}
}

func TestFetchProfileTypes_FallbackToLegacyAPI(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case parcaProfileTypesPath:
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			if _, err := fmt.Fprintln(writer, "<!doctype html><html><body>spa</body></html>"); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		case legacyProfileTypesPath:
			if request.Method != http.MethodPost {
				http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)

				return
			}

			if err := json.NewEncoder(writer).Encode(map[string]any{
				"types": []parcaProfileType{{Name: "cpu"}, {Name: "heap"}},
			}); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	types, err := fetchProfileTypes(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatalf("fetchProfileTypes: %v", err)
	}

	if len(types) != 2 {
		t.Fatalf("got %d types, want 2", len(types))
	}

	if types[0] != "cpu" || types[1] != "heap" {
		t.Fatalf("unexpected profile types: %v", types)
	}
}

func TestFetchProfileTypes_FallbackFromGatewayToV1API(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case parcaGatewayProfileTypesPath:
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			if _, err := fmt.Fprintln(writer, "<!doctype html><html><body>spa</body></html>"); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		case parcaProfileTypesPath:
			if request.Method != http.MethodGet {
				http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)

				return
			}

			if err := json.NewEncoder(writer).Encode(map[string]any{
				"profileTypes": []string{"cpu", "heap"},
			}); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	types, err := fetchProfileTypes(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatalf("fetchProfileTypes: %v", err)
	}

	if len(types) != 2 {
		t.Fatalf("got %d types, want 2", len(types))
	}

	if types[0] != "cpu" || types[1] != "heap" {
		t.Fatalf("unexpected profile types: %v", types)
	}
}

func TestDownloadProfile(t *testing.T) {
	t.Parallel()

	profileData := []byte("compressed-pprof-bytes")

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(writer).Encode(parcaQueryResponse{
			Pprof: base64.StdEncoding.EncodeToString(profileData),
		}); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	profilesDir := t.TempDir()

	err := downloadProfile(context.Background(), nil, srv.URL, parcaAPI{queryPath: parcaQueryPath}, "process_cpu:cpu:nanoseconds", profilesDir)
	if err != nil {
		t.Fatalf("downloadProfile: %v", err)
	}

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		t.Fatalf("reading profiles dir: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d files, want 1", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(profilesDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("reading profile: %v", err)
	}

	if !bytes.Equal(data, profileData) {
		t.Errorf("profile data = %q, want %q", data, profileData)
	}
}

func grpcWebFrameForTest(frameType byte, payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = frameType
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)

	return frame
}

func buildProfileTypesResponseProtoForTest(names ...string) []byte {
	resp := []byte{}
	for _, name := range names {
		entry := []byte{}
		entry = protowire.AppendTag(entry, 1, protowire.BytesType)
		entry = protowire.AppendString(entry, name)

		resp = protowire.AppendTag(resp, 1, protowire.BytesType)
		resp = protowire.AppendBytes(resp, entry)
	}

	return resp
}

func buildQueryResponseProtoForTest(pprof []byte) []byte {
	resp := []byte{}
	resp = protowire.AppendTag(resp, 6, protowire.BytesType)
	resp = protowire.AppendBytes(resp, pprof)

	return resp
}

func TestParseGRPCWebResponse(t *testing.T) {
	t.Parallel()

	msg := []byte{0x0a, 0x01, 0x01}
	trailers := []byte("grpc-status: 0\r\ngrpc-message: \r\n")
	body := append(grpcWebFrameForTest(0x00, msg), grpcWebFrameForTest(0x80, trailers)...)

	parsed, status, grpcMsg, err := parseGRPCWebResponse(body)
	if err != nil {
		t.Fatalf("parseGRPCWebResponse: %v", err)
	}

	if status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}

	if grpcMsg != "" {
		t.Fatalf("grpc message = %q, want empty", grpcMsg)
	}

	if !bytes.Equal(parsed, msg) {
		t.Fatalf("message payload mismatch: got %v want %v", parsed, msg)
	}
}

func TestFetchProfileTypes_GRPCWeb(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != parcaGRPCWebProfileTypesPath {
			http.Error(writer, "not found", http.StatusNotFound)

			return
		}

		payload := buildProfileTypesResponseProtoForTest("cpu", "heap")
		trailers := []byte("grpc-status: 0\r\n")
		responseBody := append(grpcWebFrameForTest(0x00, payload), grpcWebFrameForTest(0x80, trailers)...)

		writer.Header().Set("Content-Type", "application/grpc-web+proto")
		writer.WriteHeader(http.StatusOK)
		if _, err := writer.Write(responseBody); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	types, err := fetchProfileTypes(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatalf("fetchProfileTypes: %v", err)
	}

	if len(types) != 2 {
		t.Fatalf("got %d types, want 2", len(types))
	}

	if types[0] != "cpu" || types[1] != "heap" {
		t.Fatalf("unexpected profile types: %v", types)
	}
}

func TestDownloadProfile_GRPCWeb(t *testing.T) {
	t.Parallel()

	pprofData := []byte("grpc-web-pprof")

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != parcaGRPCWebQueryPath {
			http.Error(writer, "not found", http.StatusNotFound)

			return
		}

		payload := buildQueryResponseProtoForTest(pprofData)
		trailers := []byte("grpc-status: 0\r\n")
		responseBody := append(grpcWebFrameForTest(0x00, payload), grpcWebFrameForTest(0x80, trailers)...)

		writer.Header().Set("Content-Type", "application/grpc-web+proto")
		writer.WriteHeader(http.StatusOK)
		if _, err := writer.Write(responseBody); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	profilesDir := t.TempDir()
	err := downloadProfile(
		context.Background(),
		nil,
		srv.URL,
		parcaAPI{grpcWeb: true, queryPath: parcaGRPCWebQueryPath},
		"cpu",
		profilesDir,
	)
	if err != nil {
		t.Fatalf("downloadProfile: %v", err)
	}

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		t.Fatalf("reading profiles dir: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d files, want 1", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(profilesDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("reading profile: %v", err)
	}

	if !bytes.Equal(data, pprofData) {
		t.Errorf("profile data = %q, want %q", data, pprofData)
	}
}

func TestHttpGetStream(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		resp, err := httpGetStream(context.Background(), nil, srv.URL)
		if err != nil {
			t.Fatalf("httpGetStream: %v", err)
		}

		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing response body: %v", err)
		}
	})

	t.Run("non-200 returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := httpGetStream(context.Background(), nil, srv.URL) //nolint:bodyclose // body closed by httpGetStream on non-200
		if err == nil {
			t.Fatal("expected error for 404, got nil")
		}

		if !errors.Is(err, ErrUnexpectedHTTPStatus) {
			t.Errorf("expected ErrUnexpectedHTTPStatus, got: %v", err)
		}
	})
}

func TestCloseResourceErrorPath(t *testing.T) {
	t.Parallel()

	// closeResource should not panic on a closer that returns an error.
	closeResource("test-error-closer", &failingCloser{})
}

type failingCloser struct{}

func (f *failingCloser) Close() error {
	return errors.New("close failed")
}

func TestRunExport_InvalidOutputDir(t *testing.T) {
	t.Parallel()

	result := RunExport(context.Background(), &ExportOptions{
		OutputDir:    "/dev/null/invalid",
		SkipMetrics:  true,
		SkipTraces:   true,
		SkipLogs:     true,
		SkipProfiles: true,
	})

	if result.Status != StatusFailure {
		t.Errorf("status = %d, want StatusFailure", result.Status)
	}

	if len(result.Errors) == 0 {
		t.Error("expected errors, got none")
	}
}

func TestRunExport_AllSkipped(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()

	result := RunExport(context.Background(), &ExportOptions{
		OutputDir:    outputDir,
		SkipMetrics:  true,
		SkipTraces:   true,
		SkipLogs:     true,
		SkipProfiles: true,
	})

	if result.Status != StatusSuccess {
		t.Errorf("status = %d, want StatusSuccess", result.Status)
	}

	if len(result.Errors) != 0 {
		t.Errorf("got %d errors, want 0", len(result.Errors))
	}

	if result.OutputPath == "" {
		t.Error("OutputPath should be set")
	}
}

func TestFetchJSON_NonOKStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := fetchJSON[testJSONResponse](context.Background(), nil, srv.URL, "/test")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}

	if !errors.Is(err, ErrUnexpectedHTTPStatus) {
		t.Errorf("expected ErrUnexpectedHTTPStatus, got: %v", err)
	}
}

func TestFetchJSON_InvalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprintln(writer, "not-json"); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	_, err := fetchJSON[testJSONResponse](context.Background(), nil, srv.URL, "/test")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestCreateSchema_DuplicateTableError(t *testing.T) {
	t.Parallel()

	database := openTestDB(t)
	ctx := context.Background()

	// First call succeeds.
	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("first CreateSchema: %v", err)
	}

	// Second call fails because tables already exist.
	if err := CreateSchema(ctx, database); err == nil {
		t.Error("expected error for duplicate table creation, got nil")
	}
}

func TestRunExport_SuccessWithSkips(t *testing.T) {
	t.Parallel()

	// Create a metrics server that returns valid data and skip traces/profiles.
	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != metricsExportPath {
			http.Error(writer, "not found", http.StatusNotFound)

			return
		}

		if _, err := fmt.Fprintln(writer, `{"metric":{"__name__":"test_metric","job":"test"},"values":[42.0],"timestamps":[1700000000000]}`); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	outputDir := t.TempDir()

	result := RunExport(context.Background(), &ExportOptions{
		OutputDir:    outputDir,
		MetricsURL:   srv.URL,
		SkipTraces:   true,
		SkipLogs:     true,
		SkipProfiles: true,
	})

	if result.Status != StatusSuccess {
		t.Errorf("status = %d, want StatusSuccess; errors: %v", result.Status, result.Errors)
	}

	if result.OutputPath == "" {
		t.Error("OutputPath should be set")
	}
}

func TestFetchServices(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(writer).Encode(jaegerServicesResponse{Data: []string{"svc-a", "svc-b"}}); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	services, err := fetchServices(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatalf("fetchServices: %v", err)
	}

	if len(services) != 2 {
		t.Errorf("got %d services, want 2", len(services))
	}
}

func TestFetchServices_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := fetchServices(context.Background(), nil, srv.URL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExportTraces_NoServices(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(writer).Encode(jaegerServicesResponse{Data: []string{}}); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	database := openTestDB(t)
	ctx := context.Background()

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	if err := ExportTraces(ctx, database, srv.URL, nil); err != nil {
		t.Fatalf("ExportTraces: %v", err)
	}

	count := queryCount(t, database, "SELECT COUNT(*) FROM spans")
	if count != 0 {
		t.Errorf("got %d spans, want 0", count)
	}
}

func TestExportTraces_TraceFetchFailsContinues(t *testing.T) {
	t.Parallel()

	// Services endpoint returns one service, but traces endpoint returns an error.
	// ExportTraces should log a warning and return nil (no spans collected).
	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case servicesAPIPath:
			if err := json.NewEncoder(writer).Encode(jaegerServicesResponse{Data: []string{"failing-service"}}); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
		case tracesAPIPath:
			http.Error(writer, "server error", http.StatusInternalServerError)
		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	database := openTestDB(t)
	ctx := context.Background()

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	// Should not return error — trace fetch failure for a service is non-fatal.
	if err := ExportTraces(ctx, database, srv.URL, nil); err != nil {
		t.Fatalf("ExportTraces: %v", err)
	}

	count := queryCount(t, database, "SELECT COUNT(*) FROM spans")
	if count != 0 {
		t.Errorf("got %d spans, want 0", count)
	}
}

func TestExportTraces_ServiceFetchError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	database := openTestDB(t)
	ctx := context.Background()

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	err := ExportTraces(ctx, database, srv.URL, nil)
	if err == nil {
		t.Fatal("expected error when service fetch fails, got nil")
	}
}

func TestExportMetrics_EmptyResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		// Return empty body — valid but no metric lines.
		writer.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	database := openTestDB(t)
	ctx := context.Background()

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	err := ExportMetrics(ctx, database, srv.URL, nil)
	if err != nil {
		t.Fatalf("ExportMetrics: %v", err)
	}

	count := queryCount(t, database, "SELECT COUNT(*) FROM metrics")
	if count != 0 {
		t.Errorf("got %d rows, want 0", count)
	}
}

func TestExportMetrics_MalformedLine(t *testing.T) {
	t.Parallel()

	validLine := `{"metric":{"__name__":"up"},"values":[1],"timestamps":[1700000000000]}`

	assertMalformedLineSkipped(t, validLine, "metrics", ExportMetrics)
}

func TestResolveProcess_MissingProcessID(t *testing.T) {
	t.Parallel()

	trace := &jaegerTrace{
		Processes: map[string]jaegerProcess{
			"p1": {ServiceName: "known-service"},
		},
	}
	span := &jaegerSpan{ProcessID: "p99"} // Not in Processes map.

	flattenedSpan := &flatSpan{
		Attributes: make(duckdb.Map),
	}

	resolveProcess(trace, span, flattenedSpan)

	if flattenedSpan.ServiceName != "" {
		t.Errorf("ServiceName = %q, want empty for missing process", flattenedSpan.ServiceName)
	}

	if flattenedSpan.ResourceAttrs != nil {
		t.Errorf("ResourceAttrs should be nil for missing process, got %v", flattenedSpan.ResourceAttrs)
	}
}

func TestHttpGetStream_InvalidURL(t *testing.T) {
	t.Parallel()

	_, err := httpGetStream(context.Background(), nil, "://invalid") //nolint:bodyclose // no body on error
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestFetchJSON_InvalidBaseURL(t *testing.T) {
	t.Parallel()

	_, err := fetchJSON[testJSONResponse](context.Background(), nil, "://invalid", "/test")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestParseLogEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		wantMessage  string
		wantStream   string
		wantSeverity string
		wantFields   int
		wantErr      bool
	}{
		{
			name:         "full entry with all fields",
			input:        `{"_msg":"hello world","_time":"2026-01-15T10:30:00Z","_stream":"{app=\"test\"}","severityText":"INFO","custom_field":"value"}`,
			wantMessage:  "hello world",
			wantStream:   `{app="test"}`,
			wantSeverity: "INFO",
			wantFields:   1,
		},
		{
			name:        "minimal entry with only _msg and _time",
			input:       `{"_msg":"minimal","_time":"2026-01-15T10:30:00Z"}`,
			wantMessage: "minimal",
			wantFields:  0,
		},
		{
			name:         "severity from level key",
			input:        `{"_msg":"leveled","_time":"2026-01-15T10:30:00Z","level":"ERROR"}`,
			wantMessage:  "leveled",
			wantSeverity: "ERROR",
			wantFields:   0,
		},
		{
			name:         "severity from severity key",
			input:        `{"_msg":"sev","_time":"2026-01-15T10:30:00Z","severity":"WARN"}`,
			wantMessage:  "sev",
			wantSeverity: "WARN",
			wantFields:   0,
		},
		{
			name:         "severityText takes priority over level",
			input:        `{"_msg":"multi","_time":"2026-01-15T10:30:00Z","severityText":"DEBUG","level":"INFO"}`,
			wantMessage:  "multi",
			wantSeverity: "DEBUG",
			wantFields:   1, // "level" remains as a field since severityText was used
		},
		{
			name:        "extra custom fields collected",
			input:       `{"_msg":"rich","_time":"2026-01-15T10:30:00Z","host":"node1","region":"eu","pod":"app-1"}`,
			wantMessage: "rich",
			wantFields:  3,
		},
		{
			name:    "invalid JSON",
			input:   `{broken`,
			wantErr: true,
		},
		{
			name:    "missing _time field",
			input:   `{"_msg":"no time"}`,
			wantErr: true,
		},
		{
			name:    "invalid timestamp format",
			input:   `{"_msg":"bad time","_time":"not-a-time"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entry, err := ParseLogEntry([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if entry.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", entry.Message, tt.wantMessage)
			}

			if entry.Stream != tt.wantStream {
				t.Errorf("Stream = %q, want %q", entry.Stream, tt.wantStream)
			}

			if entry.Severity != tt.wantSeverity {
				t.Errorf("Severity = %q, want %q", entry.Severity, tt.wantSeverity)
			}

			if len(entry.Fields) != tt.wantFields {
				t.Errorf("Fields count = %d, want %d; fields: %v", len(entry.Fields), tt.wantFields, entry.Fields)
			}
		})
	}
}

func TestExportLogs_Integration(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"_msg":"first log message","_time":"2026-01-15T10:30:00Z","_stream":"{app=\"test\"}","severityText":"INFO","host":"node1"}`,
		`{"_msg":"second log message","_time":"2026-01-15T10:30:01Z","_stream":"{app=\"test\"}","severityText":"ERROR"}`,
		`{"_msg":"third log message","_time":"2026-01-15T10:30:02Z"}`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != logsQueryPath {
			http.Error(writer, "not found", http.StatusNotFound)

			return
		}

		for _, line := range lines {
			if _, err := fmt.Fprintln(writer, line); err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)

				return
			}
		}
	}))
	defer srv.Close()

	database := openTestDB(t)
	ctx := context.Background()

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	if err := ExportLogs(ctx, database, srv.URL, nil); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}

	count := queryCount(t, database, "SELECT COUNT(*) FROM logs")
	if count != 3 {
		t.Errorf("got %d log rows, want 3", count)
	}

	infoCount := queryCount(t, database, "SELECT COUNT(*) FROM logs WHERE severity = 'INFO'")
	if infoCount != 1 {
		t.Errorf("INFO logs = %d, want 1", infoCount)
	}

	errorCount := queryCount(t, database, "SELECT COUNT(*) FROM logs WHERE severity = 'ERROR'")
	if errorCount != 1 {
		t.Errorf("ERROR logs = %d, want 1", errorCount)
	}

	var msg string
	if err := database.QueryRow("SELECT message FROM logs WHERE severity = 'INFO'").Scan(&msg); err != nil {
		t.Fatalf("querying INFO log message: %v", err)
	}

	if msg != "first log message" {
		t.Errorf("INFO log message = %q, want %q", msg, "first log message")
	}
}

func TestExportLogs_EmptyResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	database := openTestDB(t)
	ctx := context.Background()

	if err := CreateSchema(ctx, database); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	err := ExportLogs(ctx, database, srv.URL, nil)
	if err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}

	count := queryCount(t, database, "SELECT COUNT(*) FROM logs")
	if count != 0 {
		t.Errorf("got %d rows, want 0", count)
	}
}

func TestExportLogs_MalformedLine(t *testing.T) {
	t.Parallel()

	validLine := `{"_msg":"valid entry","_time":"2026-01-15T10:30:00Z"}`

	assertMalformedLineSkipped(t, validLine, "logs", ExportLogs)
}

func TestBuildLogsExportURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		wantErr bool
		check   func(t *testing.T, result string)
	}{
		{
			name:    "standard URL",
			baseURL: "http://localhost:9428",
			check: func(t *testing.T, result string) {
				t.Helper()

				parsedURL, err := url.Parse(result)
				if err != nil {
					t.Fatalf("parsing result URL: %v", err)
				}

				if parsedURL.Path != logsQueryPath {
					t.Errorf("path = %q, want %q", parsedURL.Path, logsQueryPath)
				}

				queryParam := parsedURL.Query().Get("query")
				if queryParam != "*" {
					t.Errorf("query = %q, want %q", queryParam, "*")
				}

				startParam := parsedURL.Query().Get("start")
				if startParam != "0" {
					t.Errorf("start = %q, want %q", startParam, "0")
				}
			},
		},
		{
			name:    "URL with trailing slash",
			baseURL: "http://localhost:9428/",
			check: func(t *testing.T, result string) {
				t.Helper()
				verifyExportURLPath(t, result, logsQueryPath)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := buildLogsExportURL(tt.baseURL)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestDetermineStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		attempted int
		succeeded int
		want      ExportStatus
	}{
		{name: "none attempted", attempted: 0, succeeded: 0, want: StatusSuccess},
		{name: "all succeed", attempted: 3, succeeded: 3, want: StatusSuccess},
		{name: "partial", attempted: 3, succeeded: 1, want: StatusPartial},
		{name: "all fail", attempted: 3, succeeded: 0, want: StatusFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := determineStatus(tt.attempted, tt.succeeded)
			if got != tt.want {
				t.Errorf("determineStatus(%d, %d) = %d, want %d", tt.attempted, tt.succeeded, got, tt.want)
			}
		})
	}
}
