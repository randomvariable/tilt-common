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

package observability_test

import (
	"path/filepath"
	"testing"

	"github.com/randomvariable/tilt-common/tests/tilttest"
)

func extensionPath(t *testing.T) string {
	t.Helper()

	return tilttest.ExtensionPath(t, "observability")
}

// TestSmoke verifies the extension loads without errors for valid configurations.
func TestSmoke(t *testing.T) {
	t.Parallel()

	ext := extensionPath(t)

	tests := []struct {
		name     string
		starlark string
	}{
		{
			name:     "default_config",
			starlark: `ext["enable_observability"]()`,
		},
		{
			name:     "empty_config",
			starlark: `ext["enable_observability"]({})`,
		},
		{
			name: "all_disabled",
			starlark: `ext["enable_observability"]({
    'metrics': False,
    'logs': False,
    'traces': False,
    'prometheus_metrics': False,
    'otel': False,
    'profiling': False,
})`,
		},
		{
			name: "custom_ports",
			starlark: `ext["enable_observability"]({
    'ports': {
        'grafana': 3001,
        'metrics': 9090,
    },
})`,
		},
		{
			name: "otel_enabled",
			starlark: `ext["enable_observability"]({
    'otel': True,
})`,
		},
		{
			name: "prometheus_metrics_enabled",
			starlark: `ext["enable_observability"]({
    'prometheus_metrics': True,
})`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := tilttest.Tiltfile(t, ext, tt.starlark)
			tilttest.ExpectPass(t, content)
		})
	}
}

// TestConfigValidationFail verifies invalid configs trigger fail().
func TestConfigValidationFail(t *testing.T) {
	t.Parallel()

	ext := extensionPath(t)

	tests := []struct {
		name     string
		starlark string
	}{
		// Invalid port tests
		{
			name:     "port_zero",
			starlark: `ext["enable_observability"]({'ports': {'metrics': 0}})`,
		},
		{
			name:     "port_negative",
			starlark: `ext["enable_observability"]({'ports': {'metrics': -1}})`,
		},
		{
			name:     "port_too_high",
			starlark: `ext["enable_observability"]({'ports': {'metrics': 65536}})`,
		},
		// Invalid namespace tests
		{
			name:     "namespace_uppercase",
			starlark: `ext["enable_observability"]({'namespace': 'MyNamespace'})`,
		},
		{
			name:     "namespace_with_dots",
			starlark: `ext["enable_observability"]({'namespace': 'my.namespace'})`,
		},
		{
			name:     "namespace_empty",
			starlark: `ext["enable_observability"]({'namespace': ''})`,
		},
		// Unrecognized key tests
		{
			name:     "unknown_top_key",
			starlark: `ext["enable_observability"]({'unknown_key': 'value'})`,
		},
		{
			name:     "unknown_component_key",
			starlark: `ext["enable_observability"]({'components': {'unknown': True}})`,
		},
		{
			name:     "unknown_port_key",
			starlark: `ext["enable_observability"]({'ports': {'unknown': 8080}})`,
		},
		// Dependency constraint tests
		{
			name: "vmagent_without_metrics",
			starlark: `ext["enable_observability"]({
    'components': {'metrics': False, 'vmagent': True},
})`,
		},
		// OTel Operator dependency constraints
		{
			name: "otel_operator_no_backends",
			starlark: `ext["enable_observability"]({
    'components': {
        'metrics': False,
        'logs': False,
        'traces': False,
        'grafana': False,
        'otel_operator': True,
    },
})`,
		},
		// Vector dependency constraints
		{
			name: "vector_without_logs",
			starlark: `ext["enable_observability"]({
    'components': {'logs': False, 'vector': True},
})`,
		},
		// VM Operator dependency constraints
		{
			name: "vm_operator_without_metrics",
			starlark: `ext["enable_observability"]({
    'components': {'metrics': False, 'vmagent': True, 'vm_operator': True},
})`,
		},
		{
			name: "vm_operator_without_vmagent",
			starlark: `ext["enable_observability"]({
    'components': {'vm_operator': True},
})`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := tilttest.Tiltfile(t, ext, tt.starlark)
			tilttest.ExpectFail(t, content)
		})
	}
}

// TestConfigValidationPass verifies valid configs succeed.
func TestConfigValidationPass(t *testing.T) {
	t.Parallel()

	ext := extensionPath(t)

	tests := []struct {
		name     string
		starlark string
	}{
		{
			name: "all_disabled_noop",
			starlark: `ext["enable_observability"]({
    'components': {
        'metrics': False,
        'logs': False,
        'traces': False,
        'grafana': False,
    },
})`,
		},
		{
			name: "vmagent_with_metrics",
			starlark: `ext["enable_observability"]({
    'components': {'vmagent': True},
})`,
		},
		{
			name: "custom_ports_valid",
			starlark: `ext["enable_observability"]({
    'ports': {'grafana': 3001, 'metrics': 9090, 'logs': 9428, 'traces': 10428, 'parca': 7071},
})`,
		},
		{
			name: "otel_operator_with_defaults",
			starlark: `ext["enable_observability"]({
    'components': {'otel_operator': True},
})`,
		},
		{
			name: "otel_operator_metrics_only",
			starlark: `ext["enable_observability"]({
    'components': {
        'otel_operator': True,
        'logs': False,
        'traces': False,
        'grafana': False,
    },
})`,
		},
		{
			name: "vmagent_with_scrape_targets",
			starlark: `ext["enable_observability"]({
    'components': {'vmagent': True},
    'scrape_targets': ['my-service:8080', 'another-service:9090'],
})`,
		},
		{
			name: "profiling_with_full_stack",
			starlark: `ext["enable_observability"]({
    'components': {'profiling': True},
})`,
		},
		{
			name: "no_dashboards_no_error",
			starlark: `ext["enable_observability"]({
    'dashboard_paths': [],
})`,
		},
		{
			name: "vector_with_logs",
			starlark: `ext["enable_observability"]({
    'components': {'vector': True},
})`,
		},
		{
			name: "vm_operator_with_vmagent",
			starlark: `ext["enable_observability"]({
    'components': {'vmagent': True, 'vm_operator': True},
})`,
		},
		{
			name: "registry_mirror_with_components",
			starlark: `ext["enable_observability"]({
    'registry_mirror': 'harbor.example.com/docker',
    'components': {'vmagent': True},
})`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := tilttest.Tiltfile(t, ext, tt.starlark)
			tilttest.ExpectPass(t, content)
		})
	}
}

// TestGoldenFiles verifies generated YAML matches expected output.
func TestGoldenFiles(t *testing.T) {
	t.Parallel()

	ext := extensionPath(t)
	goldenDir := filepath.Join(tilttest.RepoRoot(t), "tests", "observability", "golden")

	tests := []struct {
		name     string
		starlark string
		env      []string
	}{
		{
			name:     "default-stack",
			starlark: `ext["enable_observability"]()`,
		},
		{
			name: "with-vmagent",
			starlark: `ext["enable_observability"]({
    'components': {'vmagent': True},
})`,
		},
		{
			name: "selective-components",
			starlark: `ext["enable_observability"]({
    'components': {
        'metrics': False,
        'logs': False,
        'traces': False,
        'grafana': False,
        'profiling': True,
    },
})`,
			env: []string{"TILT_OBSERVABILITY_CONTAINERD_SOCKET=/run/containerd/containerd.sock"},
		},
		{
			name: "with-vector",
			starlark: `ext["enable_observability"]({
    'components': {'vector': True},
})`,
		},
		{
			name: "with-registry-mirror",
			starlark: `ext["enable_observability"]({
    'registry_mirror': 'harbor.example.com/docker',
})`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := tilttest.Tiltfile(t, ext, tt.starlark)

			var result *tilttest.TiltfileResult
			if len(tt.env) > 0 {
				result = tilttest.ExpectPassWithEnv(t, content, tt.env)
			} else {
				result = tilttest.ExpectPass(t, content)
			}

			actual := tilttest.ExtractYAML(result)
			goldenFile := filepath.Join(goldenDir, tt.name+".yaml")
			tilttest.CompareGolden(t, goldenFile, actual)
		})
	}
}
