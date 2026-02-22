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
	"bufio"
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

// vmExportLine represents a single JSON line from the VictoriaMetrics
// /api/v1/export endpoint. Each line contains a metric name with labels,
// and parallel arrays of values and timestamps.
type vmExportLine struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

// ExportMetrics fetches all metrics from VictoriaMetrics via the /api/v1/export
// endpoint and writes them to the metrics table in the DuckDB database. The
// export endpoint returns JSON lines where each line contains a full time series
// with parallel arrays of values and timestamps.
func ExportMetrics(ctx context.Context, database *sql.DB, metricsURL string, client *http.Client) error {
	exportURL, err := buildMetricsExportURL(metricsURL)
	if err != nil {
		return fmt.Errorf("building export URL: %w", err)
	}

	resp, err := httpGetStream(ctx, client, exportURL)
	if err != nil {
		return err
	}

	defer closeResource("metrics response body", resp.Body)

	conn, appender, err := newTableAppender(ctx, database, "metrics")
	if err != nil {
		return err
	}

	defer closeResource("metrics connection", conn)
	defer closeResource("metrics appender", appender)

	exportTime := time.Now().UTC()

	var totalRows int

	scanner := bufio.NewScanner(resp.Body)
	// Increase scanner buffer for large lines (16 MiB).
	const maxScannerBuf = 16 * 1024 * 1024
	scanner.Buffer(make([]byte, 0, maxScannerBuf), maxScannerBuf)

	for scanner.Scan() {
		var line vmExportLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			slog.Warn("skipping malformed metrics line", "error", err)

			continue
		}

		rows, err := appendMetricRows(appender, &line, exportTime)
		if err != nil {
			return err
		}

		totalRows += rows
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading metrics response: %w", err)
	}

	if err := appender.Flush(); err != nil {
		return fmt.Errorf("flushing metrics appender: %w", err)
	}

	slog.Info("metrics rows written", "count", totalRows)

	return nil
}

// appendMetricRows writes all value/timestamp pairs from a single metric line
// to the appender and returns the number of rows written.
func appendMetricRows(appender *duckdb.Appender, line *vmExportLine, exportTime time.Time) (int, error) {
	metricName := line.Metric["__name__"]

	labels := make(duckdb.Map)

	for k, v := range line.Metric {
		if k != "__name__" {
			labels[k] = v
		}
	}

	var count int

	for i := range line.Values {
		if i >= len(line.Timestamps) {
			break
		}

		ts := time.UnixMilli(line.Timestamps[i]).UTC()

		if err := appender.AppendRow(ts, metricName, labels, line.Values[i], exportTime); err != nil {
			return count, fmt.Errorf("appending metric row: %w", err)
		}

		count++
	}

	return count, nil
}

// buildMetricsExportURL constructs the VictoriaMetrics export API URL with
// the match[] parameter to select all metrics from the beginning of time.
func buildMetricsExportURL(baseURL string) (string, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parsing base URL %q: %w", baseURL, err)
	}

	parsedURL.Path = metricsExportPath

	query := parsedURL.Query()
	query.Set("match[]", `{__name__!=""}`)
	query.Set("start", "0")
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

// ParseMetricsLine parses a single JSON line from the VictoriaMetrics export
// endpoint. This is exported for testing purposes.
func ParseMetricsLine(data []byte) (vmExportLine, error) {
	var line vmExportLine

	err := json.Unmarshal(data, &line)
	if err != nil {
		return vmExportLine{}, fmt.Errorf("parsing metrics line: %w", err)
	}

	return line, nil
}
