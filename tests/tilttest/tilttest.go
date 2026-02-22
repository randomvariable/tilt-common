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

// Package tilttest provides test helpers for running Tilt extension tests.
// It wraps `tilt alpha tiltfile-result` to evaluate Tiltfiles without a cluster,
// extracts generated YAML from the results, and supports golden file comparison.
package tilttest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// tiltFailExitCode is the exit code tilt returns when a Tiltfile calls fail().
const tiltFailExitCode = 5

// TiltfileResult represents the JSON output of `tilt alpha tiltfile-result`.
type TiltfileResult struct {
	Manifests []Manifest `json:"Manifests"`
}

// Manifest represents a single manifest from the tiltfile result.
type Manifest struct {
	Name         string       `json:"Name"`
	DeployTarget DeployTarget `json:"DeployTarget"`
}

// DeployTarget contains the rendered YAML for a manifest.
type DeployTarget struct {
	YAML string `json:"yaml"`
}

// RepoRoot returns the absolute path to the repository root.
func RepoRoot(t *testing.T) string {
	t.Helper()
	// This file is at tests/tilttest/tilttest.go; walk up to find go.mod.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine source file location")
	}

	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find repo root (no go.mod found)")
		}

		dir = parent
	}
}

// ExtensionPath returns the absolute path to an extension's entry point.
func ExtensionPath(t *testing.T, name string) string {
	t.Helper()

	p := filepath.Join(RepoRoot(t), "extensions", name, "extension.star")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("extension not found: %s", p)
	}

	return p
}

// GoldenDir returns the absolute path to a test suite's golden file directory.
func GoldenDir(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("cannot determine caller file location")
	}

	return filepath.Join(filepath.Dir(filename), "golden")
}

// toolsDir returns the platform-specific managed tools directory.
func toolsDir(t *testing.T) string {
	t.Helper()

	return filepath.Join(RepoRoot(t), "hack", "bin", runtime.GOOS, runtime.GOARCH)
}

// tiltBinary returns the path to the tilt binary.
// Checks the managed tools directory first, then falls back to PATH.
func tiltBinary(t *testing.T) string {
	t.Helper()

	managed := filepath.Join(toolsDir(t), "tilt")
	if _, err := os.Stat(managed); err == nil {
		return managed
	}

	p, err := exec.LookPath("tilt")
	if err != nil {
		t.Skip("tilt not found; run 'go run mage.go tools:ensure'")
	}

	return p
}

// envWithTools returns the current environment with the managed tools
// directory prepended to PATH, so tilt can find helm and other tools.
func envWithTools(t *testing.T) []string {
	t.Helper()
	dir := toolsDir(t)

	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = fmt.Sprintf("PATH=%s:%s", dir, e[5:])

			return env
		}
	}

	return append(env, "PATH="+dir)
}

// kubectlContext returns the current kubectl context name.
// Falls back to "docker-desktop" if kubectl is not available.
func kubectlContext(t *testing.T) string {
	t.Helper()
	managed := filepath.Join(toolsDir(t), "kubectl")

	kubectl := managed
	if _, err := os.Stat(managed); err != nil {
		var lookupErr error

		kubectl, lookupErr = exec.LookPath("kubectl")
		if lookupErr != nil {
			return "docker-desktop"
		}
	}

	out, err := exec.Command(kubectl, "config", "current-context").Output()
	if err != nil {
		return "docker-desktop"
	}

	return strings.TrimSpace(string(out))
}

// Tiltfile builds a Tiltfile content string that loads an extension via
// load_dynamic and executes the given Starlark code. The code can reference
// the `ext` variable which holds the loaded extension module.
func Tiltfile(t *testing.T, extensionPath, starlark string) string {
	t.Helper()
	ctx := kubectlContext(t)

	return fmt.Sprintf("allow_k8s_contexts('%s')\next = load_dynamic('%s')\n%s\n", ctx, extensionPath, starlark)
}

// RunTiltfile runs `tilt alpha tiltfile-result` with the given Tiltfile content
// and returns the parsed JSON result and the process exit code.
// The result is nil when tilt exits with a non-zero code.
func RunTiltfile(t *testing.T, content string) (result *TiltfileResult, code int) {
	t.Helper()

	return RunTiltfileWithEnv(t, content, nil)
}

// RunTiltfileWithEnv runs `tilt alpha tiltfile-result` with the given Tiltfile content
// and extra environment variables appended to the process env.
// Returns the parsed JSON result and the process exit code.
// The result is nil when tilt exits with a non-zero code.
func RunTiltfileWithEnv(t *testing.T, content string, extraEnv []string) (result *TiltfileResult, code int) {
	t.Helper()

	tiltfile := filepath.Join(t.TempDir(), "Tiltfile")
	if err := os.WriteFile(tiltfile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing Tiltfile: %v", err)
	}

	tilt := tiltBinary(t)
	cmd := exec.Command(tilt, "alpha", "tiltfile-result", "-f", tiltfile)

	cmd.Env = append(envWithTools(t), extraEnv...)

	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0

	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("running tilt: %v", err)
		}
	}

	if exitCode != 0 {
		return nil, exitCode
	}

	var parsed TiltfileResult
	if err := json.Unmarshal([]byte(stdout.String()), &parsed); err != nil {
		t.Fatalf("parsing tiltfile-result JSON: %v\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}

	return &parsed, 0
}

// ExpectPass runs a Tiltfile and asserts it succeeds (exit code 0).
func ExpectPass(t *testing.T, content string) *TiltfileResult {
	t.Helper()

	result, exitCode := RunTiltfile(t, content)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	return result
}

// ExpectPassWithEnv runs a Tiltfile with extra environment variables and asserts it succeeds.
func ExpectPassWithEnv(t *testing.T, content string, extraEnv []string) *TiltfileResult {
	t.Helper()

	result, exitCode := RunTiltfileWithEnv(t, content, extraEnv)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	return result
}

// ExpectFail runs a Tiltfile and asserts it triggers fail() (exit code 5).
func ExpectFail(t *testing.T, content string) {
	t.Helper()

	_, exitCode := RunTiltfile(t, content)
	if exitCode != tiltFailExitCode {
		t.Fatalf("expected exit code %d (fail()), got %d", tiltFailExitCode, exitCode)
	}
}

// ExtractYAML extracts and sorts YAML documents from a TiltfileResult.
// This matches the jq extraction used by the shell tests:
//
//	[.Manifests[] | select(.DeployTarget.yaml != null) | .DeployTarget.yaml] | sort | join("\n---\n")
func ExtractYAML(result *TiltfileResult) string {
	var yamls []string

	for _, m := range result.Manifests {
		if m.DeployTarget.YAML != "" {
			yamls = append(yamls, m.DeployTarget.YAML)
		}
	}

	sort.Strings(yamls)

	return strings.Join(yamls, "\n---\n")
}

// CompareGolden compares actual output against a golden file.
// If UPDATE_GOLDEN=true is set, the golden file is created/updated instead.
func CompareGolden(t *testing.T, goldenFile, actual string) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "true" {
		dir := filepath.Dir(goldenFile)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating golden dir: %v", err)
		}

		if err := os.WriteFile(goldenFile, []byte(actual), 0o644); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}

		t.Logf("updated golden file: %s", goldenFile)

		return
	}

	expected, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("reading golden file %s: %v\nRun with UPDATE_GOLDEN=true to create it", goldenFile, err)
	}

	if string(expected) != actual {
		t.Errorf("output differs from golden file %s", goldenFile)
		t.Error("Run with UPDATE_GOLDEN=true to update")

		expectedLines := strings.Split(string(expected), "\n")
		actualLines := strings.Split(actual, "\n")
		// Show a basic line-count diff for debugging.
		t.Logf("expected %d lines, got %d lines", len(expectedLines), len(actualLines))
		// Show first diverging line.
		for i := 0; i < len(expectedLines) && i < len(actualLines); i++ {
			if expectedLines[i] != actualLines[i] {
				t.Logf("first diff at line %d:\n  expected: %s\n  actual:   %s", i+1, expectedLines[i], actualLines[i])

				break
			}
		}
	}
}
