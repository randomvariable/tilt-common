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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/marcboeker/go-duckdb"
)

// errMissingTime is returned when a log entry lacks the required _time field.
var errMissingTime = errors.New("log entry missing _time field")

// severityKeys lists the well-known JSON keys that may contain a log severity
// level, checked in priority order.
var severityKeys = []string{"severityText", "level", "severity"}

// ExportLogs fetches all logs from VictoriaLogs via the /select/logsql/query
// endpoint and writes them to the logs table in the DuckDB database. The
// endpoint returns JSON lines where each line is a log entry with arbitrary
// fields plus the system fields _msg, _time, and _stream.
func ExportLogs(ctx context.Context, database *sql.DB, logsURL string, client *http.Client) error {
	exportURL, err := buildLogsExportURL(logsURL)
	if err != nil {
		return fmt.Errorf("building logs export URL: %w", err)
	}

	resp, err := httpGetStream(ctx, client, exportURL)
	if err != nil {
		return err
	}

	defer closeResource("logs response body", resp.Body)

	conn, appender, err := newTableAppender(ctx, database, "logs")
	if err != nil {
		return err
	}

	defer closeResource("logs connection", conn)
	defer closeResource("logs appender", appender)

	exportTime := time.Now().UTC()

	var totalRows int

	scanner := bufio.NewScanner(resp.Body)
	// Increase scanner buffer for large lines (16 MiB).
	const maxScannerBuf = 16 * 1024 * 1024
	scanner.Buffer(make([]byte, 0, maxScannerBuf), maxScannerBuf)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		entry, err := ParseLogEntry(line)
		if err != nil {
			slog.Warn("skipping malformed log line", "error", err)

			continue
		}

		if err := appendLogRow(appender, &entry, exportTime); err != nil {
			return err
		}

		totalRows++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading logs response: %w", err)
	}

	if err := appender.Flush(); err != nil {
		return fmt.Errorf("flushing logs appender: %w", err)
	}

	slog.Info("logs rows written", "count", totalRows)

	return nil
}

// logEntry is the parsed representation of a VictoriaLogs JSON line entry.
type logEntry struct {
	Timestamp time.Time
	Message   string
	Stream    string
	Severity  string
	Fields    duckdb.Map
}

// ParseLogEntry parses a single JSON line from VictoriaLogs into a logEntry.
// System fields (_msg, _time, _stream) are extracted into dedicated fields.
// Well-known severity keys (severityText, level, severity) are extracted.
// All remaining keys are collected into the Fields map.
// This is exported for testing purposes.
func ParseLogEntry(data []byte) (logEntry, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return logEntry{}, fmt.Errorf("parsing log entry: %w", err)
	}

	timestamp, err := parseLogTimestamp(raw)
	if err != nil {
		return logEntry{}, err
	}

	entry := logEntry{
		Timestamp: timestamp,
		Fields:    make(duckdb.Map),
	}

	// Extract system fields.
	if msg, ok := raw["_msg"].(string); ok {
		entry.Message = msg
	}

	if stream, ok := raw["_stream"].(string); ok {
		entry.Stream = stream
	}

	entry.Severity = extractSeverity(raw)

	// Collect remaining fields (excluding system fields).
	for k, v := range raw {
		if k == "_msg" || k == "_time" || k == "_stream" {
			continue
		}

		entry.Fields[k] = fmt.Sprintf("%v", v)
	}

	return entry, nil
}

// parseLogTimestamp extracts and parses the _time field from a raw log entry.
func parseLogTimestamp(raw map[string]any) (time.Time, error) {
	timeStr, ok := raw["_time"].(string)
	if !ok || timeStr == "" {
		return time.Time{}, errMissingTime
	}

	ts, err := time.Parse(time.RFC3339Nano, timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing log timestamp %q: %w", timeStr, err)
	}

	return ts.UTC(), nil
}

// extractSeverity finds and removes a severity value from well-known keys.
func extractSeverity(raw map[string]any) string {
	for _, key := range severityKeys {
		if v, ok := raw[key].(string); ok && v != "" {
			delete(raw, key)

			return v
		}
	}

	return ""
}

// appendLogRow writes a single log entry to the DuckDB appender.
func appendLogRow(appender *duckdb.Appender, entry *logEntry, exportTime time.Time) error {
	if err := appender.AppendRow(
		entry.Timestamp,
		entry.Message,
		nilIfEmpty(entry.Stream),
		nilIfEmpty(entry.Severity),
		entry.Fields,
		exportTime,
	); err != nil {
		return fmt.Errorf("appending log row: %w", err)
	}

	return nil
}

// buildLogsExportURL constructs the VictoriaLogs query API URL to fetch all
// logs from the beginning of time.
func buildLogsExportURL(baseURL string) (string, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parsing base URL %q: %w", baseURL, err)
	}

	parsedURL.Path = logsQueryPath

	query := parsedURL.Query()
	query.Set("query", "*")
	query.Set("start", "0")
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}
