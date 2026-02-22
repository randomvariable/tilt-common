# Feature Specification: Observability Stack Extension

**Feature Branch**: `002-observability-stack`
**Created**: 2026-02-14
**Status**: Draft
**Input**: User description: "porting the observability stack from ../memex's Tiltfile as an extension"

## Clarifications

### Session 2026-02-14

- Q: Should the extension support selective component enablement or always deploy all four core components? → A: Selective -- allow enabling/disabling individual components (metrics, logs, traces, grafana) via config.
- Q: Should image versions be pinned in manifests, configurable, or use latest tags? → A: Default to latest tags, but allow image overrides via config.
- Q: Should the Parca agent containerd socket path be hardcoded (k3s), configurable, or auto-detected? → A: Auto-detect by probing known paths at deploy time (kind, minikube, k3s, standard containerd expected).
- Q: Should custom Grafana dashboards, VMAgent, and Prometheus scraping be in or out of scope? → A: In scope -- custom Grafana dashboards via provisioning, and VMAgent for Prometheus-mode scraping, because not everything supports OTel yet.
- Q: What constitutes the "base observability stack" that profiling requires? → A: No backend required -- profiling can run standalone without any core backends enabled.
- Q: When the export DuckDB file already exists, should the export overwrite, append, or error? → A: Timestamped -- always create a new file with a timestamp suffix, preserving previous exports.
- Q: Should unknown/unrecognized configuration keys produce an error, warning, or be silently ignored? → A: Error -- fail() on any unrecognized key to catch typos immediately.
- Q: Can all components be disabled simultaneously, or must at least one be enabled? → A: Allow all disabled -- the function is a valid no-op without error when no components are enabled.

### Session 2026-02-15

- Q: How should component options be structured? → A: Simplified flags: `metrics` (VictoriaMetrics), `logs` (VMLogs + Vector), `traces` (VictoriaTraces), `prometheus_metrics` (VMAgent), `otel` (OTel Operator). If any flag is enabled, deploy Grafana Operator automatically.
- Q: How should OTel env vars be provided? → A: Non-service-specific. Print to Tilt logs and/or show in the Tilt UI. No per-service helper function needed -- the endpoints are always the same.
- Q: How should Prometheus scraping work? → A: Support both Prometheus annotations (`prometheus.io/scrape`, `/port`, `/path`, `/scheme`) for zero-config discovery AND all Prometheus Operator CRDs (ServiceMonitor, PodMonitor, ScrapeConfig, Probe) for structured configuration. This is P1.
- Q: How should Grafana dashboards be provisioned? → A: Only via `GrafanaDashboard` CRD (`grafana.integreatly.org/v1beta1`). No ConfigMap-based provisioning.
- Q: What priority is the OTel Operator? → A: P1 -- always deployed when `otel` flag is enabled.
- Q: How should testing be done? → A: All testing via Go (table-driven tests, gomega). No Bash test scripts.
- Q: What should the default values of the component flags be? → A: Core signal flags (`metrics`, `logs`, `traces`) default to `True` (full stack out of the box). Optional flags (`prometheus_metrics`, `otel`, `profiling`) default to `False`.
- Q: How should CRD installation handle pre-existing CRDs in the cluster? → A: Use server-side apply with force-conflicts to take ownership. The extension is authoritative in local dev clusters.
- Q: How should ephemeral storage growth be bounded for signal backends? → A: Use VictoriaMetrics/Logs/Traces built-in retention flags (e.g., `-retentionPeriod`) rather than emptyDir sizeLimit. Retention keeps data bounded at the application level.
- Q: Should cluster-scoped RBAC resources use a unique name prefix? → A: Yes, prefix with `tilt-observability-` (e.g., `tilt-observability-vmagent`) to avoid naming collisions and simplify cleanup.
- Q: What is the minimum supported Kubernetes version? → A: Kubernetes 1.34.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Enable Full Observability Stack (Priority: P1)

A developer using Tilt for local Kubernetes development wants to add a complete observability stack (metrics, logs, traces, visualization) to their project by loading a single extension and calling one function. They should not need to manage individual Kubernetes manifests or wire up component dependencies manually.

**Why this priority**: This is the core value proposition -- turning a project-specific, hardcoded observability setup into a reusable, one-call extension that any Tilt project can adopt.

**Independent Test**: Can be fully tested by loading the extension in a fresh Tiltfile, calling the main function with default settings, and verifying all four components (VictoriaMetrics, VictoriaLogs, VictoriaTraces, Grafana) are deployed and accessible via port-forwards.

**Acceptance Scenarios**:

1. **Given** a Tiltfile with no observability resources, **When** the developer loads the extension and calls the enable function with default configuration, **Then** VictoriaMetrics, VictoriaLogs, VictoriaTraces, and Grafana are deployed to the cluster with health checks passing and port-forwards accessible.
2. **Given** the observability stack is deployed with defaults, **When** the developer opens the Grafana UI, **Then** all three datasources (VictoriaMetrics, VictoriaLogs, VictoriaTraces) are pre-configured and ready to query.
3. **Given** the observability stack is running, **When** the developer inspects the Tilt dashboard, **Then** all observability resources appear under an "observability" label group.

---

### User Story 2 - Configure Port Assignments (Priority: P2)

A developer working on a project that already uses default ports (e.g., port 3000 for a frontend server) needs to customize which host ports the observability components bind to, avoiding conflicts with existing services.

**Why this priority**: Port conflicts are a common pain point in local development. Configurability here prevents the extension from being unusable when defaults collide.

**Independent Test**: Can be tested by calling the enable function with custom port overrides and verifying port-forwards use the specified ports.

**Acceptance Scenarios**:

1. **Given** a Tiltfile that already uses port 3000 for another service, **When** the developer calls the enable function with a custom Grafana port (e.g., 3001), **Then** Grafana's port-forward binds to 3001 instead of 3000.
2. **Given** the developer provides port overrides for all four components, **When** the stack deploys, **Then** each component's port-forward uses the specified port.

---

### User Story 3 - Display OTel Environment Variables (Priority: P2)

A developer building application services wants to know the correct OTLP endpoint environment variables for metrics, logs, and traces so their application containers can send telemetry to the observability stack. The endpoints are always the same regardless of which service is being instrumented.

**Why this priority**: The observability stack is only useful if applications can send telemetry to it. Displaying ready-to-use OTel environment configuration removes guesswork and is a key differentiator over manual setup.

**Independent Test**: Can be tested by enabling the observability stack and verifying the correct OTLP endpoint URLs and protocols are printed to the Tilt log output and/or displayed in the Tilt UI.

**Acceptance Scenarios**:

1. **Given** the observability stack is enabled with any signal backends, **When** the extension starts, **Then** the correct OTLP endpoint environment variables for all enabled signal types are printed to the Tilt log and/or displayed in the Tilt UI.
2. **Given** only metrics and traces are enabled, **When** the extension starts, **Then** only metrics and traces OTLP endpoint variables are displayed (logs endpoints are omitted).

---

### User Story 4 - Enable Continuous Profiling (Priority: P3)

A developer investigating performance issues wants to enable continuous profiling (Parca) alongside or independently of the core observability stack. Parca can run standalone and requires additional privileged resources (eBPF agent DaemonSet with kernel filesystem mounts).

**Why this priority**: Continuous profiling is a power-user feature that builds on top of the core stack. It has additional system requirements (privileged containers, kernel access) making it an opt-in add-on.

**Independent Test**: Can be tested by calling the enable function with profiling enabled and verifying Parca server and agent are deployed, and that Parca is accessible via its port-forward.

**Acceptance Scenarios**:

1. **Given** the developer calls the enable function with profiling enabled, **When** the stack deploys, **Then** both the Parca server and Parca agent DaemonSet are running alongside the core observability components.
2. **Given** the developer enables profiling without any core backends, **When** the stack deploys, **Then** only the Parca server and agent are deployed -- profiling operates standalone without error.
3. **Given** profiling is enabled, **When** the developer opens the Parca UI, **Then** eBPF-based profiles are being collected from cluster nodes.

---

### User Story 5 - Scrape Prometheus Metrics via VMAgent (Priority: P1)

A developer running services that expose Prometheus `/metrics` endpoints (but do not use OTel) wants the observability stack to automatically scrape those metrics into VictoriaMetrics. They should be able to use standard Prometheus annotations (`prometheus.io/scrape: "true"`) for simple cases, and Prometheus Operator CRDs (ServiceMonitor, PodMonitor, ScrapeConfig, Probe) for richer configuration, matching the patterns used in production Kubernetes environments.

**Why this priority**: Not all services support OTel yet. Prometheus scraping via VMAgent bridges this gap and makes the stack useful for the majority of existing Kubernetes workloads that expose `/metrics`. Annotation-based discovery provides zero-config scraping for annotated services, while Prometheus Operator CRDs provide structured, validated configuration portable between local dev and production.

**Independent Test**: Can be tested by deploying a service with a Prometheus `/metrics` endpoint and `prometheus.io/scrape: "true"` annotation, verifying metrics appear in VictoriaMetrics. Additionally, create a ServiceMonitor CR and verify CRD-based scraping works.

**Acceptance Scenarios**:

1. **Given** a service with `prometheus.io/scrape: "true"` annotation, **When** VMAgent is running, **Then** VMAgent auto-discovers and scrapes the target, and metrics are queryable in VictoriaMetrics.
2. **Given** a pod with `prometheus.io/scrape: "true"`, `prometheus.io/port: "8080"`, and `prometheus.io/path: "/custom-metrics"` annotations, **When** VMAgent is running, **Then** VMAgent scrapes the correct port and path.
3. **Given** a service exposing Prometheus metrics, **When** the developer creates a `ServiceMonitor` CR targeting that service, **Then** VMAgent discovers and scrapes the target.
4. **Given** VMAgent is enabled, **When** the developer creates a `PodMonitor` CR, **Then** VMAgent scrapes matching pods.
5. **Given** VMAgent is enabled, **When** the developer creates a `ScrapeConfig` CR with static targets, **Then** VMAgent scrapes those targets.
6. **Given** VMAgent is enabled, **When** the developer creates a `Probe` CR for blackbox monitoring, **Then** VMAgent performs the probe checks.

---

### User Story 6 - Provision Custom Grafana Dashboards via CRD (Priority: P3)

A developer wants to ship custom Grafana dashboards alongside their project so that when the observability stack starts, relevant dashboards are pre-loaded in Grafana without manual import. Dashboard provisioning is exclusively via the `GrafanaDashboard` CRD (`grafana.integreatly.org/v1beta1`), managed by the Grafana Operator.

**Why this priority**: Pre-provisioned dashboards turn raw telemetry into immediate visibility. Using the GrafanaDashboard CRD provides a declarative, Kubernetes-native approach consistent with production patterns.

**Independent Test**: Can be tested by creating a `GrafanaDashboard` CR with dashboard JSON and verifying it appears in the Grafana UI.

**Acceptance Scenarios**:

1. **Given** the developer creates a `GrafanaDashboard` CR containing dashboard JSON, **When** the Grafana Operator reconciles it, **Then** the dashboard is provisioned and visible in the Grafana UI.
2. **Given** no `GrafanaDashboard` CRs exist, **When** Grafana starts, **Then** Grafana launches with only the default home dashboard (no errors).
3. **Given** the developer updates a `GrafanaDashboard` CR, **When** the Grafana Operator reconciles, **Then** the dashboard is updated in the Grafana UI without manual intervention.

---

### User Story 7 - Export Telemetry Data Before Teardown (Priority: P2)

A developer has been debugging a performance issue with the observability stack running. Before tearing down the cluster, they want to export the collected metrics and traces to local files so they can continue analysis offline (e.g., in a Jupyter notebook).

**Why this priority**: Telemetry data is ephemeral by design, but the insights derived from a debugging session are valuable. Without an export path, tearing down the stack means losing all collected data, forcing developers to reproduce issues.

**Independent Test**: Can be tested by deploying the stack, sending test telemetry, triggering the export, tearing down the stack, and verifying the exported files contain the expected data and are loadable in a Python/Jupyter environment.

**Acceptance Scenarios**:

1. **Given** the observability stack has collected metrics over time, **When** the developer triggers a metrics export, **Then** complete time series data (not point-in-time snapshots) are exported to a DuckDB database file.
2. **Given** the observability stack has collected traces, **When** the developer triggers a traces export, **Then** traces are exported to the same DuckDB database file in a queryable schema.
3. **Given** Parca profiling is enabled and profiles have been collected, **When** the developer triggers an export, **Then** pprof profile files are downloaded to the export output directory alongside the DuckDB file, and are usable with `go tool pprof`.
4. **Given** the developer exports data, **When** they open the exported DuckDB file in a Jupyter notebook, **Then** metrics and traces are queryable via SQL using DuckDB's Python client.
5. **Given** the developer wants to export before teardown, **When** they use a Tilt dashboard button or CLI command, **Then** the export runs without requiring manual API calls to each backend.
6. **Given** the developer has exported pprof profiles, **When** they run `go tool pprof` against an exported profile, **Then** the profile loads correctly for interactive analysis.

---

### User Story 8 - Automatic OTel Env Var Injection via Webhook (Priority: P1)

A developer wants OTLP endpoint environment variables injected into application pods automatically without modifying each deployment's env section. They annotate their pod specs with `instrumentation.opentelemetry.io/inject-sdk: "true"` and the OpenTelemetry Operator webhook handles the rest. The OTel Operator is always deployed when the `otel` flag is enabled.

**Why this priority**: Webhook-based injection is the standard Kubernetes-native approach to OTLP configuration. It eliminates per-pod wiring and makes the stack zero-touch for applications that just need standard OTLP endpoints. This is essential infrastructure, not optional.

**Independent Test**: Can be tested by enabling the OTel Operator component, deploying a test pod with the inject-sdk annotation, and verifying OTLP env vars are present in the pod spec.

**Acceptance Scenarios**:

1. **Given** the developer enables the `otel_operator` component, **When** the stack deploys, **Then** the OpenTelemetry Operator and an Instrumentation CR are created in the target namespace.
2. **Given** the OTel Operator is running, **When** a pod is created with annotation `instrumentation.opentelemetry.io/inject-sdk: "true"`, **Then** the pod receives OTLP endpoint environment variables matching those returned by `otel_env()`.
3. **Given** only metrics and traces backends are enabled, **When** the Instrumentation CR is created, **Then** only metrics and traces OTLP env vars are configured (logs env vars are omitted).

---

### Edge Cases

- What happens when a port number is already in use by another Tilt resource or host process? The extension should not crash; Tilt's built-in port conflict handling will report the issue.
- What happens when the cluster does not have enough resources to run all observability components? Each component has defined resource requests; Kubernetes scheduling will surface pending pods via Tilt's existing resource status.
- What happens when the Parca agent cannot access kernel filesystems (e.g., in a restricted container runtime)? The Parca agent should fail gracefully with clear log messages without affecting the core observability stack.
- What happens when the developer calls the enable function multiple times? The function should be idempotent -- re-registering the same Tilt resources without error.
- What happens when the developer provides an invalid port number (negative, zero, or above 65535)? Configuration validation should reject it with a clear error before any resources are deployed.
- What happens when the developer enables only `otel` without any signal backends? The OTel Operator deploys but the Instrumentation CR has no endpoint env vars configured. Grafana Operator also deploys but with no datasources.
- What happens when the developer triggers an export but a backend is unreachable or has no data? The export should succeed for reachable backends and report which backends were skipped, producing partial output rather than failing entirely.
- What happens when the Helm CLI is not installed and `otel_operator` is enabled? The `local()` call for `helm template` will fail with a clear error indicating Helm is required.
- What happens when the OTel Operator webhook is not yet ready but pods are being created? The MutatingWebhookConfiguration uses `failurePolicy: Ignore` so pods are created normally without injection until the operator is ready.
- What happens when CRDs (Grafana Operator, Prometheus Operator, OTel Operator) already exist in the cluster? The extension uses server-side apply with force-conflicts to take ownership. Pre-existing CRDs are overwritten with the extension's versions.

## Requirements *(mandatory)*

### Functional Requirements

#### Component Configuration Model

- **FR-001**: The extension MUST provide the following boolean configuration flags:
  - `metrics` (default: `True`): Deploy VictoriaMetrics (metrics storage with OTLP ingestion)
  - `logs` (default: `True`): Deploy VictoriaLogs + Vector (log storage with OTLP ingestion, Vector for collection/forwarding)
  - `traces` (default: `True`): Deploy VictoriaTraces (trace storage with OTLP and Jaeger-compatible query APIs)
  - `prometheus_metrics` (default: `False`): Deploy VMAgent (Prometheus-compatible scraping agent)
  - `otel` (default: `False`): Deploy OpenTelemetry Operator (webhook-based OTLP env var injection)
  - `profiling` (default: `False`): Deploy Parca (server + agent DaemonSet)
- **FR-002**: When ANY of the above flags are enabled, the extension MUST automatically deploy the Grafana Operator (`grafana.integreatly.org`) and a managed Grafana instance. The developer MUST NOT need to explicitly enable Grafana.
- **FR-003**: When all flags are disabled, the function MUST be a valid no-op without error.

#### Core Signal Backends

- **FR-004**: When `metrics` is enabled, the extension MUST deploy VictoriaMetrics as a single-node Deployment with a Service.
- **FR-005**: When `logs` is enabled, the extension MUST deploy VictoriaLogs as a single-node Deployment with a Service, and Vector as a log collection/forwarding agent.
- **FR-006**: When `traces` is enabled, the extension MUST deploy VictoriaTraces as a single-node Deployment with a Service exposing both HTTP and OTLP gRPC ports.

#### Grafana Operator and Dashboards

- **FR-007**: The extension MUST deploy the Grafana Operator (`grafana.integreatly.org`) via Helm chart when any component flag is enabled.
- **FR-008**: The Grafana Operator MUST manage a Grafana instance with auto-provisioned `GrafanaDatasource` CRs for all enabled signal backends.
- **FR-009**: Custom Grafana dashboards MUST be provisioned exclusively via `GrafanaDashboard` CRDs (`grafana.integreatly.org/v1beta1`). No ConfigMap-based provisioning is supported.
- **FR-010**: The extension MUST only create `GrafanaDatasource` CRs for backends that are enabled, and only register Tilt resource dependencies on enabled backends.

#### OTel Environment Variables

- **FR-011**: When any signal backend is enabled, the extension MUST print the corresponding OTLP endpoint environment variables to the Tilt log output. The endpoints are non-service-specific (always the same for all applications).
- **FR-012**: The extension MUST display OTLP endpoint information in the Tilt UI (e.g., via a `local_resource` or Tilt dashboard text).
- **FR-013**: The OTLP endpoint variables MUST only include signal types whose corresponding backend is enabled.

#### OTel Operator

- **FR-014**: When `otel` is enabled, the extension MUST deploy the OpenTelemetry Operator (controller + mutating admission webhook). The OTel Operator is always deployed when this flag is set, regardless of other flags.
- **FR-015**: When the OTel Operator is enabled, the extension MUST create an Instrumentation custom resource that configures OTLP endpoint environment variables for all enabled signal backends.
- **FR-016**: The Instrumentation CR MUST only include environment variables for signal backends (metrics, logs, traces) that are enabled.
- **FR-017**: The OTel Operator MUST use self-signed certificates for webhook TLS via the Helm chart's `autoGenerateCert` option, without requiring cert-manager.
- **FR-018**: The extension MUST render the OTel Operator manifests using `helm template` with the `opentelemetry-operator` Helm chart, requiring the Helm CLI as a prerequisite.

#### Prometheus Metrics Scraping

- **FR-019**: When `prometheus_metrics` is enabled, the extension MUST deploy VMAgent as a scraping agent that forwards Prometheus metrics to VictoriaMetrics.
- **FR-020**: When `prometheus_metrics` is enabled, the extension MUST require `metrics` to also be enabled, and MUST fail with a clear error otherwise.
- **FR-021**: VMAgent MUST support Prometheus annotation-based service discovery (`prometheus.io/scrape: "true"`, `prometheus.io/port`, `prometheus.io/path`, `prometheus.io/scheme`) for zero-config scraping of annotated services and pods.
- **FR-022**: VMAgent MUST support all Prometheus Operator scraping CRDs: `ServiceMonitor`, `PodMonitor`, `ScrapeConfig`, and `Probe`. The extension MUST install the required CRDs.
- **FR-022a**: VMAgent MUST discover and honor both annotation-based targets and Prometheus Operator CRs for automated scrape target configuration.

#### Continuous Profiling

- **FR-023**: When `profiling` is enabled, the extension MUST deploy Parca (server Deployment + agent DaemonSet with RBAC).
- **FR-024**: Profiling MUST be standalone -- it can run without any core signal backends enabled.
- **FR-025**: When Parca profiling is enabled, the extension MUST auto-detect the containerd socket path by probing known locations and MUST fail with a clear error if no socket is found.

#### General

- **FR-026**: The extension MUST allow the developer to configure host port-forward assignments for each component via a configuration dictionary.
- **FR-027**: The extension MUST provide sensible default ports (VictoriaMetrics: 8428, VictoriaLogs: 9428, VictoriaTraces: 10428, Grafana: 3000, Parca: 7070).
- **FR-028**: The extension MUST label all deployed Tilt resources under the "observability" label group.
- **FR-029**: All deployed components MUST include readiness and liveness probes with appropriate timeouts.
- **FR-030**: The extension MUST follow the same extension.star pattern (public API function as entry point, internal helper functions, config validation with defaults).
- **FR-031**: The extension MUST provide a configuration option to set the Kubernetes namespace for all deployed resources, defaulting to "default".
- **FR-032**: The extension MUST validate all user-provided configuration values and reject unrecognized configuration keys with `fail()`.
- **FR-033**: The extension MUST use latest image tags by default for all components and MUST allow overriding each component's container image reference via the configuration dictionary.
- **FR-033a**: All cluster-scoped RBAC resources (ClusterRoles, ClusterRoleBindings) MUST use the `tilt-observability-` name prefix to avoid collisions with other installations and simplify cleanup.

#### Telemetry Export

- **FR-034**: The extension MUST provide a telemetry export capability that snapshots metrics and traces from their respective backends to local files before stack teardown.
- **FR-035**: The metrics export MUST produce complete time series data into a DuckDB database file with timestamped filenames.
- **FR-036**: The traces export MUST produce trace data into the same DuckDB database file with a queryable schema.
- **FR-037**: The export MUST be triggerable via a Tilt dashboard button or a single CLI command.
- **FR-038**: The extension MUST allow the developer to configure the export output directory, defaulting to a sensible location.
- **FR-039**: When profiling is enabled, the export MUST download pprof profile files in standard format compatible with `go tool pprof`.
- **FR-040**: The extension MUST ship example Jupyter notebooks and `go tool pprof` usage examples.
- **FR-041**: The e2e test suite MUST exercise the full export pipeline.

#### Testing

- **FR-042**: All extension tests MUST be written in Go (table-driven tests, gomega assertions). No Bash test scripts.

### Non-Functional Requirements

- **NFR-001**: All core observability components MUST pass their health checks and become accessible within 90 seconds of `tilt up` with cached container images (warm start). Cold starts (first pull from registry) are excluded from this target as they depend on network speed. (SC-002).
- **NFR-002**: The total resource footprint of the core stack (metrics, logs, traces, grafana) MUST NOT exceed approximately 1 CPU and 1.5 GiB memory at default resource requests.
- **NFR-003**: The extension MUST support environment variable overrides for key configuration values (namespace, ports, export directory) to enable CI/automation use cases without modifying the Tiltfile.
- **NFR-004**: The extension MUST be idempotent -- calling `enable_observability()` multiple times with the same config MUST NOT produce errors or duplicate resources.
- **NFR-005**: All signal backends (VictoriaMetrics, VictoriaLogs, VictoriaTraces) MUST use built-in retention flags (e.g., `-retentionPeriod`) to bound ephemeral storage growth, with sensible defaults for local development (e.g., 1 day retention).

### Key Entities

- **Observability Component**: A Kubernetes workload (Deployment or DaemonSet) plus its Service, representing one pillar of the observability stack (metrics, logs, traces, visualization, profiling).
- **Grafana Operator**: The `grafana.integreatly.org` operator that manages Grafana instances, datasources, and dashboards via CRDs.
- **GrafanaDatasource CR**: A `grafana.integreatly.org/v1beta1` custom resource that wires Grafana to backend storage.
- **GrafanaDashboard CR**: A `grafana.integreatly.org/v1beta1` custom resource that provisions Grafana dashboards declaratively.
- **OTel Environment Block**: A set of OTLP endpoint environment variable definitions printed to Tilt logs/UI for developer reference.
- **Scrape Configuration**: Prometheus annotations (`prometheus.io/scrape`) for zero-config discovery, plus Prometheus Operator CRDs (ServiceMonitor, PodMonitor, ScrapeConfig, Probe) for structured scrape target definition.
- **Telemetry Export**: A DuckDB database file containing metrics time series and trace data, plus pprof profile files, exported from backends for offline analysis.
- **OTel Operator**: The OpenTelemetry Operator deployment that provides a mutating admission webhook for automatic OTLP environment variable injection into annotated pods.
- **Instrumentation CR**: An OpenTelemetry Instrumentation custom resource that defines which OTLP endpoint environment variables the webhook injects.
- **Vector**: A log collection and forwarding agent deployed alongside VictoriaLogs when the `logs` flag is enabled.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A developer can add a complete observability stack (metrics, logs, traces, visualization) to any Tilt project in under 5 minutes by adding two lines to their Tiltfile (load + function call).
- **SC-002**: All four core observability components pass their health checks and become accessible within 90 seconds of `tilt up`.
- **SC-003**: Grafana displays all three datasources as operational on first load without any manual configuration.
- **SC-004**: The extension is reusable across multiple projects without modification -- no project-specific hardcoding is required.
- **SC-005**: Enabling profiling adds Parca server and agent without disrupting the core observability stack.
- **SC-006**: An end-to-end test validates the full observability pipeline by deploying the stack, sending test telemetry (metrics, logs, traces) via OTLP endpoints, confirming the data is queryable through each backend's API, and verifying successful export to DuckDB with valid pprof files.
- **SC-007**: Example Jupyter notebooks and `go tool pprof` usage are included in the repository and validated as part of the e2e test suite.

## Out of Scope

- Persistent storage (PersistentVolumeClaims) for metrics, logs, or traces -- all data is ephemeral for local development. Export before teardown is supported as a separate capability.
- Alerting rules or alert manager integration.
- Production deployment patterns (HA, clustering, TLS, authentication beyond local defaults).
- Data retention policies beyond component defaults.

## Assumptions

- The target cluster runs Kubernetes 1.34 or later.
- The target cluster supports Kubernetes Deployments, Services, ConfigMaps, and (for Parca) DaemonSets with privileged containers.
- Container images are pulled from public registries (Docker Hub for VictoriaMetrics/Grafana, GitHub Container Registry for Parca). No private registry configuration is required.
- The developer has sufficient cluster resources to run the observability stack (approximately 1 CPU and 1.5 GiB memory for all four core components at their default resource requests).
- The extension will ship its own Kubernetes manifests as asset files, following the extension.star pattern of bundling assets.
- The `app.kubernetes.io/part-of` label in manifests will be parameterizable or omitted (unlike the memex source which hardcodes `memex`).
- Grafana anonymous auth will be disabled by default (matching the memex source), with admin/admin as default credentials for local development.
- The Parca agent DaemonSet auto-detects the containerd socket path at deploy time, supporting k3s, kind, minikube, and standard containerd installations.
- The Helm CLI (v3.x) is available on the developer's PATH (required for OTel Operator and Grafana Operator).
- The Grafana Operator CRDs (`grafana.integreatly.org/v1beta1`) are installed by the extension (via Helm chart or direct CRD apply).
- Prometheus Operator CRDs (ServiceMonitor, PodMonitor, ScrapeConfig, Probe) are installed by the extension when `prometheus_metrics` is enabled.
- All CRD installations use server-side apply with force-conflicts, making the extension authoritative in local dev clusters. Pre-existing CRDs from other tools are overwritten.
