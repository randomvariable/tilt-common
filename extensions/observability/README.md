# Observability Stack Extension

Deploy a complete local observability stack (metrics, logs, traces, profiling) with a single function call.

## Quick Start

```python
# Tiltfile
load('ext://tilt-common/observability', 'enable_observability')
enable_observability()
```

Run `tilt up`. Open [localhost:3000](http://localhost:3000) for Grafana (admin/admin), with all datasources pre-configured.

## Prerequisites

- **Tilt** v0.33+
- **kubectl** configured with a local Kubernetes cluster (k3d, kind, minikube, or Docker Desktop)
- **Go 1.23+** with CGO enabled (only for the DuckDB export CLI)
- **Helm 3.x** (only when `otel_operator` is enabled)

## Components

| Component | Purpose | Default Port | Enabled by Default |
|-----------|---------|:------------:|:------------------:|
| VictoriaMetrics | Metrics backend (Prometheus-compatible) | 8428 | Yes |
| VictoriaLogs | Structured log storage | 9428 | Yes |
| VictoriaTraces | Distributed traces (Jaeger-compatible, OTLP gRPC on 4317) | 10428 | Yes |
| Grafana | Visualization with auto-provisioned datasources | 3000 | Yes |
| VMAgent | Prometheus scraping agent (requires metrics) | -- | No |
| Parca | Continuous profiling (server + agent) | 7070 | No |
| OTel Operator | Webhook-based OTLP env var injection (requires Helm) | -- | No |

All resources appear under the `observability` label in the Tilt UI. Grafana waits for its backend datasources before starting. VMAgent depends on VictoriaMetrics. Parca runs standalone.

## Configuration Reference

All keys are optional. Defaults produce a working metrics + logs + traces + Grafana stack.

### Top-Level Keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `namespace` | string | `"default"` | Kubernetes namespace. Must be lowercase, RFC 1123 compliant. |
| `components` | dict | See below | Enable/disable individual components. |
| `ports` | dict | See below | Host port-forward overrides (1--65535). |
| `images` | dict | See below | Container image overrides. |
| `scrape_targets` | list[string] | `[]` | Static scrape targets for VMAgent (e.g., `["myapp:8080"]`). |
| `dashboard_paths` | list[string] | `[]` | Paths to Grafana dashboard JSON files to auto-provision. |
| `export_dir` | string | `"./observability-export"` | Output directory for telemetry exports. |

### Components

| Key | Type | Default | Notes |
|-----|------|---------|-------|
| `metrics` | bool | `True` | VictoriaMetrics. |
| `logs` | bool | `True` | VictoriaLogs. |
| `traces` | bool | `True` | VictoriaTraces. |
| `grafana` | bool | `True` | Grafana. Datasources auto-configured for enabled backends. |
| `vmagent` | bool | `False` | VMAgent. Requires `metrics=True`. |
| `profiling` | bool | `False` | Parca server + agent. Needs containerd socket on cluster nodes. |
| `otel_operator` | bool | `False` | OTel Operator webhook. Requires Helm CLI and at least one signal backend (metrics, logs, or traces). |

If every component is `False`, `enable_observability()` is a no-op.

### Ports

| Key | Type | Default |
|-----|------|---------|
| `metrics` | int | `8428` |
| `logs` | int | `9428` |
| `traces` | int | `10428` |
| `grafana` | int | `3000` |
| `parca` | int | `7070` |

### Images

| Key | Default |
|-----|---------|
| `metrics` | `victoriametrics/victoria-metrics:latest` |
| `logs` | `victoriametrics/victoria-logs:latest` |
| `traces` | `victoriametrics/victoria-traces:latest` |
| `grafana` | `grafana/grafana:latest` |
| `vmagent` | `victoriametrics/vmagent:latest` |
| `parca_server` | `ghcr.io/parca-dev/parca:latest` |
| `parca_agent` | `ghcr.io/parca-dev/parca-agent:latest` |
| `otel_operator` | `ghcr.io/open-telemetry/opentelemetry-operator/opentelemetry-operator:0.116.0` |

## Environment Variable Overrides

Environment variables take precedence over the config dict. Useful for CI or per-developer overrides without modifying the Tiltfile.

| Variable | Overrides |
|----------|-----------|
| `TILT_OBSERVABILITY_NAMESPACE` | `namespace` |
| `TILT_OBSERVABILITY_EXPORT_DIR` | `export_dir` |
| `TILT_OBSERVABILITY_PORT_METRICS` | `ports.metrics` |
| `TILT_OBSERVABILITY_PORT_LOGS` | `ports.logs` |
| `TILT_OBSERVABILITY_PORT_TRACES` | `ports.traces` |
| `TILT_OBSERVABILITY_PORT_GRAFANA` | `ports.grafana` |
| `TILT_OBSERVABILITY_PORT_PARCA` | `ports.parca` |
| `TILT_OBSERVABILITY_CONTAINERD_SOCKET` | Containerd socket path for Parca agent (bypasses auto-detection) |

## otel_env() Helper

Returns a list of `KEY=VALUE` strings to configure OpenTelemetry SDKs in your application containers. Only emits variables for enabled components.

```python
load('ext://tilt-common/observability', 'enable_observability', 'otel_env')

enable_observability()
env = otel_env(service_name='my-app')
# Returns:
# [
#   "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://victoriametrics.default.svc.cluster.local:8428/opentelemetry/api/v1/push",
#   "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf",
#   "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://victorialogs.default.svc.cluster.local:9428/insert/opentelemetry/v1/logs",
#   "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf",
#   "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://victoriatraces.default.svc.cluster.local:4317",
#   "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc",
#   "OTEL_SERVICE_NAME=my-app",
# ]
```

Pass the same `config` dict you gave to `enable_observability()` to keep namespace and component settings consistent:

```python
config = {"namespace": "monitoring"}
enable_observability(config)
env = otel_env(service_name='my-app', config=config)
```

### Alternative: Automatic Injection via OTel Operator

Instead of manually injecting `otel_env()`, enable the OTel Operator and annotate your pods:

```python
enable_observability({"components": {"otel_operator": True}})
```

```yaml
# In your pod spec
metadata:
  annotations:
    instrumentation.opentelemetry.io/inject-sdk: "true"
```

The operator's mutating webhook injects the same env vars that `otel_env()` returns. Requires Helm 3.x on PATH.

## export_telemetry()

Registers a manually-triggered Tilt resource (`observability-export`) that snapshots telemetry data into DuckDB files for offline analysis.

```python
load('ext://tilt-common/observability', 'enable_observability', 'export_telemetry')

enable_observability()
export_telemetry()
```

Click the **observability-export** button in the Tilt UI, or run directly:

```bash
go run ./cmd/observability-export/ --output-dir ./observability-export
```

Exported files:

- `observability-export/telemetry-<timestamp>.duckdb` -- metrics and traces
- `observability-export/profiles/*.pb.gz` -- pprof profiles (if profiling enabled)

Query exported data with DuckDB or the example Jupyter notebooks in `examples/observability/notebooks/`.

## Usage Examples

### Full Stack with Profiling and Scraping

```python
load('ext://tilt-common/observability', 'enable_observability')

enable_observability({
    "namespace": "observability",
    "components": {
        "profiling": True,
        "vmagent": True,
    },
    "scrape_targets": ["my-app.default.svc:8080"],
})
```

### Metrics Only (Minimal)

```python
enable_observability({
    "components": {
        "logs": False,
        "traces": False,
        "grafana": False,
    },
})
```

### Custom Grafana Dashboards

```python
enable_observability({
    "dashboard_paths": [
        "./dashboards/overview.json",
        "./dashboards/latency.json",
    ],
})
```

### Avoid Port Conflicts

```python
enable_observability({
    "ports": {
        "grafana": 3001,
        "metrics": 9090,
    },
})
```

## Troubleshooting

### Port Conflicts

If a default port is in use, override it via config or environment variable:

```python
enable_observability({"ports": {"grafana": 3001}})
```

```bash
export TILT_OBSERVABILITY_PORT_GRAFANA=3001
```

### VMAgent Fails to Start

VMAgent requires the metrics component. Enabling `vmagent: True` with `metrics: False` fails validation with a clear error. Enable metrics or disable vmagent.

### Parca Agent Cannot Access containerd Socket

The profiling component auto-detects the containerd socket by probing these paths in order:

1. `/run/k3s/containerd/containerd.sock` (k3s/k3d)
2. `/var/snap/microk8s/common/run/containerd.sock` (MicroK8s)
3. `/run/containerd/containerd.sock` (standard)

If none are found, the extension fails listing all checked paths. Override detection with:

```bash
export TILT_OBSERVABILITY_CONTAINERD_SOCKET=/path/to/containerd.sock
```

### Resource Limits

Default resource requests are minimal, sized for local development. If components are OOMKilled, pin specific image versions or adjust resource limits in the asset YAML templates under `extensions/observability/assets/`.

### OTel Operator Webhook Not Injecting

The operator webhook uses `failurePolicy: Ignore`, so pods start normally if the operator is not yet ready. Wait for the `otel-operator` resource to be healthy in the Tilt UI, then redeploy your pod. Verify the annotation is present:

```yaml
instrumentation.opentelemetry.io/inject-sdk: "true"
```

### OTel Operator Requires Helm

The `otel_operator` component renders manifests via `helm template`. Install Helm 3.x if missing:

```bash
brew install helm
# or see https://helm.sh/docs/intro/install/
```

### All Components Disabled

When every component is `False`, `enable_observability()` deploys nothing. This is intentional for conditional setups where observability is toggled by environment.

## Project Structure

```
extensions/observability/
├── extension.star              # Main extension (public API)
├── assets/
│   ├── metrics.yaml            # VictoriaMetrics deployment template
│   ├── logs.yaml               # VictoriaLogs deployment template
│   ├── traces.yaml             # VictoriaTraces deployment template
│   ├── grafana.yaml            # Grafana deployment template
│   ├── grafana-dashboards.yaml # Dashboard provisioning template
│   ├── vmagent.yaml            # VMAgent deployment template
│   ├── vmagent-config.yaml     # VMAgent scrape config template
│   └── parca.yaml              # Parca server + agent template
└── README.md

tests/observability/
├── extension_test.go           # Go tests (smoke, config validation, golden files)
├── golden/                     # Expected YAML output files
└── e2e/                        # End-to-end assets (Tiltfile, test pod)
```

## License

Apache License 2.0 -- See [LICENSE](../../LICENSE).
