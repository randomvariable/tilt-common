# Copyright 2026 Naadir Jeewa
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

"""Observability Stack Extension.

Deploy a complete local observability stack with a single function call.
Includes VictoriaMetrics (metrics, logs, traces), Grafana, VMAgent, Parca,
Vector (container log collection), and optional OTel Operator and
VictoriaMetrics Operator for webhook-based env var injection and
Prometheus CRD compatibility.
"""

# ============================================================================
# Constants
# ============================================================================

_EXTENSION_DIR = os.path.dirname(__file__)

# NEW simplified flag model (post-clarification architecture)
# Grafana Operator and Vector are auto-deployed, not separate flags
_DEFAULT_CONFIG = {
    "namespace": "observability",
    "metrics": True,
    "logs": True,
    "traces": True,
    "prometheus_metrics": False,
    "otel": False,
    "profiling": False,
    "parca_agent": True,
    "ports": {
        "metrics": 8428,
        "logs": 9428,
        "traces": 10428,
        "grafana": 3000,
        "parca": 7070,
    },
    "images": {},
    "export_dir": "./observability-export",
}

_DEFAULT_IMAGES = {
    "metrics": "victoriametrics/victoria-metrics:latest",
    "logs": "victoriametrics/victoria-logs:latest",
    "traces": "victoriametrics/victoria-traces:latest",
    "grafana": "grafana/grafana:latest",
    "parca_server": "ghcr.io/parca-dev/parca:v0.25.0",
    "parca_agent": "ghcr.io/parca-dev/parca-agent:v0.25.0",
    "vmagent": "victoriametrics/vmagent:latest",
    "otel_operator": "ghcr.io/open-telemetry/opentelemetry-operator/opentelemetry-operator:0.116.0",
    "vector": "timberio/vector:latest-alpine",
    "grafana_operator": "ghcr.io/grafana/grafana-operator:v5.0.0",
}

_VALID_CONFIG_KEYS = [
    "namespace",
    "metrics",
    "logs",
    "traces",
    "prometheus_metrics",
    "otel",
    "profiling",
    "parca_agent",
    "ports",
    "images",
    "export_dir",
    "components",
    "scrape_targets",
    "dashboard_paths",
    "registry_mirror",
]

_VALID_PORT_KEYS = ["metrics", "logs", "traces", "grafana", "parca"]
_VALID_IMAGE_KEYS = ["metrics", "logs", "traces", "grafana", "parca_server", "parca_agent", "vmagent", "otel_operator", "vector", "grafana_operator"]
_VALID_COMPONENT_KEYS = ["metrics", "logs", "traces", "grafana", "vmagent", "otel_operator", "vector", "vm_operator", "profiling"]

# Component ID to Kubernetes resource name mapping
_COMPONENT_RESOURCE_NAMES = {
    "metrics": "VictoriaMetrics",
    "logs": "VictoriaLogs",
    "traces": "VictoriaTraces",
    "grafana": "Grafana",
    "parca_server": "Parca",
    "parca_agent": "Parca Agent",
    "vmagent": "VMAgent",
    "otel_operator": "otel-operator",
    "vector": "Vector",
    "vm_operator": "vm-operator",
}

# ============================================================================
# Public API
# ============================================================================

def enable_observability(config = {}):
    """Deploy observability stack with simplified component flags (NEW architecture).

    Deploys VictoriaMetrics, VictoriaLogs + Vector, VictoriaTraces, Grafana Operator,
    VMAgent, OTel Operator, and Parca based on simplified boolean flags.

    Grafana Operator is automatically deployed when ANY component is enabled.
    Vector is automatically deployed when logs=True.

    Args:
        config (dict): Configuration with simplified flags. All keys are optional.
            namespace (str): Kubernetes namespace. Default: "default"
            metrics (bool): Deploy VictoriaMetrics. Default: True
            logs (bool): Deploy VictoriaLogs + Vector. Default: True
            traces (bool): Deploy VictoriaTraces. Default: True
            prometheus_metrics (bool): Deploy VMAgent for Prometheus scraping. Default: False
            otel (bool): Deploy OpenTelemetry Operator. Default: False
            profiling (bool): Deploy Parca (standalone). Default: False
            ports (dict): Port overrides for components
            images (dict): Image overrides for components
            export_dir (str): Directory for telemetry exports. Default: "./observability-export"

    Returns:
        None

    Example:
        load('ext://tilt-common/observability', 'enable_observability')

        # Full stack with defaults (metrics/logs/traces enabled)
        enable_observability()

        # Custom configuration
        enable_observability({
            'namespace': 'observability',
            'prometheus_metrics': True,
            'ports': {'grafana': 3001},
        })
    """
    validated = _validate_config(config)

    # All-disabled no-op (FR-003)
    if _is_noop(validated):
        print("⚠️  All observability components disabled, nothing to deploy")
        return

    yamls = {}

    # Core signal backends
    if validated["metrics"]:
        yamls["metrics"] = _generate_component_yaml("metrics", validated)

    if validated["logs"]:
        yamls["logs"] = _generate_component_yaml("logs", validated)

        # Vector is auto-deployed with logs (NEW architecture)
        yamls["vector"] = _generate_component_yaml("vector", validated)
        yamls["vector-config"] = _generate_vector_config(validated)

    if validated["traces"]:
        yamls["traces"] = _generate_component_yaml("traces", validated)

    # Grafana Operator (auto-deployed when any component enabled - FR-002)
    if validated["metrics"] or validated["logs"] or validated["traces"]:
        _deploy_grafana_operator(validated)
        yamls["grafana-cr"] = _generate_grafana_cr(validated)
        datasources = _generate_grafana_datasources(validated)
        for ds_name, ds_yaml in datasources.items():
            yamls[ds_name] = ds_yaml

    # VMAgent for Prometheus scraping (requires metrics - FR-020)
    if validated["prometheus_metrics"]:
        _install_prometheus_operator_crds()
        yamls["vmagent"] = _generate_component_yaml("vmagent", validated)
        yamls["vmagent-config"] = _generate_vmagent_scrape_config(validated)

    # OTel Operator for webhook-based env var injection
    if validated["otel"]:
        _deploy_otel_operator(validated)
        yamls["otel-instrumentation"] = _generate_instrumentation_cr(validated)

    # Parca profiling (standalone - FR-024)
    # parca_agent requires a real Linux kernel with eBPF support; not supported on kind.
    if validated["profiling"]:
        yamls["parca"] = _generate_component_yaml("parca", validated)
        if validated["parca_agent"]:
            socket_path = _detect_containerd_socket()
            yamls["parca-agent"] = _generate_component_yaml("parca-agent", validated, extra = {"containerd_socket": socket_path})

    # Print OTel environment variables for enabled backends (FR-011, FR-012)
    _print_otel_env_vars(validated)

    _deploy_to_tilt(validated, yamls)

def otel_env(service_name = "", config = {}):
    """Return OpenTelemetry SDK environment variables for application containers (NEW architecture).

    Generates OTEL_EXPORTER_OTLP_* variables for enabled backends using simplified flags.

    Args:
        service_name (str): Value for OTEL_SERVICE_NAME. Optional.
        config (dict): Config dict with simplified flags (metrics, logs, traces).

    Returns:
        list[str]: KEY=VALUE strings for container environment injection.

    Example:
        # Get env vars for enabled backends
        env_list = otel_env(service_name="my-app", config={'namespace': 'prod'})

        # Use in container spec
        for env_var in env_list:
            key, val = env_var.split("=", 1)
            k8s_container_env[key] = val
    """

    # Apply defaults for standalone use
    ns = config.get("namespace", "default")
    metrics = config.get("metrics", True)
    logs = config.get("logs", True)
    traces = config.get("traces", True)

    env_vars = []

    if metrics:
        env_vars.append("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://victoriametrics.%s.svc.cluster.local:8428/opentelemetry/api/v1/push" % ns)
        env_vars.append("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf")

    if logs:
        env_vars.append("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://victorialogs.%s.svc.cluster.local:9428/insert/opentelemetry/v1/logs" % ns)
        env_vars.append("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf")

    if traces:
        env_vars.append("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://victoriatraces.%s.svc.cluster.local:4317" % ns)
        env_vars.append("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc")

    if service_name:
        env_vars.append("OTEL_SERVICE_NAME=%s" % service_name)

    return env_vars

def export_telemetry(config = {}):
    """Register a Tilt local_resource for one-shot telemetry data export.

    Creates a manually-triggered Tilt resource called "observability-export" that
    runs the Go export CLI (cmd/observability-export). The CLI connects to the
    port-forwarded backends and downloads metrics, traces, and pprof profiles
    into the configured export directory.

    Args:
        config: Same config dict accepted by enable_observability().
    """
    validated = _validate_config(config)
    export_dir = validated["export_dir"]

    # Build CLI flags (NEW flat flag model)
    flags = ["--output-dir", export_dir]
    flags.extend(["--metrics-url", "http://localhost:%d" % validated["ports"]["metrics"]])
    flags.extend(["--traces-url", "http://localhost:%d" % validated["ports"]["traces"]])
    flags.extend(["--logs-url", "http://localhost:%d" % validated["ports"]["logs"]])

    if validated["profiling"]:
        flags.extend(["--parca-url", "http://localhost:%d" % validated["ports"]["parca"]])
    else:
        flags.append("--skip-profiles")

    if not validated["metrics"]:
        flags.append("--skip-metrics")
    if not validated["traces"]:
        flags.append("--skip-traces")
    if not validated["logs"]:
        flags.append("--skip-logs")

    cmd = "go run ./cmd/observability-export/ " + " ".join(flags)

    local_resource(
        "observability-export",
        cmd = cmd,
        labels = ["observability"],
        auto_init = False,
        trigger_mode = TRIGGER_MODE_MANUAL,
    )

# ============================================================================
# Internal Functions
# ============================================================================

def _validate_config(config):
    """Validate user configuration and apply defaults.

    Args:
        config: User-provided config dict.

    Returns:
        dict: Validated config with defaults applied.

    Raises:
        fail(): If configuration is invalid.
    """

    # Check for unrecognized top-level keys (FR-032)
    for key in config:
        if key not in _VALID_CONFIG_KEYS:
            fail("observability: unrecognized configuration key '%s'. Valid keys: %s" % (key, ", ".join(sorted(_VALID_CONFIG_KEYS))))

    # Merge user config with defaults
    validated = dict(_DEFAULT_CONFIG)
    for key in config:
        if key == "ports":
            # Merge port overrides
            validated["ports"] = dict(_DEFAULT_CONFIG["ports"])

            # Validate port keys before merging
            for port_key in config["ports"]:
                if port_key not in _VALID_PORT_KEYS:
                    fail("observability: unrecognized port key '%s'. Valid keys: %s" % (port_key, ", ".join(sorted(_VALID_PORT_KEYS))))
            validated["ports"].update(config["ports"])
        elif key == "images":
            # Merge image overrides
            validated["images"] = dict(_DEFAULT_IMAGES)
            validated["images"].update(config["images"])
        elif key == "components":
            # Handle components dict (legacy/alternate API)
            # Validate component keys
            for comp_key in config["components"]:
                if comp_key not in _VALID_COMPONENT_KEYS:
                    fail("observability: unrecognized component key '%s'. Valid keys: %s" % (comp_key, ", ".join(sorted(_VALID_COMPONENT_KEYS))))

            # Map components to flat flags
            validated[key] = config[key]
        else:
            validated[key] = config[key]

    # Handle components dict (map to flat flags)
    components = validated.get("components", {})
    if components:
        # Map component flags to top-level flags
        if "metrics" in components:
            validated["metrics"] = components["metrics"]
        if "logs" in components:
            validated["logs"] = components["logs"]
        if "traces" in components:
            validated["traces"] = components["traces"]
        if "profiling" in components:
            validated["profiling"] = components["profiling"]
        if "vmagent" in components:
            validated["prometheus_metrics"] = components["vmagent"]
        if "otel_operator" in components:
            validated["otel"] = components["otel_operator"]
        if "vector" in components:
            # Vector is auto-deployed with logs, so this is just enabling logs
            if components.get("vector") and not components.get("logs", True):
                fail("observability: vector requires logs=True (Vector is the log collector)")
        if "vm_operator" in components:
            # vm_operator requires vmagent and metrics
            if components.get("vm_operator"):
                if not components.get("vmagent", validated["prometheus_metrics"]):
                    fail("observability: vm_operator requires vmagent=True (VM Operator reconciles VMAgent CRs)")

                # vmagent check will be done below

    # Environment variable overrides (NFR-003)
    env_namespace = os.environ.get("TILT_OBSERVABILITY_NAMESPACE", "")
    if env_namespace:
        validated["namespace"] = env_namespace

    env_export_dir = os.environ.get("TILT_OBSERVABILITY_EXPORT_DIR", "")
    if env_export_dir:
        validated["export_dir"] = env_export_dir

    # Validate namespace (RFC 1123)
    ns = validated["namespace"]
    if type(ns) != "string" or not ns:
        fail("observability: namespace must be a non-empty string, got: %s" % repr(ns))
    if ns != ns.lower():
        fail("observability: namespace must be lowercase, got: '%s'" % ns)
    if " " in ns or "." in ns or "_" in ns:
        fail("observability: namespace must not contain spaces, dots, or underscores (RFC 1123): '%s'" % ns)

    # Validate boolean flags
    bool_flags = ["metrics", "logs", "traces", "prometheus_metrics", "otel", "profiling", "parca_agent"]
    for flag in bool_flags:
        val = validated[flag]
        if type(val) != "bool":
            fail("observability: %s must be a boolean, got: %s" % (flag, type(val)))

    # Dependency: prometheus_metrics requires metrics (FR-020)
    if validated["prometheus_metrics"] and not validated["metrics"]:
        fail("observability: prometheus_metrics=True requires metrics=True (VMAgent needs VictoriaMetrics for remote write)")

    # Dependency: otel requires at least one backend
    if validated["otel"] and not (validated["metrics"] or validated["logs"] or validated["traces"]):
        fail("observability: otel=True requires at least one backend (metrics, logs, or traces)")

    # Validate port ranges (already merged in validated dict)
    _port_env_map = {
        "metrics": "TILT_OBSERVABILITY_PORT_METRICS",
        "logs": "TILT_OBSERVABILITY_PORT_LOGS",
        "traces": "TILT_OBSERVABILITY_PORT_TRACES",
        "grafana": "TILT_OBSERVABILITY_PORT_GRAFANA",
        "parca": "TILT_OBSERVABILITY_PORT_PARCA",
    }
    for port_key, env_key in _port_env_map.items():
        env_val = os.environ.get(env_key, "")
        if env_val:
            validated["ports"][port_key] = int(env_val)

    for port_key, port_val in validated["ports"].items():
        if type(port_val) != "int":
            fail("observability: port '%s' must be an integer, got: %s" % (port_key, type(port_val)))
        if port_val < 1 or port_val > 65535:
            fail("observability: port '%s' must be between 1 and 65535, got: %d" % (port_key, port_val))

    # Validate image overrides (already merged in validated dict)
    for key, img in validated["images"].items():
        if key not in _VALID_IMAGE_KEYS:
            fail("observability: unrecognized image key '%s'. Valid keys: %s" % (key, ", ".join(sorted(_VALID_IMAGE_KEYS))))
        if type(img) != "string" or not img:
            fail("observability: image '%s' must be a non-empty string, got: %s" % (key, repr(img)))

    return validated

def _is_noop(config):
    """Check if all components are disabled (valid no-op per FR-003).

    Args:
        config (dict): Validated configuration

    Returns:
        bool: True if all components disabled, False otherwise
    """
    return not (
        config["metrics"] or
        config["logs"] or
        config["traces"] or
        config["prometheus_metrics"] or
        config["otel"] or
        config["profiling"]
    )

def _print_otel_env_vars(config):
    """Print OTel environment variables to Tilt log (FR-011, FR-012).

    Args:
        config (dict): Validated configuration

    Returns:
        None
    """
    ns = config["namespace"]
    env_vars = []

    if config["metrics"]:
        env_vars.append("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://victoriametrics.%s.svc.cluster.local:8428/opentelemetry/api/v1/push" % ns)
        env_vars.append("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf")

    if config["logs"]:
        env_vars.append("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://victorialogs.%s.svc.cluster.local:9428/insert/opentelemetry/v1/logs" % ns)
        env_vars.append("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf")

    if config["traces"]:
        env_vars.append("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://victoriatraces.%s.svc.cluster.local:4317" % ns)
        env_vars.append("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc")

    if env_vars:
        print("📊 OpenTelemetry environment variables for enabled backends:")
        for var in env_vars:
            print("   " + var)

def _generate_component_yaml(component_id, config, extra = {}):
    """Generate Kubernetes YAML for a single observability component.

    Args:
        component_id: One of 'metrics', 'logs', 'traces', 'grafana', 'parca', 'vmagent'.
        config: Validated configuration.
        extra: Additional substitution values (e.g., containerd_socket for parca).

    Returns:
        str: Kubernetes YAML content with placeholders substituted.
    """
    template_path = _EXTENSION_DIR + "/assets/" + component_id + ".yaml"
    template = str(read_file(template_path))

    ns = config["namespace"]
    images = config["images"]

    yaml = template
    yaml = yaml.replace("__NAMESPACE__", ns)

    # Image substitution varies by component
    if component_id == "metrics":
        yaml = yaml.replace("__IMAGE__", images.get("metrics", _DEFAULT_IMAGES["metrics"]))
    elif component_id == "logs":
        yaml = yaml.replace("__IMAGE__", images.get("logs", _DEFAULT_IMAGES["logs"]))
    elif component_id == "traces":
        yaml = yaml.replace("__IMAGE__", images.get("traces", _DEFAULT_IMAGES["traces"]))
    elif component_id == "grafana":
        yaml = yaml.replace("__IMAGE__", images.get("grafana", _DEFAULT_IMAGES["grafana"]))
    elif component_id == "vmagent":
        yaml = yaml.replace("__IMAGE__", images.get("vmagent", _DEFAULT_IMAGES["vmagent"]))
        yaml = yaml.replace("__METRICS_SERVICE__", "victoriametrics.%s.svc.cluster.local" % ns)
    elif component_id == "vector":
        yaml = yaml.replace("__IMAGE__", images.get("vector", _DEFAULT_IMAGES["vector"]))
        yaml = yaml.replace("__LOGS_SERVICE__", "victorialogs.%s.svc.cluster.local" % ns)
    elif component_id == "parca":
        yaml = yaml.replace("__PARCA_SERVER_IMAGE__", images.get("parca_server", _DEFAULT_IMAGES["parca_server"]))
    elif component_id == "parca-agent":
        yaml = yaml.replace("__PARCA_AGENT_IMAGE__", images.get("parca_agent", _DEFAULT_IMAGES["parca_agent"]))
        socket = extra.get("containerd_socket", "/run/containerd/containerd.sock")
        yaml = yaml.replace("__CONTAINERD_SOCKET__", socket)

    return yaml

def _deploy_grafana_operator(config):
    """Deploy Grafana Operator via Helm.

    Uses the official Grafana Operator Helm chart from OCI registry.
    Renders via local_resource so it doesn't block Tiltfile evaluation.

    Args:
        config: Validated configuration.
    """
    ns = config["namespace"]
    images = config["images"]

    # Install Grafana Operator CRDs first (v5.16.0)
    # Helm does not automatically install/update CRDs, so install them separately
    # https://grafana.github.io/grafana-operator/docs/installation/helm/
    crd_url = "https://github.com/grafana/grafana-operator/releases/download/v5.16.0/crds.yaml"
    local_resource(
        "grafana-operator-crds",
        "kubectl apply --server-side --force-conflicts -f %s" % crd_url,
        labels = ["observability"],
    )

    # Grafana Operator Helm chart: https://github.com/grafana/grafana-operator
    # Chart: oci://ghcr.io/grafana/helm-charts/grafana-operator
    helm_cmd = [
        "helm",
        "template",
        "grafana-operator",
        "oci://ghcr.io/grafana/helm-charts/grafana-operator",
        "--version",
        "v5.16.0",
        "--namespace",
        ns,
        "--set",
        "leaderElect=false",  # Single-replica for local dev
    ]

    # Override image if user provides custom one
    image = images.get("grafana_operator", "")
    if image and image != _DEFAULT_IMAGES["grafana_operator"]:
        parts = image.rsplit(":", 1)
        repo = parts[0]
        tag = parts[1] if len(parts) > 1 else "latest"
        helm_cmd += ["--set", "image.repository=%s" % repo, "--set", "image.tag=%s" % tag]

    # Use local_resource to render and apply Helm chart after CRDs are installed
    local_resource(
        "grafana-operator-helm",
        " ".join(helm_cmd) + " | kubectl apply -f -",
        labels = ["observability"],
        resource_deps = ["grafana-operator-crds", "%s-namespace" % ns],
    )

def _install_prometheus_operator_crds():
    """Install Prometheus Operator CRDs for VMAgent.

    Installs ServiceMonitor, PodMonitor, ScrapeConfig, and Probe CRDs
    that VMAgent can discover and use for scraping.
    """

    # Prometheus Operator CRDs from GitHub releases
    # Using v0.79.0 - latest stable release
    crd_url = "https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.79.0/stripped-down-crds.yaml"

    # Use local_resource to fetch and apply CRDs
    # Server-side apply with force-conflicts to handle existing CRDs
    local_resource(
        "prometheus-operator-crds",
        "kubectl apply --server-side --force-conflicts -f " + crd_url,
        labels = ["observability"],
    )

def _deploy_otel_operator(config):
    """Deploy OpenTelemetry Operator via Helm.

    Uses the official OTel Operator Helm chart.
    Configured with self-signed certs and failurePolicy: Ignore for local dev.

    Args:
        config: Validated configuration.
    """
    ns = config["namespace"]
    images = config["images"]

    # OTel Operator Helm chart: https://github.com/open-telemetry/opentelemetry-helm-charts
    # Chart: open-telemetry/opentelemetry-operator
    helm_cmd = [
        "helm",
        "template",
        "otel-operator",
        "open-telemetry/opentelemetry-operator",
        "--version",
        "0.105.1",
        "--namespace",
        ns,
        "--set",
        "admissionWebhooks.certManager.enabled=false",
        "--set",
        "admissionWebhooks.autoGenerateCert.enabled=true",
        "--set",
        "admissionWebhooks.failurePolicy=Ignore",  # Don't block on webhook failures in local dev
    ]

    # Override image if user provides custom one
    image = images.get("otel_operator", "")
    if image and image != _DEFAULT_IMAGES["otel_operator"]:
        parts = image.rsplit(":", 1)
        repo = parts[0]
        tag = parts[1] if len(parts) > 1 else "latest"
        helm_cmd += ["--set", "manager.image.repository=%s" % repo, "--set", "manager.image.tag=%s" % tag]

    # Add OpenTelemetry Helm repo if needed
    repo_add_cmd = "helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts --force-update 2>/dev/null || true"

    # Use local_resource to render and apply Helm chart
    local_resource(
        "otel-operator-helm",
        repo_add_cmd + " && " + " ".join(helm_cmd) + " | kubectl apply -f -",
        labels = ["observability"],
        resource_deps = ["%s-namespace" % ns],
    )

def _generate_grafana_cr(config):
    """Generate Grafana custom resource.

    Creates a Grafana CR for the Grafana Operator to reconcile.

    Args:
        config: Validated configuration.

    Returns:
        str: Grafana CR YAML.
    """
    ns = config["namespace"]

    yaml = """apiVersion: grafana.integreatly.org/v1beta1
kind: Grafana
metadata:
  name: grafana
  namespace: %s
  labels:
    app: grafana
    app.kubernetes.io/managed-by: tilt-observability
spec:
  config:
    auth:
      disable_login_form: "false"
    auth.anonymous:
      enabled: "true"
      org_role: Admin
    security:
      admin_user: admin
      admin_password: admin
  deployment:
    spec:
      replicas: 1
      template:
        spec:
          containers:
            - name: grafana
              resources:
                requests:
                  cpu: 100m
                  memory: 128Mi
                limits:
                  cpu: 500m
                  memory: 256Mi
  service:
    spec:
      ports:
        - name: http
          port: 3000
          targetPort: 3000
          protocol: TCP
""" % ns

    return yaml

def _generate_grafana_datasources(config):
    """Generate GrafanaDatasource CRs for enabled backends.

    Creates GrafanaDatasource custom resources for each enabled backend.

    Args:
        config: Validated configuration.

    Returns:
        dict: Mapping of datasource name -> YAML content.
    """
    ns = config["namespace"]
    datasources = {}

    if config["metrics"]:
        ds_yaml = """apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDatasource
metadata:
  name: victoriametrics
  namespace: %s
  labels:
    app: grafana
    app.kubernetes.io/managed-by: tilt-observability
spec:
  instanceSelector:
    matchLabels:
      app: grafana
  datasource:
    name: VictoriaMetrics
    type: prometheus
    uid: victoriametrics
    access: proxy
    url: http://victoriametrics.%s.svc.cluster.local:8428
    isDefault: true
    editable: true
""" % (ns, ns)
        datasources["grafana-ds-metrics"] = ds_yaml

    if config["logs"]:
        ds_yaml = """apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDatasource
metadata:
  name: victorialogs
  namespace: %s
  labels:
    app: grafana
    app.kubernetes.io/managed-by: tilt-observability
spec:
  instanceSelector:
    matchLabels:
      app: grafana
  datasource:
    name: VictoriaLogs
    type: victoriametrics-logs-datasource
    uid: victorialogs
    access: proxy
    url: http://victorialogs.%s.svc.cluster.local:9428
    editable: true
""" % (ns, ns)
        datasources["grafana-ds-logs"] = ds_yaml

    if config["traces"]:
        ds_yaml = """apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDatasource
metadata:
  name: victoriatraces
  namespace: %s
  labels:
    app: grafana
    app.kubernetes.io/managed-by: tilt-observability
spec:
  instanceSelector:
    matchLabels:
      app: grafana
  datasource:
    name: VictoriaTraces
    type: jaeger
    uid: victoriatraces
    access: proxy
    url: http://victoriatraces.%s.svc.cluster.local:10428/select/jaeger
    editable: true
""" % (ns, ns)
        datasources["grafana-ds-traces"] = ds_yaml

    return datasources

def _generate_vmagent_scrape_config(config):
    """Generate VMAgent scrape configuration ConfigMap YAML.

    Args:
        config: Validated configuration.

    Returns:
        str: ConfigMap YAML content.
    """
    ns = config["namespace"]
    scrape_targets = config.get("scrape_targets", [])

    # Build static targets section
    static_section = ""
    if scrape_targets:
        targets_str = ", ".join(["'%s'" % t for t in scrape_targets])
        static_section = """
    - job_name: static-targets
      static_configs:
      - targets: [%s]""" % targets_str

    template_path = _EXTENSION_DIR + "/assets/vmagent-config.yaml"
    template = str(read_file(template_path))

    yaml = template
    yaml = yaml.replace("__NAMESPACE__", ns)
    yaml = yaml.replace("__STATIC_TARGETS__", static_section)

    return yaml

def _generate_vector_config(config):
    """Generate Vector configuration ConfigMap YAML.

    Reads the vector-config.yaml template and substitutes the namespace
    and VictoriaLogs service address.

    Args:
        config: Validated configuration.

    Returns:
        str: ConfigMap YAML content.
    """
    ns = config["namespace"]

    template_path = _EXTENSION_DIR + "/assets/vector-config.yaml"
    template = str(read_file(template_path))

    yaml = template
    yaml = yaml.replace("__NAMESPACE__", ns)
    yaml = yaml.replace("__LOGS_SERVICE__", "victorialogs.%s.svc.cluster.local" % ns)

    return yaml

def _detect_containerd_socket():
    """Determine containerd socket path for the cluster node.

    Returns the path configured via TILT_OBSERVABILITY_CONTAINERD_SOCKET env var,
    or defaults to the standard containerd socket path. Override this for
    non-standard runtimes (e.g., k3s: /run/k3s/containerd/containerd.sock,
    microk8s: /var/snap/microk8s/common/run/containerd.sock).

    Returns:
        str: Socket path on the node.
    """

    # Allow env var override for non-standard runtimes (NFR-003)
    env_socket = os.environ.get("TILT_OBSERVABILITY_CONTAINERD_SOCKET", "")
    if env_socket:
        return env_socket

    return "/run/containerd/containerd.sock"

def _generate_instrumentation_cr(config):
    """Generate an OpenTelemetry Instrumentation CR for webhook injection.

    Creates an Instrumentation resource whose env vars match those returned
    by otel_env(). Pods annotated with
    instrumentation.opentelemetry.io/inject-sdk: "true" will receive these
    env vars automatically via the OTel Operator's mutating webhook.

    Args:
        config: Validated configuration.

    Returns:
        str: Instrumentation CR YAML content.
    """
    ns = config["namespace"]

    env_entries = []

    if config["metrics"]:
        env_entries.append("    - name: OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")
        env_entries.append("      value: http://victoriametrics.%s.svc.cluster.local:8428/opentelemetry/api/v1/push" % ns)
        env_entries.append("    - name: OTEL_EXPORTER_OTLP_METRICS_PROTOCOL")
        env_entries.append("      value: http/protobuf")

    if config["logs"]:
        env_entries.append("    - name: OTEL_EXPORTER_OTLP_LOGS_ENDPOINT")
        env_entries.append("      value: http://victorialogs.%s.svc.cluster.local:9428/insert/opentelemetry/v1/logs" % ns)
        env_entries.append("    - name: OTEL_EXPORTER_OTLP_LOGS_PROTOCOL")
        env_entries.append("      value: http/protobuf")

    if config["traces"]:
        env_entries.append("    - name: OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
        env_entries.append("      value: http://victoriatraces.%s.svc.cluster.local:10428/insert/opentelemetry/v1/traces" % ns)
        env_entries.append("    - name: OTEL_EXPORTER_OTLP_TRACES_PROTOCOL")
        env_entries.append("      value: http/protobuf")

    env_section = ""
    if env_entries:
        env_section = "  env:\n" + "\n".join(env_entries)

    # Default exporter endpoint for the Instrumentation CR spec
    default_endpoint = ""
    if config["traces"]:
        default_endpoint = "http://victoriatraces.%s.svc.cluster.local:10428" % ns
    elif config["metrics"]:
        default_endpoint = "http://victoriametrics.%s.svc.cluster.local:8428" % ns
    elif config["logs"]:
        default_endpoint = "http://victorialogs.%s.svc.cluster.local:9428" % ns

    yaml = """apiVersion: opentelemetry.io/v1alpha1
kind: Instrumentation
metadata:
  name: observability-auto
  namespace: %s
  labels:
    app.kubernetes.io/managed-by: tilt-observability
spec:
  exporter:
    endpoint: %s
  propagators:
    - tracecontext
    - baggage
%s
""" % (ns, default_endpoint, env_section)

    return yaml

def _deploy_to_tilt(config, yamls):
    """Register all generated YAML and resources with Tilt.

    Args:
        config: Validated configuration.
        yamls: Component ID -> YAML string mapping.
    """
    ports = config["ports"]
    ns = config["namespace"]

    # Create namespace with PSA labels (needed for parca-agent privileged pods).
    # Use local_resource so all other resources (both k8s_resource and local_resource)
    # can declare an explicit dependency and wait until the namespace actually exists.
    # k8s_yaml does not guarantee namespace-before-workload ordering within a single batch.
    ns_template_path = _EXTENSION_DIR + "/assets/namespace.yaml"
    ns_yaml = str(read_file(ns_template_path)).replace("__NAMESPACE__", ns)
    ns_resource_name = "%s-namespace" % ns
    local_resource(
        ns_resource_name,
        "echo %s | kubectl apply -f -" % shlex.quote(ns_yaml),
        labels = ["observability"],
    )

    # Register all YAML EXCEPT Grafana operator CRs
    # (Grafana CR and GrafanaDatasource CRs must wait for operator CRDs to be installed)
    grafana_cr_yaml = None
    datasource_yamls = {}
    for component_id, yaml_content in yamls.items():
        if component_id == "grafana-cr":
            grafana_cr_yaml = yaml_content
        elif component_id.startswith("grafana-ds-"):
            datasource_yamls[component_id] = yaml_content
        else:
            k8s_yaml(blob(yaml_content))

    # Core backends
    grafana_deps = []

    if config["metrics"] and "metrics" in yamls:
        k8s_resource(
            "victoriametrics",
            new_name = _COMPONENT_RESOURCE_NAMES["metrics"],
            labels = ["observability"],
            port_forwards = ["%d:8428" % ports["metrics"]],
            links = [link("http://localhost:%d/vmui" % ports["metrics"], "VMUI")],
            resource_deps = [ns_resource_name],
        )
        grafana_deps.append(_COMPONENT_RESOURCE_NAMES["metrics"])

    if config["logs"] and "logs" in yamls:
        k8s_resource(
            "victorialogs",
            new_name = _COMPONENT_RESOURCE_NAMES["logs"],
            labels = ["observability"],
            port_forwards = ["%d:9428" % ports["logs"]],
            links = [link("http://localhost:%d/select/vmui" % ports["logs"], "VictoriaLogs UI")],
            resource_deps = [ns_resource_name],
        )
        grafana_deps.append(_COMPONENT_RESOURCE_NAMES["logs"])

    if config["traces"] and "traces" in yamls:
        k8s_resource(
            "victoriatraces",
            new_name = _COMPONENT_RESOURCE_NAMES["traces"],
            labels = ["observability"],
            port_forwards = ["%d:10428" % ports["traces"]],
            links = [link("http://localhost:%d/" % ports["traces"], "VictoriaTraces UI")],
            resource_deps = [ns_resource_name],
        )
        grafana_deps.append(_COMPONENT_RESOURCE_NAMES["traces"])

    # Grafana Operator and CR auto-deploy when any component enabled (FR-005)
    if grafana_cr_yaml:
        # grafana-operator-helm local_resource created by _deploy_grafana_operator
        # Wait for operator deployment to be ready before applying CRs
        # CRDs are already installed via grafana-operator-crds resource
        local_resource(
            "grafana-operator-ready",
            "kubectl wait --for=condition=Available deployment/grafana-operator -n %s --timeout=120s" % ns,
            labels = ["observability"],
            resource_deps = ["grafana-operator-helm"],
        )

        # Use local_resource to apply Grafana CR after CRDs are established
        local_resource(
            "grafana-cr-apply",
            "echo %s | kubectl apply -f -" % shlex.quote(grafana_cr_yaml),
            labels = ["observability"],
            resource_deps = grafana_deps + ["grafana-operator-ready"],
        )

        # Port-forward Grafana via local_resource
        # Wait for Grafana deployment to be ready before starting port-forward
        local_resource(
            "grafana-port-forward",
            "kubectl wait --for=condition=Available deployment/grafana-deployment -n %s --timeout=120s" % ns,
            labels = ["observability"],
            resource_deps = ["grafana-cr-apply"],
            serve_cmd = "kubectl port-forward -n %s svc/grafana-service %d:3000" % (ns, ports["grafana"]),
            links = [link("http://localhost:%d" % ports["grafana"], "Grafana")],
        )

    # GrafanaDatasource CRs - apply after Grafana CR and CRDs are ready
    if datasource_yamls:
        # Combine all datasource YAMLs
        all_ds_yaml = "\n---\n".join(datasource_yamls.values())

        # Use local_resource to apply datasources after Grafana CR is applied
        local_resource(
            "grafana-datasources-apply",
            "echo %s | kubectl apply -f -" % shlex.quote(all_ds_yaml),
            labels = ["observability"],
            resource_deps = ["grafana-cr-apply", "grafana-operator-ready"],
        )

    # VMAgent deploys when prometheus_metrics enabled (FR-023)
    if config["prometheus_metrics"] and "vmagent" in yamls:
        k8s_resource(
            "vmagent",
            new_name = _COMPONENT_RESOURCE_NAMES["vmagent"],
            labels = ["observability"],
            resource_deps = [ns_resource_name, _COMPONENT_RESOURCE_NAMES["metrics"]],
        )

    # Parca (standalone, no backend dependency - FR-024)
    if config["profiling"] and "parca" in yamls:
        k8s_resource(
            "parca-server",
            new_name = _COMPONENT_RESOURCE_NAMES["parca_server"],
            labels = ["observability"],
            port_forwards = ["%d:7070" % ports["parca"]],
            links = [link("http://localhost:%d/" % ports["parca"], "Parca UI")],
            resource_deps = [ns_resource_name],
        )
        if config["parca_agent"] and "parca-agent" in yamls:
            k8s_resource(
                "parca-agent",
                new_name = _COMPONENT_RESOURCE_NAMES["parca_agent"],
                labels = ["observability"],
                resource_deps = [_COMPONENT_RESOURCE_NAMES["parca_server"]],
            )

    # Vector auto-deploys with logs (FR-018)
    if config["logs"] and "vector" in yamls:
        k8s_resource(
            "vector",
            new_name = _COMPONENT_RESOURCE_NAMES["vector"],
            labels = ["observability"],
            resource_deps = [_COMPONENT_RESOURCE_NAMES["logs"]],
        )

    # OTel Operator deploys when otel flag enabled (FR-025)
    if config["otel"] and "otel-instrumentation" in yamls:
        # otel-operator-helm local_resource created by _deploy_otel_operator
        otel_operator_deps = []
        if config["metrics"] and "metrics" in yamls:
            otel_operator_deps.append(_COMPONENT_RESOURCE_NAMES["metrics"])
        if config["logs"] and "logs" in yamls:
            otel_operator_deps.append(_COMPONENT_RESOURCE_NAMES["logs"])
        if config["traces"] and "traces" in yamls:
            otel_operator_deps.append(_COMPONENT_RESOURCE_NAMES["traces"])

        # Deploy the Instrumentation CR — wait for operator webhook to be ready
        k8s_resource(
            objects = ["observability-auto:instrumentation"],
            new_name = "otel-instrumentation",
            labels = ["observability"],
            resource_deps = otel_operator_deps + ["otel-operator-helm"],
        )
