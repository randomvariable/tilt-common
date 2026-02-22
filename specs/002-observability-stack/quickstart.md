# Quickstart: Observability Stack Extension

**Feature**: 002-observability-stack | **Date**: 2026-02-14

## Prerequisites

- [Tilt](https://docs.tilt.dev/install.html) installed
- A running local Kubernetes cluster (k3d, kind, or minikube)
- `kubectl` configured to talk to the cluster

## 1. Basic Usage (2 lines)

Add to your `Tiltfile`:

```python
load('ext://tilt-common/observability', 'enable_observability')
enable_observability()
```

Run `tilt up`. This deploys:
- **VictoriaMetrics** (metrics) at `localhost:8428`
- **VictoriaLogs** (logs) at `localhost:9428`
- **VictoriaTraces** (traces) at `localhost:10428`
- **Grafana** (dashboards) at `localhost:3000` (admin/admin)

All Grafana datasources are pre-configured.

## 2. Instrument Your Application

```python
load('ext://tilt-common/observability', 'enable_observability', 'otel_env')

enable_observability()

```

Inject these environment variables into your application's container spec. Your application's OTel SDK will automatically send metrics, logs, and traces to the stack.

## 3. Enable Prometheus Scraping

For services that expose `/metrics` but don't use OTel:

```python
enable_observability({
    'components': {'vmagent': True},
    'scrape_targets': ['my-legacy-app:8080'],
})
```

Or annotate your Service/Pod:

```yaml
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
```

## 4. Enable Continuous Profiling

```python
enable_observability({
    'components': {'profiling': True},
})
```

Parca UI available at `localhost:7070`.

## 5. Custom Grafana Dashboards

```python
enable_observability({
    'dashboard_paths': [
        './dashboards/api-overview.json',
        './dashboards/database-metrics.json',
    ],
})
```

Dashboards appear in Grafana on startup.

## 6. Custom Ports (Avoid Conflicts)

```python
enable_observability({
    'ports': {
        'grafana': 3001,  # Your frontend already uses 3000
        'metrics': 9090,  # Custom port
    },
})
```

## 7. Export Telemetry Before Teardown

```python
load('ext://tilt-common/observability', 'enable_observability', 'export_telemetry')

enable_observability()
export_telemetry()  # Adds 'Export Telemetry' button to Tilt dashboard
```

Click the button in Tilt dashboard or run the export CLI:

```bash
go run ./cmd/observability-export/ --output-dir ./observability-export
```

Exported files:
- `observability-export/telemetry-<timestamp>.duckdb` -- metrics + traces (timestamped)
- `observability-export/profiles/*.pb.gz` -- pprof profiles (if profiling enabled)

## 8. Analyze Exported Data

**Jupyter notebook** (see `examples/observability/notebooks/`):

```python
import duckdb

db = duckdb.connect('observability-export/telemetry.duckdb')

# Query metrics
db.sql("""
    SELECT metric_name, avg(value) as avg_val
    FROM metrics
    GROUP BY metric_name
    ORDER BY avg_val DESC
    LIMIT 10
""").show()

# Query traces
db.sql("""
    SELECT service_name, operation,
           approx_quantile(duration_us, 0.95) / 1000.0 as p95_ms
    FROM spans
    GROUP BY service_name, operation
    ORDER BY p95_ms DESC
""").show()
```

**go tool pprof**:

```bash
go tool pprof observability-export/profiles/my-service-cpu-2026-02-14.pb.gz
```

## Configuration Reference

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `namespace` | string | `"default"` | K8s namespace for all resources |
| `components.metrics` | bool | `True` | Deploy VictoriaMetrics |
| `components.logs` | bool | `True` | Deploy VictoriaLogs |
| `components.traces` | bool | `True` | Deploy VictoriaTraces |
| `components.grafana` | bool | `True` | Deploy Grafana |
| `components.vmagent` | bool | `False` | Deploy VMAgent (requires metrics) |
| `components.profiling` | bool | `False` | Deploy Parca (standalone capable) |
| `ports.metrics` | int | `8428` | VictoriaMetrics port-forward |
| `ports.logs` | int | `9428` | VictoriaLogs port-forward |
| `ports.traces` | int | `10428` | VictoriaTraces port-forward |
| `ports.grafana` | int | `3000` | Grafana port-forward |
| `ports.parca` | int | `7070` | Parca port-forward |
| `images.*` | string | latest tags | Container image overrides |
| `scrape_targets` | list | `[]` | Static VMAgent scrape targets |
| `dashboard_paths` | list | `[]` | Grafana dashboard JSON files |
| `export_dir` | string | `"./observability-export"` | Export output directory |
