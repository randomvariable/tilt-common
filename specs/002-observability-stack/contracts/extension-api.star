# Starlark API Contract: Observability Stack Extension

# This file defines the public API contract for the observability Tilt extension.
# Users will load this extension in their Tiltfiles and call these functions.

# ============================================================================
# Public Functions
# ============================================================================

def enable_observability(config={}):
    """
    Deploy a complete local observability stack to the current Kubernetes cluster.

    This is the primary entry point for the extension. It deploys a configurable
    set of observability components (metrics, logs, traces, visualization,
    profiling, scraping) with pre-wired datasources and OTel-compatible endpoints.

    Args:
        config (dict): Configuration dictionary with optional keys:
            - namespace (str): Kubernetes namespace for all resources (default: 'default')
            - components (dict): Enable/disable individual components:
                - metrics (bool): VictoriaMetrics (default: True)
                - logs (bool): VictoriaLogs (default: True)
                - traces (bool): VictoriaTraces (default: True)
                - grafana (bool): Grafana with pre-provisioned datasources (default: True)
                - vmagent (bool): VMAgent for Prometheus scraping (default: False)
                - profiling (bool): Parca server + agent (default: False)
                - otel_operator (bool): OpenTelemetry Operator for webhook-based
                    OTLP env var injection into annotated pods (default: False).
                    Requires at least one signal backend (metrics, logs, or traces).
                    When enabled, creates an Instrumentation CR that configures the
                    same OTLP endpoints as otel_env().
            - ports (dict): Host port-forward assignments:
                - metrics (int): VictoriaMetrics port (default: 8428)
                - logs (int): VictoriaLogs port (default: 9428)
                - traces (int): VictoriaTraces port (default: 10428)
                - grafana (int): Grafana port (default: 3000)
                - parca (int): Parca server port (default: 7070)
            - images (dict): Container image overrides (keyed by component name):
                - metrics (str): VictoriaMetrics image
                - logs (str): VictoriaLogs image
                - traces (str): VictoriaTraces image
                - grafana (str): Grafana image
                - parca_server (str): Parca server image
                - parca_agent (str): Parca agent image
                - vmagent (str): VMAgent image
                - otel_operator (str): OpenTelemetry Operator image
            - scrape_targets (list): Static Prometheus scrape targets for VMAgent
                (e.g., ['my-service:8080', 'other-service:9090'])
            - dashboard_paths (list): Paths to Grafana dashboard JSON files
                (e.g., ['./dashboards/overview.json', './dashboards/api.json'])
            - export_dir (str): Output directory for telemetry export (default: './observability-export')

    Returns:
        None (side effects: registers Tilt resources under 'observability' label)

    Raises:
        fail(): If configuration invalid or dependency constraints violated:
            - Port values outside 1-65535
            - vmagent enabled without metrics
            - otel_operator enabled without any signal backend (metrics/logs/traces)
            - Unrecognized configuration keys
            - Invalid namespace format

    Example:
        # Basic usage -- deploys metrics, logs, traces, and Grafana
        load('ext://tilt-common/observability', 'enable_observability')
        enable_observability()

        # Custom configuration
        enable_observability({
            'namespace': 'monitoring',
            'components': {
                'vmagent': True,
                'profiling': True,
            },
            'ports': {
                'grafana': 3001,  # Avoid conflict with frontend
            },
            'scrape_targets': ['my-app:8080'],
            'dashboard_paths': ['./dashboards/my-dashboard.json'],
        })
    """
    pass  # Implementation in extension.star


def otel_env(service_name="", config={}):
    """
    Return OpenTelemetry environment variable definitions for application instrumentation.

    Returns a list of Tilt-compatible environment variable definitions that configure
    OTel SDK exporters to send telemetry to the observability stack. Only includes
    variables for backends that are enabled.

    Args:
        service_name (str): Value for OTEL_SERVICE_NAME (optional, omitted if empty)
        config (dict): Same config dict passed to enable_observability() (optional).
            Used to determine which backends are enabled and the namespace.
            If omitted, assumes all core components are enabled in 'default' namespace.

    Returns:
        list[str]: List of 'KEY=VALUE' strings suitable for container env injection.
            Example: [
                'OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://victoriametrics.default.svc.cluster.local:8428/opentelemetry/api/v1/push',
                'OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf',
                'OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://victorialogs.default.svc.cluster.local:9428/insert/opentelemetry/v1/logs',
                'OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf',
                'OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://victoriatraces.default.svc.cluster.local:4317',
                'OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=grpc',
                'OTEL_SERVICE_NAME=my-service',
            ]

    Example:
        load('ext://tilt-common/observability', 'enable_observability', 'otel_env')

        enable_observability()

        # Get OTel env vars for an application
        otel_vars = otel_env(service_name='my-app')
        # Returns list of 'KEY=VALUE' strings to inject into your container spec
        # e.g., via env section in your Kubernetes YAML or Tiltfile helpers

        # Example: use in a custom YAML builder or print for debugging
        print('OTel env vars:', otel_vars)
    """
    pass  # Implementation in extension.star


def export_telemetry(config={}):
    """
    Register a Tilt button and local_resource for exporting telemetry data.

    Creates a Tilt button in the dashboard that, when clicked, exports metrics
    and traces from their respective backends to a DuckDB database file, and
    downloads pprof profiles if profiling is enabled.

    The export runs the observability-export CLI tool which must be built
    from cmd/observability-export/.

    Args:
        config (dict): Same config dict passed to enable_observability().
            Used to determine export_dir and which backends to export from.

    Returns:
        None (side effects: registers a Tilt local_resource with a button trigger)

    Example:
        load('ext://tilt-common/observability', 'enable_observability', 'export_telemetry')

        enable_observability()
        export_telemetry()  # Adds 'Export Telemetry' button to Tilt dashboard
    """
    pass  # Implementation in extension.star


# ============================================================================
# Internal Functions (Not Part of Public API)
# ============================================================================

def _validate_config(config):
    """
    Validate user configuration and apply defaults.

    Args:
        config (dict): User-provided config

    Returns:
        dict: Validated config with defaults applied

    Raises:
        fail(): If configuration invalid

    Internal use only - not exposed to users.
    """
    pass


def _generate_component_yaml(component_id, config):
    """
    Generate Kubernetes YAML for a single observability component.

    Reads the asset template from extensions/observability/assets/ and
    substitutes placeholders with config values.

    Args:
        component_id (str): One of 'metrics', 'logs', 'traces', 'grafana',
            'parca', 'vmagent'
        config (dict): Validated configuration

    Returns:
        str: Kubernetes YAML content

    Internal use only - not exposed to users.
    """
    pass


def _generate_datasource_config(config):
    """
    Generate Grafana datasource provisioning ConfigMap YAML.

    Only includes datasources for enabled backends.

    Args:
        config (dict): Validated configuration

    Returns:
        str: ConfigMap YAML content

    Internal use only - not exposed to users.
    """
    pass


def _generate_dashboard_config(config):
    """
    Generate Grafana dashboard provisioning ConfigMaps.

    Creates both the provider ConfigMap and the dashboard JSON ConfigMap
    from user-provided dashboard file paths.

    Args:
        config (dict): Validated configuration

    Returns:
        str: Combined YAML content (provider + dashboards ConfigMaps)
        None: If no dashboards configured

    Internal use only - not exposed to users.
    """
    pass


def _generate_vmagent_scrape_config(config):
    """
    Generate VMAgent scrape configuration ConfigMap YAML.

    Includes kubernetes_sd_configs for annotation-based discovery
    and optional static_configs from user-provided scrape targets.

    Args:
        config (dict): Validated configuration

    Returns:
        str: ConfigMap YAML content

    Internal use only - not exposed to users.
    """
    pass


def _detect_containerd_socket(config):
    """
    Detect containerd socket path inside the cluster node.

    Probes known paths in order: k3s -> microk8s -> standard containerd.

    Args:
        config (dict): Validated configuration

    Returns:
        str: Socket path on the node

    Raises:
        fail(): If no containerd socket found

    Internal use only - not exposed to users.
    """
    pass


def _deploy_to_tilt(config, yamls):
    """
    Register all generated YAML and resources with Tilt.

    Handles k8s_yaml(), k8s_resource() with labels, resource_deps,
    and port_forwards for all enabled components.

    Args:
        config (dict): Validated configuration
        yamls (dict): Component ID -> YAML string mapping

    Returns:
        None (side effects: Tilt resource registration)

    Internal use only - not exposed to users.
    """
    pass


# ============================================================================
# Configuration Schema (Documentation)
# ============================================================================

CONFIG_SCHEMA = {
    'namespace': {
        'type': 'string',
        'default': 'default',
        'description': 'Kubernetes namespace for all observability resources',
        'validation': 'Must be valid RFC 1123 DNS label',
    },
    'components': {
        'type': 'dict',
        'default': {
            'metrics': True,
            'logs': True,
            'traces': True,
            'grafana': True,
            'vmagent': False,
            'profiling': False,
            'otel_operator': False,
        },
        'description': 'Enable/disable individual observability components',
        'validation': 'vmagent requires metrics; profiling can run standalone; otel_operator requires at least one signal backend',
    },
    'ports': {
        'type': 'dict',
        'default': {
            'metrics': 8428,
            'logs': 9428,
            'traces': 10428,
            'grafana': 3000,
            'parca': 7070,
        },
        'description': 'Host port-forward assignments per component',
        'validation': 'Each port must be integer 1-65535',
    },
    'images': {
        'type': 'dict',
        'default': {},
        'description': 'Container image overrides per component',
        'validation': 'Non-empty string values when provided',
    },
    'scrape_targets': {
        'type': 'list',
        'default': [],
        'description': 'Static Prometheus scrape targets for VMAgent (host:port)',
        'validation': 'List of non-empty strings',
    },
    'dashboard_paths': {
        'type': 'list',
        'default': [],
        'description': 'Paths to Grafana dashboard JSON files for provisioning',
        'validation': 'List of readable file paths',
    },
    'export_dir': {
        'type': 'string',
        'default': './observability-export',
        'description': 'Output directory for telemetry export files',
        'validation': 'Must be a writable directory path',
    },
}

# ============================================================================
# Default Images (Documentation)
# ============================================================================

DEFAULT_IMAGES = {
    'metrics': 'victoriametrics/victoria-metrics:latest',
    'logs': 'victoriametrics/victoria-logs:latest',
    'traces': 'victoriametrics/victoria-traces:latest',
    'grafana': 'grafana/grafana:latest',
    'parca_server': 'ghcr.io/parca-dev/parca:latest',
    'parca_agent': 'ghcr.io/parca-dev/parca-agent:latest',
    'vmagent': 'victoriametrics/vmagent:latest',
    'otel_operator': 'ghcr.io/open-telemetry/opentelemetry-operator/opentelemetry-operator:0.116.0',
}
