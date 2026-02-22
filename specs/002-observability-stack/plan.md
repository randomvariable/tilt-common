# Implementation Plan: Observability Stack Extension

**Branch**: `002-observability-stack` | **Date**: 2026-02-15 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/002-observability-stack/spec.md`

## Summary

Build a composable Tilt extension that deploys a full observability stack (VictoriaMetrics, VictoriaLogs + Vector, VictoriaTraces, Grafana via Operator, VMAgent, OTel Operator, Parca) with simplified boolean flags. Include a Go CLI tool for exporting telemetry to DuckDB + pprof. Migrate cluster lifecycle from k3d to the kindgpu Go library.

## Technical Context

**Language/Version**: Starlark (Tilt extension) + Go 1.23+ (export CLI, requires CGO for DuckDB) + Python 3.11+ (Jupyter notebook examples only) + Bash 4.0+ (test scripts, containerd detection)
**Primary Dependencies**: VictoriaMetrics ecosystem (metrics/logs/traces), Grafana Operator (`grafana.integreatly.org`), Vector, VMAgent, OpenTelemetry Operator (Helm chart v0.105.1), Parca 0.25.x, DuckDB (`github.com/duckdb/duckdb-go` v2.5.5+), Tilt (extension runtime), Helm v3.x (for OTel + Grafana Operator chart rendering)
**Storage**: DuckDB database file (export only), ephemeral in-cluster storage for all backends with built-in retention flags
**Testing**: Go table-driven tests with gomega; `tilt alpha tiltfile-result` (Tier 1/2), `tilt ci` (Tier 3 E2E); golden files for YAML validation
**Target Platform**: Local Kubernetes (kind via kindgpu Go library), Kubernetes 1.34+
**Project Type**: Tilt extension library + Go CLI tool
**Performance Goals**: All core components healthy within 90 seconds (warm start, FR/NFR-001/SC-002)
**Constraints**: Total core stack footprint <= 1 CPU, 1.5 GiB memory (NFR-002); CGO required for DuckDB; Helm CLI required for OTel/Grafana Operator
**Scale/Scope**: Single-developer local clusters; ephemeral data with configurable retention

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Extension-First Architecture | PASS | Extension is standalone Starlark module, loadable via `load('ext://tilt-common/observability', 'enable_observability')`, config via function params, no hard deps on other extensions |
| II. Configuration Over Convention | PASS | Config dict with documented defaults (FR-001), env var overrides (NFR-003), validation with `fail()` (FR-032), incremental adoption (zero-config deploys full stack) |
| III. Local-First Development Experience | PASS | Minimal replicas, resource limits for laptops (NFR-002), port forwarding (FR-026/027), structured labels (FR-028), pre-wired OTel endpoints (FR-011) |
| IV. Documentation (NON-NEGOTIABLE) | PASS | README.md required (exists in extensions/observability/), example Tiltfile in examples/observability/, inline Starlark docstrings, Jupyter notebooks (FR-040) |
| V. Test-Driven Development | PASS | All tests in Go (FR-042), table-driven with gomega, golden file validation (Decision 10), E2E via `tilt ci`, 80% coverage target |
| Code Quality (NON-NEGOTIABLE) | PASS | No linter exclusions, `golangci-lint run --fix ./...` before commit |

**Post-Design Re-Check**: All gates still pass. No complexity violations introduced.

## Project Structure

### Documentation (this feature)

```text
specs/002-observability-stack/
├── plan.md              # This file
├── research.md          # Phase 0 output (23 decisions)
├── data-model.md        # Phase 1 output (8 entities)
├── quickstart.md        # Phase 1 output
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
extensions/observability/
├── extension.star           # Main Starlark extension (public API)
├── README.md                # Extension documentation
└── assets/                  # Kubernetes manifest templates
    ├── metrics.yaml         # VictoriaMetrics Deployment + Service
    ├── logs.yaml            # VictoriaLogs Deployment + Service
    ├── vector.yaml          # Vector DaemonSet
    ├── vector-config.yaml   # Vector pipeline ConfigMap
    ├── traces.yaml          # VictoriaTraces Deployment + Service
    ├── grafana.yaml         # Grafana CR + GrafanaDatasource CRs
    ├── vmagent.yaml         # VMAgent Deployment + Service + RBAC
    ├── vmagent-config.yaml  # VMAgent scrape ConfigMap
    ├── parca.yaml           # Parca Server + Agent DaemonSet + RBAC
    └── grafana-dashboards.yaml  # (placeholder for bundled dashboards)

extensions/kind-gpu/         # Pure Go library (already implemented)
├── api/v1alpha1/types.go    # Config types
├── kindgpu.go               # Cluster lifecycle
├── kindgpu_test.go          # 12 unit tests
├── image.go                 # Node image building
├── image_test.go            # Image tests
├── mirrors.go               # Registry mirror config
├── mirrors_test.go          # Mirror tests
└── assets/Dockerfile.tmpl   # Node image template

cmd/observability-export/    # Go CLI tool (DuckDB export)
├── main.go                  # Entry point
├── export.go                # Export orchestration
├── metrics.go               # VictoriaMetrics export
├── traces.go                # VictoriaTraces export
├── profiles.go              # Parca pprof export
└── duckdb.go                # DuckDB schema + writers

tests/observability/         # Go test suites
├── extension_test.go        # Tier 1/2: smoke + golden file tests
├── e2e_test.go              # Tier 3: full E2E with cluster
└── golden/                  # Golden file reference outputs

tests/tilttest/              # Shared test helpers
└── helpers.go               # tilt alpha tiltfile-result wrapper

examples/observability/      # Usage examples
├── Tiltfile                 # Example Tiltfile
└── notebooks/               # Jupyter notebook examples
    └── analyze-telemetry.ipynb
```

**Structure Decision**: Extension-first with two workstreams: (1) Starlark extension + Go export CLI in extensions/observability/ and cmd/observability-export/, (2) kindgpu Go library already implemented in extensions/kind-gpu/. Tests follow the constitution's Go-only testing standard with golden files for YAML validation.

## Implementation Phases

### Phase 1: Core Extension Skeleton + Config Validation

**Goal**: `enable_observability()` loads without errors, validates config, deploys nothing when all flags disabled.

1. Rewrite `extensions/observability/extension.star`:
   - `enable_observability()` function signature with all config fields from ExtensionConfig (data-model.md)
   - Config validation: port range, namespace RFC 1123, unknown key rejection (FR-032)
   - `prometheus_metrics` requires `metrics` guard (FR-020)
   - Default values: metrics/logs/traces=True, prometheus_metrics/otel/profiling=False (FR-001)
   - No-op when all flags disabled (FR-003)
2. Write `tests/observability/extension_test.go` (Tier 1):
   - Table-driven tests for config validation (valid defaults, invalid ports, unknown keys, prometheus_metrics without metrics)
   - Smoke test: extension loads without error via `tilt alpha tiltfile-result`
3. Write `tests/tilttest/helpers.go`:
   - Shared helper to invoke `tilt alpha tiltfile-result` and parse JSON output

### Phase 2: Core Signal Backends (VictoriaMetrics, VictoriaLogs + Vector, VictoriaTraces)

**Goal**: Deploying with default flags produces all three backends + Vector with health checks.

1. Update asset YAML files to match data-model.md:
   - `metrics.yaml`: VictoriaMetrics Deployment + Service (port 8428, retention flag, health probes)
   - `logs.yaml`: VictoriaLogs Deployment + Service (port 9428, retention flag, health probes)
   - `vector.yaml` + `vector-config.yaml`: Vector DaemonSet + ConfigMap (kubernetes_logs source, VictoriaLogs sink)
   - `traces.yaml`: VictoriaTraces Deployment + Service (ports 10428 + 4317, retention flag, health probes)
   - All: namespace parameterization, managed-by labels, image overrides, resource requests/limits (NFR-002)
   - All: RBAC resources use `tilt-observability-` prefix (FR-033a)
2. Wire `extension.star` to conditionally apply assets based on flags
3. Add golden file tests (Tier 2): generate YAML from test Tiltfiles, compare against golden files
4. Print OTel environment variables to Tilt log for enabled backends (FR-011, FR-012)

### Phase 3: Grafana Operator + Datasources

**Goal**: Grafana Operator auto-deploys when any flag enabled, with datasources for active backends.

1. Add Grafana Operator Helm rendering in `extension.star`:
   - `helm_resource()` or `local()` + `helm template` for Grafana Operator chart
   - Grafana CR manifest: admin/admin credentials, port 3000
   - GrafanaDatasource CRs: conditional on metrics/logs/traces flags (data-model.md entity 3)
2. Update `grafana.yaml` to Grafana CR format (`grafana.integreatly.org/v1beta1`)
3. Add Tilt resource dependencies: backends → grafana-operator → grafana
4. Golden file tests for Grafana manifests with various flag combinations

### Phase 4: VMAgent + Prometheus Scraping (Annotations + CRDs)

**Goal**: `prometheus_metrics=True` deploys VMAgent with annotation-based and CRD-based scraping.

1. Update `vmagent.yaml` + `vmagent-config.yaml`:
   - VMAgent Deployment + Service (port 8429)
   - RBAC: ServiceAccount + ClusterRole (get/list/watch nodes/pods/services/endpoints) + ClusterRoleBinding with `tilt-observability-` prefix
   - Remote write to VictoriaMetrics
   - `kubernetes_sd_configs` with relabeling for `prometheus.io/scrape` annotations (FR-021)
   - CRD discovery flags for ServiceMonitor/PodMonitor/ScrapeConfig/Probe (FR-022)
2. Install Prometheus Operator CRDs via server-side apply with force-conflicts
3. Wire `extension.star`: VMAgent depends on VictoriaMetrics
4. Golden file tests + config validation tests

### Phase 5: OTel Operator + Instrumentation CR

**Goal**: `otel=True` deploys OTel Operator webhook with auto-injection of OTLP env vars.

1. Add OTel Operator Helm rendering:
   - `helm template` with values from Decision 13 (self-signed certs, failurePolicy: Ignore)
   - Instrumentation CR with OTLP env vars for enabled backends (data-model.md entity 6)
   - CRD installation via server-side apply with force-conflicts
2. Wire `extension.star`: OTel Operator as independent resource, Instrumentation CR depends on operator
3. Golden file tests for Helm-rendered manifests
4. Test: otel enabled with various backend combinations

### Phase 6: Parca Profiling

**Goal**: `profiling=True` deploys Parca standalone.

1. Update `parca.yaml`:
   - Parca Server Deployment + Service (port 7070)
   - Parca Agent DaemonSet with privileged security context, host path mounts
   - RBAC with `tilt-observability-` prefix
   - Containerd socket auto-detection (Decision 8)
2. Wire `extension.star`: Parca Agent depends on Parca Server, standalone from other backends (FR-024)
3. Golden file tests for profiling manifests

### Phase 7: Export CLI Tool

**Goal**: Go CLI exports metrics, traces, and pprof profiles to DuckDB + files.

1. Implement `cmd/observability-export/`:
   - `main.go`: CLI entry point (flags: export-dir, metrics-url, traces-url, parca-url)
   - `duckdb.go`: Schema creation (Decision 2), Appender-based bulk writes
   - `metrics.go`: VictoriaMetrics `/api/v1/export` JSON lines parser (Decision 4)
   - `traces.go`: VictoriaTraces Jaeger API client (Decision 5)
   - `profiles.go`: Parca HTTP API pprof download (Decision 6)
   - `export.go`: Orchestration -- parallel export, partial success handling
2. Wire as Tilt button/local_resource in `extension.star` (FR-037)
3. Unit tests for each exporter (mock HTTP responses)
4. Add to Magefile: `build:exportCLI` target

### Phase 8: kindgpu Migration + E2E Tests

**Goal**: E2E tests use kindgpu Go library for cluster lifecycle.

1. Update `tests/observability/e2e_test.go`:
   - Replace k3d cluster create/delete with `kindgpu.CreateClusterFromConfig`/`kindgpu.DeleteCluster`
   - Update kubeconfig context from `k3d-tilt-common-e2e` to `kind-tilt-common-e2e` (Decision 22)
   - Remove `default_registry('localhost:5005')` from E2E Tiltfile (Decision 19)
2. Delete stale k3d test artifacts:
   - `tests/k3d-gpu/test-config-validation.sh`, `test-load.sh`, `test-output-validation.sh`
   - `extensions/k3d-gpu/test.sh`
3. Full E2E test: create cluster → `tilt ci` → send test telemetry → query backends → export → verify DuckDB + pprof

### Phase 9: Documentation + Examples

**Goal**: Complete documentation per constitution requirements.

1. Update `extensions/observability/README.md`:
   - Quick start (2-line Tiltfile), config options table, prerequisites, troubleshooting
2. Create `examples/observability/Tiltfile` with real usage
3. Create `examples/observability/notebooks/analyze-telemetry.ipynb` (Python + DuckDB)
4. Inline Starlark docstrings in `extension.star`
5. Create `specs/002-observability-stack/quickstart.md`

## Complexity Tracking

No constitution violations. All components follow established patterns (Starlark extension + Go support code + golden file testing).
