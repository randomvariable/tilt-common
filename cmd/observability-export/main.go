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

// Package main implements the observability-export CLI tool that exports
// telemetry data from VictoriaMetrics and VictoriaTraces into a DuckDB
// database, and downloads pprof profiles from Parca.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

const (
	// ExitSuccess indicates all exports completed successfully.
	ExitSuccess = 0
	// ExitPartial indicates some backends were unreachable but at least one export succeeded.
	ExitPartial = 1
	// ExitFailure indicates a complete failure where no exports succeeded.
	ExitFailure = 2
)

func main() {
	os.Exit(run())
}

func run() int {
	opts := ExportOptions{}

	var kubeconfigPath, namespace string
	var noPortForward bool

	flag.StringVar(&opts.OutputDir, "output-dir", "./artifacts", "Directory to write exported data")
	flag.StringVar(&opts.MetricsURL, "metrics-url", "http://localhost:8428", "VictoriaMetrics base URL (ignored when port-forwarding)")
	flag.StringVar(&opts.TracesURL, "traces-url", "http://localhost:10428", "VictoriaTraces base URL (ignored when port-forwarding)")
	flag.StringVar(&opts.LogsURL, "logs-url", "http://localhost:9428", "VictoriaLogs base URL (ignored when port-forwarding)")
	flag.StringVar(&opts.ParcaURL, "parca-url", "http://localhost:7070", "Parca server base URL (ignored when port-forwarding)")
	flag.BoolVar(&opts.SkipMetrics, "skip-metrics", false, "Skip metrics export")
	flag.BoolVar(&opts.SkipTraces, "skip-traces", false, "Skip traces export")
	flag.BoolVar(&opts.SkipLogs, "skip-logs", false, "Skip logs export")
	flag.BoolVar(&opts.SkipProfiles, "skip-profiles", false, "Skip profile export")
	flag.StringVar(&kubeconfigPath, "kubeconfig", "", "Path to kubeconfig (defaults to KUBECONFIG env, then ~/.kube/config)")
	flag.StringVar(&namespace, "namespace", "observability", "Kubernetes namespace containing the observability stack")
	flag.BoolVar(&noPortForward, "no-portforward", false, "Disable automatic Kubernetes port-forwarding (use --*-url flags instead)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if !noPortForward {
		cleanup, err := SetupPortForward(ctx, kubeconfigPath, namespace, &opts)
		if err != nil {
			slog.Error("setting up port-forward", "error", err)

			return ExitFailure
		}

		defer cleanup()
	}

	result := RunExport(ctx, &opts)

	for _, err := range result.Errors {
		slog.Error("export error", "error", err)
	}

	switch result.Status {
	case StatusSuccess:
		slog.Info("export completed successfully", "output", result.OutputPath)

		return ExitSuccess
	case StatusPartial:
		slog.Warn("export completed with partial results", "output", result.OutputPath)

		return ExitPartial
	case StatusFailure:
		slog.Error("export failed completely")

		return ExitFailure
	default:
		if _, err := fmt.Fprintf(os.Stderr, "unexpected export status: %d\n", result.Status); err != nil {
			slog.Error("writing to stderr", "error", err)
		}

		return ExitFailure
	}
}
