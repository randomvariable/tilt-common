# Tasks: Observability Stack Extension

**Input**: Design documents from `/specs/002-observability-stack/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md

**Tests**: Tests are included based on FR-042 (all tests in Go using gomega) and the plan's testing strategy (Tier 1: smoke, Tier 2: golden files, Tier 3: E2E).

**Organization**: Tasks are grouped by implementation phase from plan.md, with user story context for traceability.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Following plan.md structure:
- Extensions: `extensions/observability/`
- Export CLI: `cmd/observability-export/`
- Tests: `tests/observability/`
- Examples: `examples/observability/`
- kindgpu library: `extensions/kind-gpu/` (already implemented)

---

## Phase 1: Core Extension Skeleton + Config Validation

**Purpose**: Foundation - `enable_observability()` function with config validation, no deployments yet

**User Stories Supported**: Foundational infrastructure for all stories

- [X] T001 Create extension directory structure: extensions/observability/extension.star, extensions/observability/README.md, extensions/observability/assets/
- [X] T002 Implement enable_observability() function signature in extensions/observability/extension.star with ExtensionConfig parameters (namespace, metrics, logs, traces, prometheus_metrics, otel, profiling, ports, images, export_dir)
- [X] T003 [P] Implement config validation helpers in extensions/observability/extension.star: validate_port_range (1-65535), validate_namespace (RFC 1123), reject_unknown_keys (FR-032)
- [X] T004 [P] Implement prometheus_metrics dependency guard in extensions/observability/extension.star (FR-020: prometheus_metrics requires metrics=True)
- [X] T005 [P] Apply default values in extensions/observability/extension.star (metrics/logs/traces=True, prometheus_metrics/otel/profiling=False per FR-001)
- [X] T006 [P] Implement no-op behavior when all flags disabled in extensions/observability/extension.star (FR-003)
- [ ] T007 Create tests/tilttest/helpers.go with TiltfileResult helper function (wraps `tilt alpha tiltfile-result`, parses JSON output)
- [ ] T008 [P] Write Tier 1 smoke tests in tests/observability/extension_test.go: TestExtensionLoadsWithDefaults
- [ ] T009 [P] Write config validation tests in tests/observability/extension_test.go: TestInvalidPort, TestUnknownKeys, TestPrometheusMetricsWithoutMetrics (table-driven with gomega)

**Checkpoint**: Extension loads without errors, validates config, deploys nothing when all flags disabled

---

## Phase 2: Core Signal Backends (VictoriaMetrics, VictoriaLogs + Vector, VictoriaTraces)

**Purpose**: Deploy metrics/logs/traces backends with health checks (US1, US7, US8)

**User Stories Supported**: US1 (full stack), US7 (export needs backends), US8 (OTel endpoints)

### Backend Manifests

- [X] T010 [P] [US1] Create VictoriaMetrics manifest in extensions/observability/assets/metrics.yaml: Deployment (port 8428, retention flag, health probes), Service, RBAC with tilt-observability- prefix (FR-004, FR-033a, NFR-005)
- [X] T011 [P] [US1] Create VictoriaLogs manifest in extensions/observability/assets/logs.yaml: Deployment (port 9428, retention flag, health probes), Service, RBAC with tilt-observability- prefix (FR-005, FR-033a, NFR-005)
- [X] T012 [P] [US1] Create Vector manifest in extensions/observability/assets/vector.yaml: DaemonSet with host path mounts, RBAC with tilt-observability- prefix (FR-005)
- [X] T013 [P] [US1] Create Vector config in extensions/observability/assets/vector-config.yaml: ConfigMap with kubernetes_logs source, VictoriaLogs sink (data-model.md entity 8)
- [X] T014 [P] [US1] Create VictoriaTraces manifest in extensions/observability/assets/traces.yaml: Deployment (ports 10428 HTTP + 4317 gRPC, retention flag, health probes), Service, RBAC with tilt-observability- prefix (FR-006, FR-033a, NFR-005)

### Extension Wiring

- [X] T015 [US1] Wire metrics flag in extensions/observability/extension.star: conditionally apply metrics.yaml, add port-forward (port 8428), label group "observability"
- [X] T016 [US1] Wire logs flag in extensions/observability/extension.star: conditionally apply logs.yaml + vector.yaml + vector-config.yaml, add port-forward (port 9428), Tilt resource dependency (victorialogs -> vector)
- [X] T017 [US1] Wire traces flag in extensions/observability/extension.star: conditionally apply traces.yaml, add port-forward (port 10428), label group "observability"
- [X] T018 [US1] [US3] Print OTel environment variables in extensions/observability/extension.star: generate OTLP endpoint URLs for enabled backends, print to Tilt log via print() and local_resource (FR-011, FR-012, FR-013)

### Tests

- [ ] T019 [P] [US1] Create golden file reference for metrics.yaml in tests/observability/golden/metrics.yaml (expected output with defaults)
- [ ] T020 [P] [US1] Create golden file reference for logs.yaml + vector in tests/observability/golden/logs.yaml, tests/observability/golden/vector.yaml
- [ ] T021 [P] [US1] Create golden file reference for traces.yaml in tests/observability/golden/traces.yaml
- [ ] T022 [US1] Write Tier 2 golden file tests in tests/observability/extension_test.go: TestMetricsManifestOutput, TestLogsManifestOutput, TestTracesManifestOutput (compare tilt alpha tiltfile-result YAML against golden files)
- [ ] T023 [US1] Write golden file test for OTel env var output in tests/observability/extension_test.go: TestOtelEnvVarOutput (verify correct endpoints printed)

**Checkpoint**: Core backends deploy with health checks, golden file tests pass, OTel env vars printed

---

## Phase 3: Grafana Operator + Datasources

**Purpose**: Auto-deploy Grafana Operator and Grafana instance with datasources (US1, US6)

**User Stories Supported**: US1 (visualization), US6 (custom dashboards via CRD)

### Grafana Operator Setup

- [ ] T024 [US1] Research and document Grafana Operator Helm chart usage in extensions/observability/extension.star comments: OCI registry URL, chart version, required values (Decision 14)
- [ ] T025 [US1] Implement Grafana Operator Helm rendering in extensions/observability/extension.star: helm_resource() or local() + helm template with values (auto-deployed when any flag enabled per FR-002)
- [ ] T026 [P] [US1] Create Grafana CR manifest in extensions/observability/assets/grafana.yaml: Grafana custom resource (grafana.integreatly.org/v1beta1) with admin/admin credentials, port 3000 (FR-008)
- [ ] T027 [P] [US1] Create GrafanaDatasource manifests in extensions/observability/assets/grafana.yaml: datasource CRs for VictoriaMetrics (prometheus type), VictoriaLogs (victoriametrics-logs-datasource type), VictoriaTraces (jaeger type) - conditional on enabled backends (data-model.md entity 3, FR-010)

### Extension Wiring

- [ ] T028 [US1] Wire Grafana Operator deployment in extensions/observability/extension.star: deploy when any component flag enabled, Tilt resource dependencies (backends -> grafana-operator -> grafana per FR-010)
- [ ] T029 [US1] Wire Grafana port-forward in extensions/observability/extension.star: port 3000, label group "observability"

### Tests

- [ ] T030 [P] [US1] Create golden file reference for Grafana Operator Helm output in tests/observability/golden/grafana-operator.yaml
- [ ] T031 [P] [US1] Create golden file reference for Grafana CR in tests/observability/golden/grafana.yaml
- [ ] T032 [US1] Write golden file tests in tests/observability/extension_test.go: TestGrafanaOperatorOutput, TestGrafanaDatasourceConditional (verify datasources only created for enabled backends)

**Checkpoint**: Grafana Operator auto-deploys, datasources provisioned for active backends, golden tests pass

---

## Phase 4: VMAgent + Prometheus Scraping (Annotations + CRDs)

**Purpose**: Deploy VMAgent with annotation-based and CRD-based Prometheus scraping (US5)

**User Stories Supported**: US5 (Prometheus metrics scraping)

### VMAgent Manifests

- [ ] T033 [P] [US5] Create VMAgent manifest in extensions/observability/assets/vmagent.yaml: Deployment (port 8429, remote write to VictoriaMetrics), Service, RBAC (ServiceAccount, ClusterRole with get/list/watch nodes/pods/services/endpoints, ClusterRoleBinding with tilt-observability- prefix per FR-033a)
- [ ] T034 [P] [US5] Create VMAgent config in extensions/observability/assets/vmagent-config.yaml: ConfigMap with kubernetes_sd_configs (role: endpoints, role: pod), relabeling rules for prometheus.io/scrape annotations (FR-021, Decision 16)
- [ ] T035 [US5] Add Prometheus Operator CRD discovery in extensions/observability/assets/vmagent-config.yaml: VMAgent flags for ServiceMonitor, PodMonitor, ScrapeConfig, Probe CRDs (FR-022, FR-022a)
- [ ] T036 [P] [US5] Fetch Prometheus Operator CRDs in extensions/observability/extension.star: download CRDs from prometheus-operator/prometheus-operator GitHub releases, install via server-side apply with force-conflicts (Decision 16)

### Extension Wiring

- [ ] T037 [US5] Wire prometheus_metrics flag in extensions/observability/extension.star: deploy VMAgent + CRDs when enabled, Tilt resource dependency (VMAgent -> VictoriaMetrics per FR-020), label group "observability"

### Tests

- [ ] T038 [P] [US5] Create golden file reference for VMAgent manifest in tests/observability/golden/vmagent.yaml
- [ ] T039 [P] [US5] Create golden file reference for VMAgent config in tests/observability/golden/vmagent-config.yaml
- [ ] T040 [US5] Write golden file tests in tests/observability/extension_test.go: TestVMAgentManifestOutput, TestVMAgentConfig (verify annotation and CRD discovery)
- [ ] T041 [US5] Write validation test in tests/observability/extension_test.go: TestPrometheusMetricsRequiresMetrics (verify prometheus_metrics without metrics produces error per FR-020)

**Checkpoint**: VMAgent deploys with annotation and CRD scraping, CRDs installed, golden tests pass

---

## Phase 5: OTel Operator + Instrumentation CR

**Purpose**: Deploy OTel Operator webhook for automatic OTLP env var injection (US8)

**User Stories Supported**: US8 (auto-injection of OTel env vars)

### OTel Operator Setup

- [ ] T042 [US8] Research and document OTel Operator Helm chart in extensions/observability/extension.star comments: chart repo URL, version (v0.105.1), values for self-signed certs (Decision 13)
- [ ] T043 [US8] Implement OTel Operator Helm rendering in extensions/observability/extension.star: helm template with values (admissionWebhooks.certManager.enabled=false, autoGenerateCert.enabled=true, failurePolicy=Ignore per FR-017, FR-018)
- [ ] T044 [P] [US8] Create Instrumentation CR template in extensions/observability/extension.star: generate Instrumentation CR YAML with OTLP endpoint env vars for enabled backends (data-model.md entity 6, FR-015, FR-016)
- [ ] T045 [US8] Wire otel flag in extensions/observability/extension.star: deploy OTel Operator + Instrumentation CR when enabled, Tilt resource dependency (instrumentation-cr -> otel-operator), label group "observability"

### Tests

- [ ] T046 [P] [US8] Create golden file reference for OTel Operator Helm output in tests/observability/golden/otel-operator.yaml
- [ ] T047 [P] [US8] Create golden file reference for Instrumentation CR in tests/observability/golden/instrumentation-cr.yaml
- [ ] T048 [US8] Write golden file tests in tests/observability/extension_test.go: TestOtelOperatorOutput, TestInstrumentationCRConditional (verify env vars only for enabled backends per FR-016)

**Checkpoint**: OTel Operator deploys with webhook, Instrumentation CR created with correct env vars, golden tests pass

---

## Phase 6: Parca Profiling

**Purpose**: Deploy Parca server and agent for continuous profiling (US4)

**User Stories Supported**: US4 (continuous profiling)

### Parca Manifests

- [ ] T049 [P] [US4] Create Parca Server manifest in extensions/observability/assets/parca.yaml: Deployment (port 7070, health probes), Service, RBAC with tilt-observability- prefix (FR-023, FR-033a)
- [ ] T050 [P] [US4] Create Parca Agent manifest in extensions/observability/assets/parca.yaml: DaemonSet with privileged security context, host path mounts for kernel filesystems, RBAC with tilt-observability- prefix (FR-023)
- [ ] T051 [US4] Implement containerd socket auto-detection in extensions/observability/extension.star: ordered probing (k3s -> microk8s -> standard per Decision 8), fail with clear error if not found (FR-025)

### Extension Wiring

- [ ] T052 [US4] Wire profiling flag in extensions/observability/extension.star: deploy Parca Server + Agent when enabled, Tilt resource dependency (parca-agent -> parca-server), standalone (no dependency on other backends per FR-024), label group "observability"

### Tests

- [ ] T053 [P] [US4] Create golden file reference for Parca manifest in tests/observability/golden/parca.yaml
- [ ] T054 [US4] Write golden file tests in tests/observability/extension_test.go: TestParcaManifestOutput
- [ ] T055 [US4] Write validation test in tests/observability/extension_test.go: TestProfilingStandalone (verify profiling deploys without any backends enabled per FR-024)

**Checkpoint**: Parca deploys standalone, containerd socket auto-detected, golden tests pass

---

## Phase 7: Export CLI Tool

**Purpose**: Go CLI exports metrics, traces, and pprof profiles to DuckDB + files (US7)

**User Stories Supported**: US7 (export telemetry data)

### Export CLI Implementation

- [ ] T056 Create cmd/observability-export/main.go with CLI entry point: flags (export-dir, metrics-url, traces-url, parca-url), cobra or flag package
- [ ] T057 [P] [US7] Implement DuckDB schema in cmd/observability-export/duckdb.go: metrics table, spans table (Decision 2), Appender-based bulk inserts
- [ ] T058 [P] [US7] Implement VictoriaMetrics export in cmd/observability-export/metrics.go: fetch from /api/v1/export (JSON lines), parse to DuckDB metrics table (Decision 4)
- [ ] T059 [P] [US7] Implement VictoriaTraces export in cmd/observability-export/traces.go: fetch from Jaeger API, parse to DuckDB spans table (Decision 5)
- [ ] T060 [P] [US7] Implement Parca export in cmd/observability-export/profiles.go: fetch pprof profiles via /api/v1/profiles/query, save as .pb.gz files (Decision 6)
- [ ] T061 [US7] Implement export orchestration in cmd/observability-export/export.go: parallel export goroutines, partial success handling (FR-037), timestamped output filename (FR-035, FR-036)

### Extension Integration

- [ ] T062 [US7] Wire export CLI as Tilt button in extensions/observability/extension.star: local_resource with button trigger, calls observability-export binary (FR-037)
- [ ] T063 [US7] Add export_dir config handling in extensions/observability/extension.star: default ./observability-export, env var override (FR-038, NFR-003)

### Tests

- [ ] T064 [P] [US7] Write unit tests for metrics exporter in cmd/observability-export/metrics_test.go: mock VictoriaMetrics responses, verify DuckDB writes
- [ ] T065 [P] [US7] Write unit tests for traces exporter in cmd/observability-export/traces_test.go: mock Jaeger API responses, verify DuckDB writes
- [ ] T066 [P] [US7] Write unit tests for profiles exporter in cmd/observability-export/profiles_test.go: mock Parca API responses, verify .pb.gz file creation

### Mage Target

- [ ] T067 [US7] Add build:exportCLI target in magefile.go: build cmd/observability-export with CGO_ENABLED=1 (Decision 1)

**Checkpoint**: Export CLI builds and runs, DuckDB exports work, unit tests pass

---

## Phase 8: kindgpu Migration + E2E Tests

**Purpose**: E2E tests use kindgpu Go library for cluster lifecycle (replaces k3d)

**User Stories Supported**: All (E2E validation)

### Migration Tasks

- [ ] T068 Update tests/observability/e2e_test.go: replace k3d cluster create/delete with kindgpu.CreateClusterFromConfig / kindgpu.DeleteCluster (Decision 18, Decision 21)
- [ ] T069 [P] Update E2E Tiltfile kubeconfig context: change allow_k8s_contexts() from k3d-tilt-common-e2e to kind-tilt-common-e2e (Decision 22)
- [ ] T070 [P] Remove default_registry('localhost:5005') from E2E Tiltfile: rely on Tilt's auto-detection of kind clusters (Decision 19)
- [ ] T071 [P] Delete stale k3d test scripts: tests/k3d-gpu/test-config-validation.sh, tests/k3d-gpu/test-load.sh, tests/k3d-gpu/test-output-validation.sh, extensions/k3d-gpu/test.sh (Decision 21)

### E2E Test Suite

- [ ] T072 Write E2E test setup in tests/observability/e2e_test.go: TestMain with kindgpu cluster lifecycle, registry mirror setup (Decision 20)
- [ ] T073 [P] Write full stack E2E test in tests/observability/e2e_test.go: TestFullObservabilityStack (tilt ci, verify all backends healthy, SC-002)
- [ ] T074 [P] Write selective component E2E test in tests/observability/e2e_test.go: TestSelectiveComponents (metrics only, logs only, traces only, verify conditional deployment)
- [ ] T075 [P] Write VMAgent scraping E2E test in tests/observability/e2e_test.go: TestPrometheusScrapingAnnotations (deploy annotated service, verify metrics in VictoriaMetrics)
- [ ] T076 [P] Write OTel injection E2E test in tests/observability/e2e_test.go: TestOtelOperatorInjection (deploy pod with inject-sdk annotation, verify env vars present)
- [ ] T077 [P] Write export pipeline E2E test in tests/observability/e2e_test.go: TestExportPipeline (send test telemetry, trigger export, verify DuckDB file + pprof files per FR-041, SC-006)

**Checkpoint**: All E2E tests pass, kindgpu cluster lifecycle works, SC-002 validated (90 second warm start)

---

## Phase 9: Documentation + Examples

**Purpose**: Complete documentation per constitution requirements

**User Stories Supported**: All (documentation)

### Documentation

- [ ] T078 [P] Write extensions/observability/README.md: quick start (2-line Tiltfile per SC-001), config options table, prerequisites (Helm, CGO), troubleshooting
- [ ] T079 [P] Add inline Starlark docstrings in extensions/observability/extension.star: enable_observability() function signature, all config parameters, return values
- [ ] T080 [P] Create specs/002-observability-stack/quickstart.md: step-by-step guide with validation checkpoints from spec.md scenarios

### Examples

- [ ] T081 [P] Create examples/observability/Tiltfile: realistic example using enable_observability() with custom ports, selective components
- [ ] T082 [P] Create examples/observability/notebooks/analyze-telemetry.ipynb: Python + DuckDB example querying exported metrics and traces (FR-040, SC-007)
- [ ] T083 [P] Create examples/observability/notebooks/pprof-example.sh: shell script showing go tool pprof usage with exported profiles (FR-040, SC-007)

### Final Validation

- [ ] T084 Run quickstart.md validation: execute all quickstart steps end-to-end, verify all scenarios pass
- [ ] T085 Validate Jupyter notebook examples: run analyze-telemetry.ipynb with sample exported data, verify queries work (SC-007)
- [ ] T086 Validate pprof example: run pprof-example.sh with exported profiles, verify go tool pprof loads correctly (SC-007)

**Checkpoint**: Documentation complete, examples validated, constitution requirement IV satisfied

---

## Phase 10: Polish & Cross-Cutting Concerns

**Purpose**: Final improvements affecting multiple user stories

- [ ] T087 [P] Run golangci-lint --fix on all Go code: cmd/observability-export/, tests/observability/, tests/tilttest/ (constitution requirement: no linter exclusions)
- [ ] T088 [P] Add resource requests/limits to all manifests: ensure total footprint <= 1 CPU, 1.5 GiB per NFR-002
- [ ] T089 [P] Add readiness/liveness probes to all components per FR-029: verify health check paths and timeouts
- [ ] T090 [P] Verify all RBAC resources use tilt-observability- prefix per FR-033a: search all manifests for ClusterRole, ClusterRoleBinding
- [ ] T091 [P] Verify all retention flags set per NFR-005: VictoriaMetrics -retentionPeriod, VictoriaLogs, VictoriaTraces
- [ ] T092 [P] Add idempotency validation: call enable_observability() multiple times, verify no errors (NFR-004)
- [ ] T093 Code cleanup and refactoring: extract common patterns, remove duplication, consistent naming
- [ ] T094 Performance validation: verify SC-002 (90 second warm start) with actual timings in E2E test logs
- [ ] T095 Security review: verify no hardcoded credentials, TLS for webhooks, RBAC least privilege
- [ ] T096 Update CLAUDE.md: run .specify/scripts/bash/update-agent-context.sh claude to capture final tech stack

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: No dependencies - can start immediately
- **Phase 2 (Core Backends)**: Depends on Phase 1 (extension skeleton must exist)
- **Phase 3 (Grafana)**: Depends on Phase 2 (backends must exist for datasources)
- **Phase 4 (VMAgent)**: Depends on Phase 2 (VictoriaMetrics must exist for remote write)
- **Phase 5 (OTel Operator)**: Can start after Phase 1 (independent of backends)
- **Phase 6 (Parca)**: Can start after Phase 1 (standalone, no backend dependencies)
- **Phase 7 (Export CLI)**: Depends on Phase 2 (backends must exist to export from)
- **Phase 8 (E2E Tests)**: Depends on Phases 1-7 (all features must be implemented)
- **Phase 9 (Documentation)**: Can start anytime, finalize after Phase 8
- **Phase 10 (Polish)**: Depends on all previous phases

### User Story Dependencies

- **US1 (Full Stack, P1)**: Phases 2-3 - Core backends + Grafana (MVP)
- **US2 (Port Config, P2)**: Phase 1 - Config validation
- **US3 (OTel Env Vars, P2)**: Phases 2, 5 - Print env vars + OTel Operator
- **US4 (Profiling, P3)**: Phase 6 - Parca standalone
- **US5 (VMAgent Scraping, P1)**: Phase 4 - VMAgent + CRDs
- **US6 (Custom Dashboards, P3)**: Phase 3 - GrafanaDashboard CRD
- **US7 (Export, P2)**: Phase 7 - Export CLI
- **US8 (OTel Injection, P1)**: Phase 5 - OTel Operator

### Within Each Phase

- Setup tasks → Implementation tasks → Test tasks
- Manifests can be created in parallel [P]
- Extension wiring after manifests
- Golden file tests after implementation
- E2E tests after all features complete

### Parallel Opportunities

**Phase 2 (Core Backends)**: T010-T014 all [P] (different manifest files)
**Phase 3 (Grafana)**: T026-T027 [P], T030-T031 [P] (different manifests)
**Phase 4 (VMAgent)**: T033-T034 [P], T038-T039 [P]
**Phase 7 (Export CLI)**: T057-T060 all [P] (different Go files), T064-T066 all [P] (different test files)
**Phase 8 (E2E)**: T069-T071 [P], T073-T077 all [P] (independent test functions)
**Phase 9 (Docs)**: T078-T083 all [P] (different doc/example files)
**Phase 10 (Polish)**: T087-T092 all [P] (different validation concerns)

---

## Parallel Example: Phase 2 (Core Backends)

```bash
# Launch all manifest creation tasks together:
Task: "Create VictoriaMetrics manifest in extensions/observability/assets/metrics.yaml"
Task: "Create VictoriaLogs manifest in extensions/observability/assets/logs.yaml"
Task: "Create Vector manifest in extensions/observability/assets/vector.yaml"
Task: "Create Vector config in extensions/observability/assets/vector-config.yaml"
Task: "Create VictoriaTraces manifest in extensions/observability/assets/traces.yaml"

# Launch all golden file creation tasks together:
Task: "Create golden file reference for metrics.yaml in tests/observability/golden/metrics.yaml"
Task: "Create golden file reference for logs.yaml in tests/observability/golden/logs.yaml"
Task: "Create golden file reference for traces.yaml in tests/observability/golden/traces.yaml"
```

---

## Implementation Strategy

### MVP First (User Story 1: Full Stack)

1. Complete Phase 1: Setup (T001-T009)
2. Complete Phase 2: Core Backends (T010-T023) - Delivers US1
3. Complete Phase 3: Grafana (T024-T032) - Completes US1
4. **STOP and VALIDATE**: Test full stack independently (SC-001, SC-002, SC-003)
5. Deploy/demo MVP

### Incremental Delivery

1. MVP (Phases 1-3) → Full observability stack working (US1)
2. Add Phase 4 (VMAgent) → Prometheus scraping (US5)
3. Add Phase 5 (OTel Operator) → Auto-injection (US8)
4. Add Phase 6 (Parca) → Profiling (US4)
5. Add Phase 7 (Export) → Data export (US7)
6. Add Phase 8 (E2E) → Full validation (SC-006)
7. Add Phase 9 (Docs) → Complete documentation
8. Add Phase 10 (Polish) → Production-ready

### Parallel Team Strategy

With multiple developers:

1. Team completes Phase 1 together (foundation)
2. Phase 2-7 can be parallelized:
   - Developer A: Phase 2-3 (Core + Grafana) - US1 MVP
   - Developer B: Phase 4 (VMAgent) - US5
   - Developer C: Phase 5 (OTel Operator) - US8
   - Developer D: Phase 6 (Parca) - US4
   - Developer E: Phase 7 (Export CLI) - US7
3. Phase 8: Team integrates for E2E tests
4. Phase 9-10: Distributed polish and docs

---

## Notes

- [P] tasks = different files, no dependencies - can launch in parallel
- [Story] label maps task to spec.md user story for traceability
- Tests follow Decision 10: Tier 1 (smoke), Tier 2 (golden files), Tier 3 (E2E)
- Golden files enable regression detection - update with `go test -update`
- CGO required for DuckDB - ensure golang:1.23 CI image includes GCC (Decision 1)
- Helm CLI required for OTel Operator and Grafana Operator rendering (FR-018)
- kindgpu library already implemented with 12 passing unit tests (Decision 18-23)
- Stop at any checkpoint to validate independently before proceeding
- Commit after each task or logical group
- Avoid: vague tasks, same file conflicts, breaking constitution requirements
