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

//go:build mage

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/magefile/mage/mg"
	"github.com/randomvariable/mage-common/config"
	magetools "github.com/randomvariable/mage-common/tools"
	//mage:import tools
	_ "github.com/randomvariable/mage-common/tools/targets"
	"github.com/spf13/pflag"
)

func init() {
	pflag.Parse()
	config.CleanOSArgs()
}

type Lint mg.Namespace

// Go runs golangci-lint on the codebase.
func (Lint) Go(ctx context.Context) error {
	_, err := magetools.Run(ctx, "golangci-lint", []string{"run", "./..."})

	return err
}

// Starlark runs buildifier lint and format checks on .star files.
// Suppresses the 'print' warning since Tilt uses print() for user logging.
func (Lint) Starlark(ctx context.Context) error {
	_, err := magetools.Run(ctx, "buildifier", []string{
		"--lint=warn", "--warnings=-print", "--mode=check", "--type=bzl",
		"-r", "extensions/",
	})

	return err
}

// All runs all linters.
func (Lint) All(ctx context.Context) error {
	if err := (Lint{}).Go(ctx); err != nil {
		return err
	}

	return (Lint{}).Starlark(ctx)
}

type Build mg.Namespace

// All compiles all Go packages.
func (Build) All(ctx context.Context) error {
	_, err := magetools.RunBinary(ctx, "go", []string{"build", "./..."})

	return err
}

// ExportCLI builds the observability-export CLI with CGO (required for DuckDB).
func (Build) ExportCLI(ctx context.Context) error {
	_, err := magetools.RunBinary(ctx, "go", []string{
		"build", "-o", "bin/observability-export", "./cmd/observability-export/",
	},
		magetools.WithEnv("CGO_ENABLED=1"),
	)

	return err
}

type Test mg.Namespace

// Unit runs Go tests with race detector.
func (Test) Unit(ctx context.Context) error {
	_, err := magetools.RunBinary(ctx, "go", []string{
		"test", "./...", "-count=1", "-timeout=120s", "-race",
	})

	return err
}

type E2e mg.Namespace

// ensureE2eTools installs tilt, helm, and kubectl, then returns a PATH env var
// with the tools directory prepended.
func ensureE2eTools(ctx context.Context) (string, error) {
	if err := magetools.Ensure(ctx, "tilt"); err != nil {
		return "", err
	}
	if err := magetools.Ensure(ctx, "helm"); err != nil {
		return "", err
	}
	if err := magetools.Ensure(ctx, "kubectl"); err != nil {
		return "", err
	}

	toolsDir, err := filepath.Abs(filepath.Join("hack", "bin", runtime.GOOS, runtime.GOARCH))
	if err != nil {
		return "", fmt.Errorf("resolving tools dir: %w", err)
	}

	return fmt.Sprintf("PATH=%s:%s", toolsDir, os.Getenv("PATH")), nil
}

// runE2eTests ensures tools are available and runs go test for the given packages.
func runE2eTests(ctx context.Context, pkgs ...string) error {
	pathEnv, err := ensureE2eTools(ctx)
	if err != nil {
		return err
	}

	args := []string{"test", "-v", "-count=1", "-timeout=120s"}
	args = append(args, pkgs...)
	_, err = magetools.RunBinary(ctx, "go", args, magetools.WithEnv(pathEnv))

	return err
}

// Observability runs observability extension tests (no cluster required).
func (E2e) Observability(ctx context.Context) error {
	return runE2eTests(ctx, "./tests/observability/")
}

// ObservabilityFull runs the full E2E observability test suite.
// Creates a kind cluster, deploys all components via tilt ci, runs integration
// tests, and cleans up. Requires CGO for DuckDB.
func (E2e) ObservabilityFull(ctx context.Context) error {
	clusterName := "tilt-dev"
	pathEnv, err := ensureE2eTools(ctx)
	if err != nil {
		return err
	}

	// Pre-load images into kind to avoid Docker Hub rate limiting
	fmt.Println("==> Pre-loading images into kind cluster...")

	// Save all images to a tar archive
	archivePath := filepath.Join(os.TempDir(), "observability-images.tar")
	images := []string{
		"victoriametrics/victoria-metrics:latest",
		"victoriametrics/victoria-logs:latest",
		"victoriametrics/victoria-traces:latest",
		"victoriametrics/vmagent:latest",
		"ghcr.io/parca-dev/parca:v0.25.0",
	}

	fmt.Println("  Saving images to tar archive...")
	saveArgs := []string{"save", "-o", archivePath}
	saveArgs = append(saveArgs, images...)
	_, err = magetools.RunBinary(ctx, "docker", saveArgs)
	if err != nil {
		fmt.Printf("  Warning: failed to save images: %v\n", err)
	} else {
		defer os.Remove(archivePath)

		fmt.Println("  Loading image archive into kind...")
		_, err = magetools.RunBinary(ctx, "kind", []string{"load", "image-archive", archivePath, "--name", clusterName})
		if err != nil {
			fmt.Printf("  Warning: failed to load image archive: %v\n", err)
		}
	}

	// Deploy observability stack via Tilt
	fmt.Println("==> Deploying observability stack with Tilt...")
	e2eDir, err := filepath.Abs(filepath.Join("tests", "observability", "e2e"))
	if err != nil {
		return fmt.Errorf("resolving e2e dir: %w", err)
	}

	// Change to e2e directory and back
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}
	if err := os.Chdir(e2eDir); err != nil {
		return fmt.Errorf("changing to e2e directory: %w", err)
	}
	defer os.Chdir(originalDir)

	_, err = magetools.RunBinary(ctx, "tilt", []string{"ci", "--timeout=5m"},
		magetools.WithEnv(pathEnv),
	)
	if err != nil {
		return fmt.Errorf("deploying with tilt: %w", err)
	}

	// Change back before running tests
	if err := os.Chdir(originalDir); err != nil {
		return fmt.Errorf("changing back to original directory: %w", err)
	}

	// Run E2E tests
	fmt.Println("==> Running E2E tests...")
	args := []string{
		"test", "-v", "-count=1", "-timeout=15m", "-tags=e2e",
		"./tests/observability/",
	}
	_, err = magetools.RunBinary(ctx, "go", args,
		magetools.WithEnv(pathEnv),
		magetools.WithEnv("CGO_ENABLED=1"),
	)
	if err != nil {
		return fmt.Errorf("running e2e tests: %w", err)
	}

	fmt.Println("==> E2E tests completed successfully!")

	return nil
}

// All runs all extension tests.
func (E2e) All(ctx context.Context) error {
	return runE2eTests(ctx, "./tests/observability/", "./tests/k3d-gpu/")
}
