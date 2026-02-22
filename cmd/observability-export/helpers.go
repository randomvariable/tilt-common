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
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/marcboeker/go-duckdb"
)

// Sentinel errors for HTTP and database operations.
var (
	// ErrUnexpectedHTTPStatus indicates a non-200 HTTP response.
	ErrUnexpectedHTTPStatus = errors.New("unexpected HTTP status")
	// ErrDriverTypeAssertion indicates the raw driver connection is not the expected type.
	ErrDriverTypeAssertion = errors.New("unexpected driver connection type")
)

const (
	// dirPermissions is the file mode used when creating output directories.
	dirPermissions = 0o750
	// defaultHTTPTimeout bounds all outbound HTTP requests when no client is injected.
	defaultHTTPTimeout = 10 * time.Second

	// API path constants.
	metricsExportPath = "/api/v1/export"
	servicesAPIPath   = "/select/jaeger/api/services"
	tracesAPIPath     = "/select/jaeger/api/traces"
	logsQueryPath     = "/select/logsql/query"
)

var defaultHTTPClient = &http.Client{Timeout: defaultHTTPTimeout}

// clientOrDefault returns client if non-nil, otherwise a shared client with a bounded timeout.
func clientOrDefault(c *http.Client) *http.Client {
	if c != nil {
		return c
	}

	return defaultHTTPClient
}

// fetchJSON performs a GET request to baseURL+path and decodes the JSON response
// into the type parameter T. If client is nil, the shared default client is used.
func fetchJSON[T any](ctx context.Context, client *http.Client, baseURL, path string) (T, error) { //nolint:ireturn // generic type parameter
	var result T

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return result, fmt.Errorf("parsing URL %q: %w", baseURL, err)
	}

	parsedURL.Path = path

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), http.NoBody)
	if err != nil {
		return result, fmt.Errorf("creating request for %s: %w", path, err)
	}

	resp, err := clientOrDefault(client).Do(request)
	if err != nil {
		return result, fmt.Errorf("fetching %s: %w", path, err)
	}

	defer closeResource("response body", resp.Body)

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("%w: %d from %s", ErrUnexpectedHTTPStatus, resp.StatusCode, path)
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, fmt.Errorf("decoding response from %s: %w", path, err)
	}

	return result, nil
}

// httpGetStream performs a GET request and returns the response for streaming
// reads. The caller must close the response body. If client is nil,
// the shared default client is used.
func httpGetStream(ctx context.Context, client *http.Client, requestURL string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	resp, err := clientOrDefault(client).Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", requestURL, err)
	}

	if resp.StatusCode != http.StatusOK {
		closeResource("response body", resp.Body)

		return nil, fmt.Errorf("%w: %d from %s", ErrUnexpectedHTTPStatus, resp.StatusCode, requestURL)
	}

	return resp, nil
}

// newTableAppender creates a DuckDB Appender for the named table. The caller
// must close both the returned connection and appender.
func newTableAppender(ctx context.Context, database *sql.DB, tableName string) (*sql.Conn, *duckdb.Appender, error) {
	conn, err := database.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("obtaining database connection: %w", err)
	}

	var appender *duckdb.Appender

	rawErr := conn.Raw(func(rawConn any) error {
		typedConn, ok := rawConn.(driver.Conn)
		if !ok {
			return fmt.Errorf("%w: %T", ErrDriverTypeAssertion, rawConn)
		}

		var appendErr error

		appender, appendErr = duckdb.NewAppenderFromConn(typedConn, "", tableName)
		if appendErr != nil {
			return fmt.Errorf("creating %s appender: %w", tableName, appendErr)
		}

		return nil
	})
	if rawErr != nil {
		closeResource("database connection", conn)

		return nil, nil, fmt.Errorf("initializing appender for %s: %w", tableName, rawErr)
	}

	return conn, appender, nil
}

// closeResource logs a warning if closing the resource fails.
func closeResource(name string, closer io.Closer) {
	if err := closer.Close(); err != nil {
		slog.Warn("closing resource", "resource", name, "error", err)
	}
}
