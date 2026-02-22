# Data Model: Observability Stack Extension

**Feature**: 002-observability-stack | **Date**: 2026-02-15 | **Spec**: [spec.md](spec.md)

## Entity Overview

```text
ExtensionConfig (1)──────(0..8) ObservabilityComponent
       │                            │
       │                        (0..1) Service
       │                        (0..1) RBAC
       │
       ├──(0..1) GrafanaOperator ──── (1) Grafana CR
       │                              (0..3) GrafanaDatasource CRs
       │                              (0..N) GrafanaDashboard CRs (user-created)
       │
       ├──(0..1) VMAgent ──── (1) ScrapeConfig (annotations + CRDs)
       │                      (0..N) ServiceMonitor CRs (user-created)
       │                      (0..N) PodMonitor CRs (user-created)
       │
       ├──(0..1) OTelOperator ──── (1) InstrumentationCR
       │                           (1) MutatingWebhookConfig
       │                           (1) TLS Secret
       │
       ├──(0..1) Vector ──── (1) ConfigMap (pipeline config)
       │
       ├──(1) OTelEnvironmentBlock
       └──(1) ExportConfig ──── (1) TelemetryExport
                                     │
                                 (1) DuckDB file
                                 (0..N) pprof files
```

## Entities

### 1. ExtensionConfig

The top-level configuration dictionary passed to `enable_observability()`.

| Field | Type | Default | Validation | Spec Ref |
|-------|------|---------|------------|----------|
| `namespace` | string | `"default"` | RFC 1123 DNS label | FR-031 |
| `metrics` | bool | `True` | - | FR-001 |
| `logs` | bool | `True` | - | FR-001 |
| `traces` | bool | `True` | - | FR-001 |
| `prometheus_metrics` | bool | `False` | Requires `metrics=True` (FR-020) | FR-001, FR-020 |
| `otel` | bool | `False` | - | FR-001 |
| `profiling` | bool | `False` | Standalone (no backend required) | FR-001, FR-024 |
| `ports` | dict | See FR-027 | 1-65535 integer range | FR-026, FR-027 |
| `ports.metrics` | int | `8428` | - | FR-027 |
| `ports.logs` | int | `9428` | - | FR-027 |
| `ports.traces` | int | `10428` | - | FR-027 |
| `ports.grafana` | int | `3000` | - | FR-027 |
| `ports.parca` | int | `7070` | - | FR-027 |
| `images` | dict | `{}` | Non-empty string values | FR-033 |
| `export_dir` | string | `"./observability-export"` | Valid directory path | FR-038 |

**Validation Rules** (FR-032):
- Port values: integer, 1 <= port <= 65535
- Namespace: lowercase, no spaces/dots/underscores, non-empty
- Image strings: non-empty when provided
- `prometheus_metrics=True` requires `metrics=True` (FR-020)
- `profiling=True` can run standalone without any core backends (FR-024)
- Unknown config keys are rejected with `fail()` (FR-032)

### 2. ObservabilityComponent

A Kubernetes workload deployed by the extension. Each component has a fixed identity.

| Component ID | K8s Kind | Container Port(s) | Service Port(s) | Flag | Spec Ref |
|-------------|----------|-------------------|-----------------|------|----------|
| `victoriametrics` | Deployment | 8428 | 8428 | `metrics` | FR-004 |
| `victorialogs` | Deployment | 9428 | 9428 | `logs` | FR-005 |
| `vector` | DaemonSet | - | - | `logs` | FR-005 |
| `victoriatraces` | Deployment | 10428 (HTTP), 4317 (gRPC) | 10428, 4317 | `traces` | FR-006 |
| `grafana-operator` | Deployment (Helm) | - | - | (any flag) | FR-007 |
| `grafana` | Deployment (via Grafana CR) | 3000 | 3000 | (any flag) | FR-008 |
| `vmagent` | Deployment | 8429 | 8429 | `prometheus_metrics` | FR-019 |
| `otel-operator` | Deployment (Helm) | - | - | `otel` | FR-014 |
| `parca-server` | Deployment | 7070 | 7070 | `profiling` | FR-023 |
| `parca-agent` | DaemonSet | - | - | `profiling` | FR-023 |

**Common Fields** (per component):
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Kubernetes resource name |
| `namespace` | string | From ExtensionConfig |
| `image` | string | Container image reference (default or override) |
| `labels` | dict | `app: <name>`, `app.kubernetes.io/managed-by: tilt-observability` |
| `resource_requests` | dict | CPU + memory requests |
| `resource_limits` | dict | CPU + memory limits |
| `readiness_probe` | probe | HTTP GET health check (FR-029) |
| `liveness_probe` | probe | HTTP GET health check (FR-029) |

### 3. GrafanaOperator

The Grafana Operator (`grafana.integreatly.org`) deployed via Helm when any component flag is enabled.

| Sub-Resource | K8s Kind | Description | Spec Ref |
|-------------|----------|-------------|----------|
| Operator Deployment | Deployment (Helm) | Grafana Operator controller | FR-007 |
| Grafana CR | `Grafana` (CRD) | Managed Grafana instance | FR-008 |
| GrafanaDatasource CRs | `GrafanaDatasource` (CRD) | Auto-provisioned per enabled backend | FR-008, FR-010 |
| GrafanaDashboard CRs | `GrafanaDashboard` (CRD) | User-created; operator reconciles them | FR-009 |
| CRDs | CustomResourceDefinition | Grafana, GrafanaDatasource, GrafanaDashboard, etc. | FR-007 |

**GrafanaDatasource CRs** (conditional on enabled backends, FR-010):

| Backend Enabled | Datasource Type | UID | URL Template |
|----------------|-----------------|-----|-------------|
| `metrics=True` | `prometheus` | `victoriametrics` | `http://victoriametrics.{ns}.svc.cluster.local:8428` |
| `logs=True` | `victoriametrics-logs-datasource` | `victorialogs` | `http://victorialogs.{ns}.svc.cluster.local:9428` |
| `traces=True` | `jaeger` | `victoriatraces` | `http://victoriatraces.{ns}.svc.cluster.local:10428` |

**Helm Chart**: `grafana-operator` from `oci://ghcr.io/grafana/helm-charts/grafana-operator`.

### 4. OTelEnvironmentBlock

Environment variables printed to Tilt logs/UI for developer reference. Non-service-specific (always the same for all applications).

| Variable | Value Template | Condition | Spec Ref |
|----------|---------------|-----------|----------|
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | `http://victoriametrics.{ns}.svc.cluster.local:8428/opentelemetry/api/v1/push` | `metrics=True` | FR-011, FR-013 |
| `OTEL_EXPORTER_OTLP_METRICS_PROTOCOL` | `http/protobuf` | `metrics=True` | FR-011 |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | `http://victorialogs.{ns}.svc.cluster.local:9428/insert/opentelemetry/v1/logs` | `logs=True` | FR-011, FR-013 |
| `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL` | `http/protobuf` | `logs=True` | FR-011 |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | `http://victoriatraces.{ns}.svc.cluster.local:4317` | `traces=True` | FR-011, FR-013 |
| `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` | `grpc` | `traces=True` | FR-011 |

### 5. VMAgentScrapeConfig

Scrape configuration for VMAgent, supporting both annotation-based discovery and Prometheus Operator CRDs.

**Annotation-based Discovery** (FR-021):
VMAgent uses `kubernetes_sd_configs` with relabeling rules to auto-discover services/pods with these annotations:
- `prometheus.io/scrape: "true"` -- enables scraping
- `prometheus.io/port: "<port>"` -- overrides scrape port
- `prometheus.io/path: "<path>"` -- overrides metrics path (default `/metrics`)
- `prometheus.io/scheme: "<scheme>"` -- overrides scheme (default `http`)

**Prometheus Operator CRD Discovery** (FR-022):
VMAgent discovers and honors these CRDs (installed by the extension):
- `ServiceMonitor` -- targets Services by label selector
- `PodMonitor` -- targets Pods by label selector
- `ScrapeConfig` -- static targets and custom SD configs
- `Probe` -- blackbox monitoring probes

**CRD Source**: CRDs from `prometheus-operator/prometheus-operator` GitHub releases.

### 6. OTelOperator

The OpenTelemetry Operator deployment and associated resources for automatic OTLP env var injection via mutating admission webhook.

| Sub-Resource | K8s Kind | Description | Spec Ref |
|-------------|----------|-------------|----------|
| Operator Deployment | Deployment | Controller + webhook server (rendered via `helm template`) | FR-014, FR-018 |
| Operator Service | Service | Webhook endpoint for MutatingWebhookConfiguration | FR-014 |
| MutatingWebhookConfiguration | MutatingWebhookConfiguration | Intercepts pod creation to inject OTLP env vars; `failurePolicy: Ignore` | FR-014, FR-017 |
| TLS Secret | Secret | Auto-generated self-signed certificate for webhook TLS | FR-017 |
| Instrumentation CR | Instrumentation (CRD) | Defines OTLP endpoint env vars to inject into annotated pods | FR-015, FR-016 |
| CRDs | CustomResourceDefinition | Instrumentation, OpenTelemetryCollector, etc. (installed by Helm chart) | FR-014 |
| RBAC | ServiceAccount + ClusterRole + ClusterRoleBinding | Operator permissions | FR-014 |

**Instrumentation CR Fields** (conditional on enabled backends, FR-016):

| Field | Value Template | Condition |
|-------|---------------|-----------|
| `spec.env[OTEL_EXPORTER_OTLP_METRICS_ENDPOINT]` | `http://victoriametrics.{ns}.svc.cluster.local:8428/opentelemetry/api/v1/push` | `metrics=True` |
| `spec.env[OTEL_EXPORTER_OTLP_METRICS_PROTOCOL]` | `http/protobuf` | `metrics=True` |
| `spec.env[OTEL_EXPORTER_OTLP_LOGS_ENDPOINT]` | `http://victorialogs.{ns}.svc.cluster.local:9428/insert/opentelemetry/v1/logs` | `logs=True` |
| `spec.env[OTEL_EXPORTER_OTLP_LOGS_PROTOCOL]` | `http/protobuf` | `logs=True` |
| `spec.env[OTEL_EXPORTER_OTLP_TRACES_ENDPOINT]` | `http://victoriatraces.{ns}.svc.cluster.local:4317` | `traces=True` |
| `spec.env[OTEL_EXPORTER_OTLP_TRACES_PROTOCOL]` | `grpc` | `traces=True` |

**Helm Chart**: `opentelemetry-operator` from `https://open-telemetry.github.io/opentelemetry-helm-charts` (chart v0.105.1, app v0.144.0). See [research.md](research.md) Decision 13 for Helm values and rationale.

### 7. TelemetryExport

Output of the export pipeline.

| Artifact | Format | Location | Spec Ref |
|----------|--------|----------|----------|
| DuckDB database | `.duckdb` file | `{export_dir}/telemetry-{timestamp}.duckdb` | FR-035, FR-036 |
| pprof profiles | `.pb.gz` files | `{export_dir}/profiles/` | FR-039 |

**DuckDB Tables**: See [research.md](research.md) Decision 2 for schema details.

### 8. Vector

Log collection and forwarding agent deployed alongside VictoriaLogs when `logs` flag is enabled.

| Field | Type | Description | Spec Ref |
|-------|------|-------------|----------|
| `sources` | config | Kubernetes logs source (`kubernetes_logs`), OTLP source | FR-005 |
| `transforms` | config | Log parsing, enrichment, structuring | FR-005 |
| `sinks` | config | VictoriaLogs HTTP API sink | FR-005 |

**Deployment**: DaemonSet with host path mounts for `/var/log` and containerd log directories. Forwards to VictoriaLogs at `http://victorialogs.{ns}.svc.cluster.local:9428`.

## Relationships

| From | To | Cardinality | Description |
|------|----|-------------|-------------|
| ExtensionConfig | ObservabilityComponent | 1:0..10 | Config determines which components are deployed |
| ExtensionConfig | OTelEnvironmentBlock | 1:1 | Config determines which env vars are printed |
| ExtensionConfig | VMAgentScrapeConfig | 1:0..1 | Generated only when `prometheus_metrics` enabled |
| ExtensionConfig | ExportConfig | 1:1 | Export dir and enabled components |
| ExtensionConfig | GrafanaOperator | 1:0..1 | Deployed when any flag is enabled |
| ExtensionConfig | OTelOperator | 1:0..1 | Deployed when `otel` flag enabled |
| ExtensionConfig | Vector | 1:0..1 | Deployed when `logs` flag enabled |
| GrafanaOperator | Grafana CR | 1:1 | Operator manages the Grafana instance |
| GrafanaOperator | GrafanaDatasource CRs | 1:0..3 | Conditional on enabled backends |
| GrafanaOperator | GrafanaDashboard CRs | 1:0..N | User-created, operator reconciles |
| VMAgent | VictoriaMetrics | 1:1 | Remote write dependency |
| VMAgent | ServiceMonitor/PodMonitor CRs | 1:0..N | CRD-based scrape target discovery |
| Vector | VictoriaLogs | 1:1 | Log forwarding dependency |
| Parca Server | Parca Agent | 1:1 | Agent sends profiles to server |
| TelemetryExport | VictoriaMetrics | 0..1:1 | Export reads from metrics backend |
| TelemetryExport | VictoriaTraces | 0..1:1 | Export reads from traces backend |
| TelemetryExport | Parca Server | 0..1:1 | Export reads profiles from parca |
| OTelOperator | InstrumentationCR | 1:1 | Operator manages the Instrumentation CR |

## Resource Dependencies (Tilt)

```text
victoriametrics ──┐
victorialogs  ────┤
victoriatraces ───┼── grafana-operator ── grafana (auto when any flag)
                  │
victoriametrics ──── vmagent (prometheus_metrics requires metrics)
victorialogs  ──── vector (logs deploys both)

parca-server ── parca-agent (standalone -- no backend dependency)

otel-operator ── instrumentation-cr (otel flag)
```

## State Transitions

### Component Lifecycle (managed by Tilt)

```text
[Not Deployed] ──enable_observability()──> [Deploying] ──health check pass──> [Ready]
                                               │
                                          health check fail
                                               │
                                               v
                                           [Unhealthy] ──Tilt auto-retry──> [Deploying]
```

### Export Lifecycle

```text
[Idle] ──trigger (button/CLI)──> [Exporting]
                                     │
                        ┌────────────┼────────────┐
                        v            v            v
                  [Export Metrics] [Export Traces] [Export Profiles]
                        │            │            │
                        v            v            v
                  [Write DuckDB]  [Write DuckDB] [Write .pb.gz]
                        │            │            │
                        └────────────┼────────────┘
                                     v
                              [Export Complete]
                              (partial OK if some backends unreachable)
```
