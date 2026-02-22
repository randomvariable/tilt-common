# tilt-common

Reusable [Tilt](https://tilt.dev/) extensions and utilities for local Kubernetes development.

## Extensions

### [observability](extensions/observability/)

Deploy a complete local observability stack with a single function call. Deploys into a dedicated `observability` namespace.

**Components:**

| Component | Default | Description |
|---|---|---|
| VictoriaMetrics | on | Metrics storage and query engine (Prometheus-compatible) |
| VictoriaLogs | on | Log storage with LogsQL query engine |
| VictoriaTraces | on | Distributed tracing (Jaeger-compatible query API) |
| Vector | on | Container log collector (auto-deployed with logs) |
| Grafana Operator | on | Grafana lifecycle management (auto-deployed with any backend) |
| VMAgent | off | Prometheus scraping via annotation-based service discovery |
| OTel Operator | off | Webhook-based OTLP env var injection into annotated pods |
| Parca | off | Continuous profiling server |
| Parca Agent | off | eBPF profiling agent (requires real Linux kernel, not kind) |

**Quick start:**

```python
# Tiltfile
load('ext://tilt-common/observability', 'enable_observability')

# Full stack with defaults (metrics, logs, traces, Grafana)
enable_observability()
```

**Custom configuration:**

```python
enable_observability({
    'namespace': 'observability',   # Dedicated namespace with PSA privileged labels
    'metrics': True,                # VictoriaMetrics
    'logs': True,                   # VictoriaLogs + Vector
    'traces': True,                 # VictoriaTraces
    'prometheus_metrics': True,     # VMAgent Prometheus scraping
    'otel': True,                   # OTel Operator webhook injection
    'profiling': True,              # Parca profiling server
    'parca_agent': False,           # eBPF agent (disable on kind — no real kernel)
    'ports': {
        'metrics': 8428,
        'logs': 9428,
        'traces': 10428,
        'grafana': 3000,
        'parca': 7070,
    },
})
```

**Get OTLP env vars for application containers:**

```python
load('ext://tilt-common/observability', 'otel_env')

k8s_resource('my-app', env=otel_env(service_name='my-app'))
```

**Trigger telemetry export to DuckDB:**

```python
load('ext://tilt-common/observability', 'export_telemetry')

export_telemetry()  # Registers a manual-trigger "observability-export" Tilt resource
```

**Ports forwarded to localhost by default:**

| Service | Port | URL |
|---|---|---|
| VictoriaMetrics | 8428 | `http://localhost:8428/vmui` |
| VictoriaLogs | 9428 | `http://localhost:9428/select/vmui` |
| VictoriaTraces | 10428 | `http://localhost:10428/` |
| Grafana | 3000 | `http://localhost:3000` (admin/admin) |
| Parca | 7070 | `http://localhost:7070/` |

**In-cluster OTLP endpoints (printed to Tilt log on startup):**

```
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://victoriametrics.observability.svc.cluster.local:8428/opentelemetry/api/v1/push
OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf

OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://victorialogs.observability.svc.cluster.local:9428/insert/opentelemetry/v1/logs
OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf

OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://victoriatraces.observability.svc.cluster.local:4317
OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc
```

**VMAgent Prometheus scraping** (when `prometheus_metrics: True`): annotate your Service with:

```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "8080"
  prometheus.io/path: "/metrics"   # optional, default /metrics
  prometheus.io/scheme: "http"     # optional, default http
```

**Environment variable overrides:**

| Variable | Description |
|---|---|
| `TILT_OBSERVABILITY_NAMESPACE` | Override namespace |
| `TILT_OBSERVABILITY_EXPORT_DIR` | Override export directory |
| `TILT_OBSERVABILITY_PORT_METRICS` | Override metrics port |
| `TILT_OBSERVABILITY_PORT_LOGS` | Override logs port |
| `TILT_OBSERVABILITY_PORT_TRACES` | Override traces port |
| `TILT_OBSERVABILITY_PORT_GRAFANA` | Override Grafana port |
| `TILT_OBSERVABILITY_PORT_PARCA` | Override Parca port |
| `TILT_OBSERVABILITY_CONTAINERD_SOCKET` | Containerd socket path for Parca Agent (default: `/run/containerd/containerd.sock`; k3s: `/run/k3s/containerd/containerd.sock`) |

## CLI Tools

### observability-export

Exports telemetry data from running backends into a DuckDB database for offline analysis (e.g. Jupyter notebooks).

```bash
# Build (requires CGO for DuckDB)
go run mage.go build:exportCLI

# Export from port-forwarded backends
./bin/observability-export \
    --output-dir ./observability-export \
    --metrics-url http://localhost:8428 \
    --logs-url http://localhost:9428 \
    --traces-url http://localhost:10428 \
    --parca-url http://localhost:7070
```

Flags: `--skip-metrics`, `--skip-logs`, `--skip-traces`, `--skip-profiles`

Exit codes: `0` = all succeeded, `1` = partial (some backends unreachable), `2` = all failed.

Output DuckDB tables: `metrics`, `logs`, `spans`, `profiles` (pprof).

## Prerequisites

- [Tilt](https://docs.tilt.dev/install.html) v0.30+
- [kubectl](https://kubernetes.io/docs/tasks/tools/) configured for a local cluster
- [Helm](https://helm.sh/docs/intro/install/) v3.x (for Grafana Operator and OTel Operator charts)
- A local Kubernetes cluster — [kind](https://kind.sigs.k8s.io/) or [k3d](https://k3d.io/) recommended
- CGO toolchain (only required for building the export CLI)

## Project Structure

```
tilt-common/
├── extensions/
│   └── observability/            # Observability stack Tilt extension
│       ├── extension.star        # Public API: enable_observability, otel_env, export_telemetry
│       └── assets/               # Kubernetes YAML templates
│           ├── namespace.yaml    # Namespace with PSA privileged labels
│           ├── metrics.yaml      # VictoriaMetrics
│           ├── logs.yaml         # VictoriaLogs
│           ├── traces.yaml       # VictoriaTraces
│           ├── vector.yaml       # Vector DaemonSet + config
│           ├── vmagent.yaml      # VMAgent + scrape config
│           ├── parca.yaml        # Parca server (SA, RBAC, Deployment, Service)
│           └── parca-agent.yaml  # Parca eBPF agent DaemonSet
├── cmd/
│   └── observability-export/     # CLI: export telemetry to DuckDB
├── tests/
│   └── observability/
│       ├── extension_test.go     # Unit tests (golden files, smoke, config validation)
│       ├── e2e_ginkgo_test.go    # Full E2E suite (requires cluster)
│       └── golden/               # Golden file snapshots of extension output
└── magefile.go                   # Mage build targets
```

## Development

### Commands

```bash
go run mage.go -l                     # List all targets
go run mage.go tools:install          # Install tools (golangci-lint, helm, tilt, kubectl, k3d, buildifier)
go run mage.go lint:all               # Run all linters (Go + Starlark)
go run mage.go build:all              # Build all Go packages
go run mage.go build:exportCLI        # Build observability-export CLI (requires CGO)
go run mage.go test:unit              # Run Go unit tests with race detector
go run mage.go e2e:observability      # Run extension unit/golden tests (no cluster needed)
go run mage.go e2e:observabilityFull  # Full E2E suite (creates k3d cluster, requires CGO)
go run mage.go e2e:all                # Run all E2E tests
```

### Updating golden files

After modifying the extension, regenerate the golden file snapshots:

```bash
UPDATE_GOLDEN=true go test ./tests/observability/ -run TestGoldenFiles
```

### Registry mirrors (avoid Docker Hub rate limiting)

```bash
export DOCKER_MIRROR=your-mirror.example.com/docker
export GHCR_MIRROR=your-mirror.example.com/github

go run mage.go e2e:observabilityFull
```

The kind cluster configures containerd to try the mirror first, then fall back to the upstream registry. TLS verification is skipped for mirrors (suitable for self-signed certificates).

Supported variables:
- `DOCKER_MIRROR` — Docker Hub (registry-1.docker.io)
- `GHCR_MIRROR` — GitHub Container Registry (ghcr.io)

### CI

GitHub Actions runs build, lint (Go + Starlark), and unit tests (with race detector) on every push and PR to `main`.

## License

Copyright 2026 Naadir Jeewa

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
