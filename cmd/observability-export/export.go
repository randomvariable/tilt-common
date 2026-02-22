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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

// ExportOptions contains configuration for the export operation.
type ExportOptions struct {
	OutputDir    string
	MetricsURL   string
	TracesURL    string
	LogsURL      string
	ParcaURL     string
	SkipMetrics  bool
	SkipTraces   bool
	SkipLogs     bool
	SkipProfiles bool
	// HTTPClient is used for all outbound requests. If nil, http.DefaultClient
	// is used. Set by SetupPortForward when port-forwarding is enabled.
	HTTPClient *http.Client
}

// ExportStatus represents the overall result of the export operation.
type ExportStatus int

const (
	// StatusSuccess means all requested exports completed without error.
	StatusSuccess ExportStatus = iota
	// StatusPartial means at least one export succeeded but others failed.
	StatusPartial
	// StatusFailure means no exports succeeded.
	StatusFailure
)

// ExportResult contains the outcome of an export operation.
type ExportResult struct {
	Status     ExportStatus
	OutputPath string
	Errors     []error
}

const (
	createMetricsTable = `CREATE TABLE metrics (
    timestamp       TIMESTAMP NOT NULL,
    metric_name     VARCHAR NOT NULL,
    labels          MAP(VARCHAR, VARCHAR),
    value           DOUBLE NOT NULL,
    export_time     TIMESTAMP
);`

	createLogsTable = `CREATE TABLE logs (
    timestamp       TIMESTAMP NOT NULL,
    message         VARCHAR NOT NULL,
    stream          VARCHAR,
    severity        VARCHAR,
    fields          MAP(VARCHAR, VARCHAR),
    export_time     TIMESTAMP
);`

	createSpansTable = `CREATE TABLE spans (
    trace_id        VARCHAR NOT NULL,
    span_id         VARCHAR NOT NULL,
    parent_span_id  VARCHAR,
    operation       VARCHAR NOT NULL,
    service_name    VARCHAR NOT NULL,
    span_kind       VARCHAR,
    start_time      TIMESTAMP NOT NULL,
    end_time        TIMESTAMP NOT NULL,
    duration_us     BIGINT NOT NULL,
    status_code     VARCHAR,
    status_message  VARCHAR,
    attributes      MAP(VARCHAR, VARCHAR),
    resource_attrs  MAP(VARCHAR, VARCHAR),
    export_time     TIMESTAMP
);`
)

// TimestampFormat is the Go time format used for the DuckDB filename.
const TimestampFormat = "2006-01-02T150405"

// DBFilename returns the timestamped DuckDB filename for the given time.
func DBFilename(t time.Time) string {
	return fmt.Sprintf("telemetry-%s.duckdb", t.Format(TimestampFormat))
}

// CreateSchema creates the metrics and spans tables in the provided database.
func CreateSchema(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, createMetricsTable); err != nil {
		return fmt.Errorf("creating metrics table: %w", err)
	}

	if _, err := database.ExecContext(ctx, createSpansTable); err != nil {
		return fmt.Errorf("creating spans table: %w", err)
	}

	if _, err := database.ExecContext(ctx, createLogsTable); err != nil {
		return fmt.Errorf("creating logs table: %w", err)
	}

	return nil
}

// telemetryExporter holds the configuration for a single telemetry export task.
type telemetryExporter struct {
	name string
	skip bool
	url  string
	fn   func() error
}

// RunExport orchestrates the full export pipeline: creates the output directory,
// initialises the DuckDB database with the telemetry schema, and runs each
// exporter that has not been skipped. Partial failures are tolerated -- the
// function continues with remaining exporters when one fails.
func RunExport(ctx context.Context, opts *ExportOptions) ExportResult {
	result := ExportResult{}

	if err := os.MkdirAll(opts.OutputDir, dirPermissions); err != nil {
		result.Status = StatusFailure
		result.Errors = append(result.Errors, fmt.Errorf("creating output directory %q: %w", opts.OutputDir, err))

		return result
	}

	dbPath := filepath.Join(opts.OutputDir, DBFilename(time.Now()))
	result.OutputPath = dbPath

	database, err := sql.Open("duckdb", dbPath)
	if err != nil {
		result.Status = StatusFailure
		result.Errors = append(result.Errors, fmt.Errorf("opening DuckDB at %q: %w", dbPath, err))

		return result
	}

	defer closeResource("DuckDB", database)

	if err := CreateSchema(ctx, database); err != nil {
		result.Status = StatusFailure
		result.Errors = append(result.Errors, fmt.Errorf("creating schema: %w", err))

		return result
	}

	exporters := []telemetryExporter{
		{name: "metrics", skip: opts.SkipMetrics, url: opts.MetricsURL, fn: func() error {
			return ExportMetrics(ctx, database, opts.MetricsURL, opts.HTTPClient)
		}},
		{name: "traces", skip: opts.SkipTraces, url: opts.TracesURL, fn: func() error {
			return ExportTraces(ctx, database, opts.TracesURL, opts.HTTPClient)
		}},
		{name: "logs", skip: opts.SkipLogs, url: opts.LogsURL, fn: func() error {
			return ExportLogs(ctx, database, opts.LogsURL, opts.HTTPClient)
		}},
		{name: "profiles", skip: opts.SkipProfiles, url: opts.ParcaURL, fn: func() error {
			return ExportProfiles(ctx, opts.ParcaURL, opts.OutputDir, opts.HTTPClient)
		}},
	}

	var attempted, succeeded int

	for _, exp := range exporters {
		if exp.skip {
			continue
		}

		attempted++

		slog.Info("exporting "+exp.name, "url", exp.url)

		if err := exp.fn(); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("%s export: %w", exp.name, err))
		} else {
			succeeded++

			slog.Info(exp.name + " export complete")
		}
	}

	result.Status = determineStatus(attempted, succeeded)

	return result
}

// determineStatus computes the overall export status from the number of
// attempted and successful exports.
func determineStatus(attempted, succeeded int) ExportStatus {
	switch {
	case attempted == 0:
		return StatusSuccess
	case succeeded == attempted:
		return StatusSuccess
	case succeeded > 0:
		return StatusPartial
	default:
		return StatusFailure
	}
}
