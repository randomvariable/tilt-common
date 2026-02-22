# Research: Observability Stack Extension

**Feature**: 002-observability-stack | **Date**: 2026-02-14 | **Spec**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md)

## Decision 1: Export CLI Language and DuckDB Library

**Decision**: Go with `github.com/duckdb/duckdb-go` v2.5.5+ (CGO required, bundling DuckDB v1.4.4)

**Rationale**:
- Project toolchain is Go + Starlark; Go keeps the language count minimal.
- `duckdb-go` is the canonical library (transferred from `marcboeker/go-duckdb`, archived Oct 2025).
- Default build statically bundles DuckDB -- no runtime dependency; single binary output.
- Appender API provides 10-100x faster bulk inserts vs row-by-row INSERT, critical for telemetry export.
- `database/sql` conformance means standard Go database patterns.
- Supports `MAP(VARCHAR, VARCHAR)` via `duckdb.Map` for dynamic label/attribute storage.
- CGO is manageable: `golang:1.23` CI image includes GCC. Cross-compile with `CC=x86_64-linux-gnu-gcc CGO_ENABLED=1`.

**Alternatives Considered**:

| Alternative | Rejected Because |
|-------------|-----------------|
| Python CLI (`click`/`typer` + `duckdb`) | Introduces runtime dependency (Python 3.9+), can't produce single binary, fragments toolchain |
| `scottlepp/go-duck` (no-CGO Go) | Wraps DuckDB CLI binary; requires separate CLI install; no `database/sql` interface; ~20 stars |
| Pure Go Parquet (`parquet-go/parquet-go`) | Loses single-file DuckDB database format; loses Jupyter SQL magic integration; loses ability to query from Go |

**CGO Build Notes**:
- Default: statically links bundled DuckDB (no extra setup)
- `-tags=duckdb_use_lib`: dynamic linking against system `libduckdb.so`
- `-tags=duckdb_arrow`: opt-in Apache Arrow support
- Platforms: Linux amd64/arm64, macOS amd64/arm64, Windows amd64

## Decision 2: DuckDB Schema Design

**Decision**: Two tables -- `metrics` and `spans` -- using `MAP(VARCHAR, VARCHAR)` for dynamic labels/attributes.

**Rationale**:
- MAP type natively stores Prometheus labels and OTel attributes without schema changes per metric.
- TIMESTAMP columns enable time-range queries; BIGINT for microsecond durations avoids INTERVAL complexity.
- Indexes on timestamp, metric_name, trace_id, and duration_us cover primary query patterns.
- Schema aligns with DuckDB best practices: most restrictive types, DOUBLE for numeric values.

**Metrics Table**:
```sql
CREATE TABLE metrics (
    timestamp       TIMESTAMP NOT NULL,
    metric_name     VARCHAR NOT NULL,
    labels          MAP(VARCHAR, VARCHAR),
    value           DOUBLE NOT NULL,
    export_time     TIMESTAMP
);
```

**Spans Table**:
```sql
CREATE TABLE spans (
    trace_id        VARCHAR NOT NULL,
    span_id         VARCHAR NOT NULL,
    parent_span_id  VARCHAR,
    operation       VARCHAR NOT NULL,
    service_name    VARCHAR NOT NULL,
    span_kind       VARCHAR,
    start_time      TIMESTAMP NOT NULL,
    end_time        TIMESTAMP NOT NULL,
    duration_us     BIGINT NOT NULL,
    status_code     VARCHAR,
    status_message  VARCHAR,
    attributes      MAP(VARCHAR, VARCHAR),
    resource_attrs  MAP(VARCHAR, VARCHAR),
    export_time     TIMESTAMP
);
```

## Decision 3: Python for Jupyter Only

**Decision**: Python `duckdb` v1.4.4 for example Jupyter notebooks only; no Python in the CLI tool or extension.

**Rationale**:
- DuckDB Python client provides zero-copy integration with pandas/polars DataFrames.
- SQL magic (`%load_ext duckdb.jupyter`, `%%sql`) enables inline SQL cells in notebooks.
- Python is only used in `examples/observability/notebooks/` -- not a build or runtime dependency.
- Requires Python 3.9+, `pip install duckdb`.

## Decision 4: VictoriaMetrics Export API

**Decision**: Use VictoriaMetrics `/api/v1/export` endpoint (JSON lines format) for metrics export.

**Rationale**:
- `/api/v1/export` returns complete time series (all samples, not point-in-time) as JSON lines.
- Each line is a JSON object with `metric` (name + labels), `values` (float64 array), and `timestamps` (unix ms array).
- Supports `match[]` filter parameter and `start`/`end` time range selectors.
- JSON lines format is trivially parseable in Go -- one `json.Decoder` per line.
- Alternative `/api/v1/export/csv` exists but JSON is easier to map to DuckDB MAP types.

**Export URL pattern**: `http://victoriametrics:8428/api/v1/export?match[]={__name__!=""}&start=0`

## Decision 5: VictoriaTraces Export API

**Decision**: Use VictoriaTraces Jaeger-compatible HTTP API for trace export.

**Rationale**:
- VictoriaTraces exposes Jaeger HTTP API at port 10428.
- `/api/traces?service=<name>` returns traces in Jaeger JSON format.
- `/api/traces/<traceID>` retrieves individual traces.
- `/api/services` lists all services for enumeration.
- Jaeger JSON format maps directly to the spans DuckDB schema.

**Export flow**: List services -> for each service, fetch traces -> flatten spans -> bulk write to DuckDB via Appender.

## Decision 6: Parca Profile Export

**Decision**: Use Parca HTTP API for pprof profile download.

**Rationale**:
- Parca server exposes HTTP API at port 7070.
- `/api/v1/profiles/query` with `report_type=pprof` returns standard pprof format.
- Downloaded files are directly compatible with `go tool pprof`.
- Profiles exported as individual `.pb.gz` files to the export output directory (alongside the DuckDB file).

## Decision 7: VMAgent Configuration

**Decision**: VMAgent v1.135.0 with `kubernetes_sd_configs` for annotation-based service discovery.

**Rationale**:
- v1.135.0 is the latest stable community release (2026-01-30).
- `kubernetes_sd_configs` with `role: endpoints` discovers services annotated with `prometheus.io/scrape: "true"`.
- Standard annotations supported: `prometheus.io/scrape`, `/port`, `/path`, `/scheme`.
- RBAC required: ServiceAccount + ClusterRole (get/list/watch on nodes, pods, services, endpoints) + ClusterRoleBinding.
- Remote write to VictoriaMetrics via `-remoteWrite.url=http://victoriametrics:8428/api/v1/write`.
- Resource requirements for local dev: 50m-250m CPU, 64Mi-128Mi memory.
- User-provided static scrape targets appended to the same ConfigMap alongside kubernetes_sd_configs.

## Decision 8: Containerd Socket Auto-Detection

**Decision**: Ordered probing of known paths: k3s -> microk8s -> standard.

**Rationale**:
- Socket paths vary by runtime:
  - k3s/k3d/RKE2: `/run/k3s/containerd/containerd.sock`
  - microk8s: `/var/snap/microk8s/common/run/containerd.sock`
  - kind/minikube/standard: `/run/containerd/containerd.sock`
- k3s path must be checked first because k3s nodes also have a standard containerd socket that is not the k3s-managed one.
- Detection runs inside the cluster node (not host) via a short-lived Job or `kubectl exec`.
- Extension fails with a clear error listing checked paths if no socket is found.

**Alternatives Considered**:

| Alternative | Rejected Because |
|-------------|-----------------|
| Hardcoded k3s path only | Breaks on kind, minikube, standard containerd |
| User-provided path only | Poor developer experience; most users don't know the path |
| Environment variable detection | Not reliable -- socket path isn't always in env |

## Decision 9: Grafana Dashboard Provisioning

**Decision**: ~~Projected volumes pattern~~ **SUPERSEDED by Decision 14** -- Grafana Operator with `GrafanaDashboard` CRD (`grafana.integreatly.org/v1beta1`). See Decision 14 for current approach.

**Original Decision** (superseded): Projected volumes pattern -- merge built-in and custom dashboard ConfigMaps into a single mount point. This approach was replaced in the 2026-02-15 session with the Grafana Operator CRD-based approach for Kubernetes-native dashboard management.

## Decision 10: Tilt Extension Testing Strategy

**Decision**: All testing in Go. Three tiers: Go unit tests invoking `tilt alpha tiltfile-result` (Tier 1/2), Go E2E tests invoking `tilt ci` (Tier 3). No Bash test scripts.

**Rationale**:
- All tests written as Go table-driven tests with gomega assertions, consistent with the rest of the project.
- Tilt has no built-in Starlark unit test runner (open issue #3815).
- `tilt alpha tiltfile-result` executes a Tiltfile and outputs JSON without deploying. Exit code 5 on `fail()`. Invoked from Go via `exec.Command`.
- Tier 1 (smoke tests): Go test verifies extension loads without errors by checking `tilt alpha tiltfile-result` exit code.
- Tier 2 (golden file tests): Go test extracts YAML from `tilt alpha tiltfile-result` JSON output and compares against golden files checked into the repository.
- Tier 3 (E2E): Go test invokes `tilt ci` with a test Tiltfile, then runs integration assertions.
- Tier 1/2 run without a cluster (CI-friendly). Tier 3 requires a cluster (`//go:build e2e`).

**Golden Files Explained**: A golden file is a known-good reference output checked into version control. The test generates actual output (e.g., rendered Kubernetes YAML), then compares it byte-for-byte against the golden file. When the extension changes intentionally, golden files are updated with `go test -update`. This catches unintended output changes and serves as documentation of expected behavior. Golden files live in `tests/<extension>/golden/` directories.

**Alternatives Considered**:

| Alternative | Rejected Because |
|-------------|-----------------|
| Bash test scripts | Inconsistent with project testing approach; harder to maintain; less structured assertions |
| BATS framework | Adds a dependency; Go table-driven tests provide equivalent structure |
| `tilt dump` | Diagnostic tool for running engine state, not for evaluating Tiltfiles |

## Decision 11: Component Image Versions

**Decision**: Default to latest tags with documented version baselines.

**Rationale**:
- Per spec clarification: "Default to latest tags, but allow image overrides via config."
- Baseline versions from memex source and research:
  - VictoriaMetrics: `victoriametrics/victoria-metrics:latest` (baseline v1.135.0)
  - VictoriaLogs: `victoriametrics/victoria-logs:latest` (baseline v1.24.0)
  - VictoriaTraces: `victoriametrics/victoria-traces:latest` (baseline v0.7.1)
  - Grafana: `grafana/grafana:latest` (baseline v11.5.2)
  - Parca Server: `ghcr.io/parca-dev/parca:latest` (baseline v0.25.0)
  - Parca Agent: `ghcr.io/parca-dev/parca-agent:latest` (baseline v0.45.1)
  - VMAgent: `victoriametrics/vmagent:latest` (baseline v1.135.0)
- All images overridable via `config["images"]` dict.

## Decision 12: OTel Environment Variables

**Decision**: Return standard OTel SDK environment variables pointing to in-cluster service endpoints.

**Rationale**:
- Based on memex Tiltfile pattern (lines 140-169).
- Environment variables follow OpenTelemetry SDK specification:
  - `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`: `http://victoriametrics.<ns>.svc.cluster.local:8428/opentelemetry/api/v1/push`
  - `OTEL_EXPORTER_OTLP_METRICS_PROTOCOL`: `http/protobuf`
  - `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`: `http://victorialogs.<ns>.svc.cluster.local:9428/insert/opentelemetry/v1/logs`
  - `OTEL_EXPORTER_OTLP_LOGS_PROTOCOL`: `http/protobuf`
  - `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`: `http://victoriatraces.<ns>.svc.cluster.local:4317`
  - `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`: `grpc`
- Only variables for enabled backends are returned.
- Optional `OTEL_SERVICE_NAME` from user parameter.

## Decision 13: OTel Operator Deployment via Helm Template

**Decision**: Use `helm template` to render the OpenTelemetry Operator Helm chart at Tilt startup, with self-signed certificate generation.

**Rationale**:
- The OTel Operator Helm chart (chart v0.105.1, app v0.144.0) handles CRDs, RBAC, Deployment, Service, and MutatingWebhookConfiguration correctly.
- `helm template` with `--repo` flag pulls and renders the chart without requiring pre-added Helm repos.
- Self-signed certs via `admissionWebhooks.certManager.enabled=false` + `admissionWebhooks.autoGenerateCert.enabled=true` eliminates the cert-manager dependency.
- The rendered YAML is passed to `k8s_yaml(blob(...))` like all other components.
- Helm CLI (v3.x) is an acceptable prerequisite for local development tooling.

**Helm Values Used**:
- `admissionWebhooks.certManager.enabled=false`: No cert-manager dependency
- `admissionWebhooks.autoGenerateCert.enabled=true`: Self-signed TLS certs
- `manager.image.repository` + `manager.image.tag`: Configurable operator image
- `manager.resources.requests`: 50m CPU, 64Mi memory (lightweight for local dev)
- `manager.resources.limits`: 250m CPU, 128Mi memory
- `admissionWebhooks.pods.failurePolicy=Ignore`: Safe for local dev

**Alternatives Considered**:

| Alternative | Rejected Because |
|-------------|-----------------|
| Bundle static YAML in assets/ | CRDs are 3000+ lines; manual maintenance burden; version drift risk |
| Fetch CRDs from GitHub at runtime | Breaks offline use; `local()` network dependency; inconsistent with existing pattern |
| Deploy cert-manager + OTel Operator | cert-manager is heavy (3 pods, CRDs); excessive for local dev |
| Custom lightweight webhook | Maintenance burden; reinventing what the OTel Operator already does |

**Chart Reference**: `opentelemetry-operator` from `https://open-telemetry.github.io/opentelemetry-helm-charts`

## Decision 14: Grafana Operator for Dashboard and Datasource Management

**Decision**: Deploy the Grafana Operator (`grafana.integreatly.org`) instead of raw Grafana with ConfigMap-based provisioning. Dashboards via `GrafanaDashboard` CRD only, datasources via `GrafanaDatasource` CRD.

**Rationale**:
- `GrafanaDashboard` CRD provides declarative, Kubernetes-native dashboard management -- consistent with production patterns.
- The Grafana Operator reconciles CRs to manage Grafana instances, datasources, and dashboards automatically.
- Users create `GrafanaDashboard` CRs (e.g., in their own Helm charts or Tiltfiles) and the operator provisions them.
- Eliminates ConfigMap-based provisioning (projected volumes, provider config files, reload intervals).
- The Grafana Operator is automatically deployed whenever any observability flag is enabled.

**Helm Chart**: `grafana-operator` from `oci://ghcr.io/grafana/helm-charts/grafana-operator` (chart version TBD).

**Alternatives Considered**:

| Alternative | Rejected Because |
|-------------|-----------------|
| Raw Grafana + ConfigMap provisioning | Not Kubernetes-native; requires custom projected volume wiring; no CRD-based management |
| Grafana with sidecar container for dashboards | Over-engineered; still not CRD-based |

## Decision 15: Vector for Log Collection

**Decision**: Deploy Vector alongside VictoriaLogs when the `logs` flag is enabled. Vector handles log collection, transformation, and forwarding to VictoriaLogs.

**Rationale**:
- Vector is a high-performance, vendor-neutral log pipeline that supports multiple inputs (Kubernetes logs, OTLP) and outputs (VictoriaLogs HTTP API).
- Decouples log collection from storage -- Vector collects from nodes, VictoriaLogs stores.
- Supports structured logging, parsing, enrichment before forwarding.
- Kubernetes-native log collection via the `kubernetes_logs` source.
- Already widely used in the VictoriaMetrics ecosystem.

**Alternatives Considered**:

| Alternative | Rejected Because |
|-------------|-----------------|
| Direct OTLP ingestion only | Not all workloads emit structured OTLP logs; miss container stdout/stderr |
| Fluent Bit | Higher resource usage; less performant; more complex configuration |
| Promtail | Loki-specific; not a good fit for VictoriaLogs |

## Decision 16: Prometheus Annotations + Operator CRDs for Scrape Configuration

**Decision**: When `prometheus_metrics` is enabled, support BOTH Prometheus annotation-based discovery (`prometheus.io/scrape: "true"`) AND Prometheus Operator CRDs (ServiceMonitor, PodMonitor, ScrapeConfig, Probe). Install CRDs and configure VMAgent to discover both annotation-based targets and CRs.

**Rationale**:
- Annotation-based discovery (`prometheus.io/scrape`, `/port`, `/path`, `/scheme`) provides zero-config scraping -- annotate a service and it's scraped automatically.
- Prometheus Operator CRDs are the de-facto standard for structured scrape target declaration in Kubernetes.
- VMAgent natively supports both: `kubernetes_sd_configs` with relabeling for annotations, and CRD discovery flags for Prometheus Operator CRs.
- Both approaches are complementary: annotations for simple cases, CRDs for complex/validated configuration.
- Configuration portability: same ServiceMonitor/PodMonitor CRs and annotations work in local dev and production.
- ServiceMonitor: targets Services. PodMonitor: targets Pods directly. ScrapeConfig: static targets. Probe: blackbox monitoring.

**CRD Installation**: CRDs are installed from the `prometheus-operator/prometheus-operator` GitHub releases. Only the CRD definitions are needed, not the full Prometheus Operator deployment.

**Annotation-based Discovery**: VMAgent uses `kubernetes_sd_configs` with `role: endpoints` and `role: pod` to discover services/pods annotated with `prometheus.io/scrape: "true"`. Additional annotations supported: `prometheus.io/port`, `prometheus.io/path`, `prometheus.io/scheme`.

**Alternatives Considered**:

| Alternative | Rejected Because |
|-------------|-----------------|
| CRDs only (no annotations) | Annotations are simpler for basic cases; many existing workloads already use them |
| Annotations only (no CRDs) | Less structured; no validation; limited configuration options for complex cases |
| Full Prometheus Operator deployment | Too heavy for local dev; only need CRDs + VMAgent |
| Custom scrape config via extension config dict | Not portable to production; reinvents existing patterns |

## Decision 17: Simplified Component Model

**Decision**: Five boolean flags (`metrics`, `logs`, `traces`, `prometheus_metrics`, `otel`) plus `profiling`. Grafana Operator auto-deployed when any flag is enabled.

**Rationale**:
- Previous model had `components.metrics/logs/traces/grafana/vmagent/otel_operator/profiling` -- too many flags with complex interdependencies.
- New model: each flag maps to a clear set of deployments. No ambiguity about what gets deployed.
- Grafana is always wanted when observability is enabled -- making it implicit simplifies the API.
- `otel` flag always deploys the OTel Operator (no conditional logic based on other flags).
- `prometheus_metrics` requires `metrics` (VMAgent needs VictoriaMetrics as remote write target).

**Flag → Deployment Mapping**:

| Flag | Deploys |
|------|---------|
| `metrics` | VictoriaMetrics |
| `logs` | VictoriaLogs + Vector |
| `traces` | VictoriaTraces |
| `prometheus_metrics` | VMAgent + Prometheus Operator CRDs (requires `metrics`) |
| `otel` | OTel Operator + Instrumentation CR |
| `profiling` | Parca Server + Parca Agent DaemonSet |
| (any of above) | Grafana Operator + Grafana instance + GrafanaDatasource CRs |

---

## Migration Decisions: k3d → kindgpu (2026-02-15)

The following decisions cover the migration from k3d-based cluster lifecycle to the kindgpu Go library.

### Decision 18: Cluster Lifecycle Management

**Decision**: Replace all `exec.Command("k3d", ...)` calls with kindgpu Go library calls (`kindgpu.CreateClusterFromConfig`, `kindgpu.DeleteCluster`).

**Rationale**: kindgpu already implements full cluster lifecycle as a Go library. Using it directly eliminates the k3d CLI dependency and aligns with the contract that cluster bring-up is done via Go code.

**Alternatives considered**:
| Alternative | Rejected Because |
|-------------|-----------------|
| Wrapping kind CLI directly | kindgpu already provides the abstraction with GPU support baked in |
| Keeping k3d alongside kind | Contradicts the migration goal |

### Decision 19: Container Image Transfer (No Local Registry)

**Decision**: Remove `default_registry('localhost:5005')` from E2E Tiltfile. Rely on Tilt's auto-detection of kind clusters, which uses `kind load docker-image` for image transfer.

**Rationale**: k3d uses `--registry-create` for a local registry. kind does not have an equivalent. However, Tilt natively detects kind clusters and automatically uses `kind load docker-image` instead of pushing to a registry. This is simpler and eliminates the registry as a moving part.

**Alternatives considered**:
| Alternative | Rejected Because |
|-------------|-----------------|
| Running a separate registry container | Adds complexity; Tilt handles kind natively |
| Using `kind load` manually in test code | Tilt already does this automatically |

### Decision 20: Registry Mirror Approach

**Decision**: Use kindgpu's built-in mirror support (baked into the node image via Dockerfile) instead of k3d's bind-mount volume approach.

**Rationale**: kindgpu generates `hosts.toml` files and bakes them into the node image during the Docker build step. This is more reliable than k3d's approach of creating a temp directory and bind-mounting it into the cluster nodes.

**Alternatives considered**:
| Alternative | Rejected Because |
|-------------|-----------------|
| Mounting mirror config at runtime via `extraMounts` | kindgpu already handles this at image build time |

### Decision 21: No Starlark Extension for kind-gpu

**Decision**: kind-gpu is a pure Go library with no Starlark `extension.star` file. The `tests/k3d-gpu/extension_test.go` (which tested the deleted Starlark extension) will be deleted.

**Rationale**: The user's contract explicitly states "Everything to do with cluster bring up is independent of tilt and done via the go code in kindgpu." kindgpu unit tests already exist in `extensions/kind-gpu/` (12 tests, all passing).

**Alternatives considered**:
| Alternative | Rejected Because |
|-------------|-----------------|
| Creating a thin Starlark wrapper | Contradicts the contract |

### Decision 22: Kubernetes Context Naming

**Decision**: Update all context references from `k3d-tilt-common-e2e` to `kind-tilt-common-e2e`.

**Rationale**: kind uses the `kind-<name>` prefix for kubeconfig contexts, while k3d uses `k3d-<name>`. This affects the Tiltfile `allow_k8s_contexts()` call and any kubectl commands.

**Alternatives considered**:
| Alternative | Rejected Because |
|-------------|-----------------|
| Using a custom context name | kind enforces the `kind-` prefix |

### Decision 23: E2E Test Structure

**Decision**: Keep the existing `//go:build e2e` tagged test structure in `tests/observability/e2e_test.go`. Replace only the cluster lifecycle functions (create, delete, mirror setup) with kindgpu equivalents.

**Rationale**: The E2E test structure (TestMain setup/teardown, tilt ci invocation, integration assertions) is sound. Only the cluster management layer needs to change.

**Alternatives considered**:
| Alternative | Rejected Because |
|-------------|-----------------|
| Rewriting E2E tests from scratch | Test logic is independent of the cluster provider |
