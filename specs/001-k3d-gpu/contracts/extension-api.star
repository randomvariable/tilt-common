# Starlark API Contract: k3s GPU Support Extension

# This file defines the public API contract for the k3d-gpu Tilt extension.
# Users will load this extension in their Tiltfiles and call these functions.

# ============================================================================
# Public Functions
# ============================================================================

def enable_gpu_support(config={}):
    """
    Enable NVIDIA GPU support in a local k3s cluster.

    This is the primary entry point for the extension. It configures GPU access
    by deploying the NVIDIA device plugin, generating CDI specs (on WSL2),
    fixing DNS resolution, and mounting kernel filesystems for profiling.

    Args:
        config (dict): Configuration dictionary with optional keys:
            - cluster_name (str): k3d cluster name (default: auto-detect)
            - namespace (str): Namespace for device plugin (default: 'kube-system')
            - enable_profiling (bool): Mount debugfs/tracefs (default: true)
            - device_plugin_image (str): Device plugin image (default: official NVIDIA image)
            - timesharing_replicas (int): GPU timesharing factor (default: 4, allows 4 pods per GPU)
            - skip_dns_fix (bool): Skip DNS reconfiguration (default: false)
            - skip_validation (bool): Skip prerequisite checks (default: false)

    Returns:
        None (side effects: registers Tilt resources)

    Raises:
        fail(): If prerequisites missing or configuration invalid

    Example:
        # Basic usage (all defaults)
        load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')
        enable_gpu_support()

        # Custom configuration
        enable_gpu_support({
            'cluster_name': 'k3d-myapp',
            'namespace': 'gpu-system',
            'device_plugin_image': 'nvcr.io/nvidia/k8s-device-plugin:v0.15.0',
            'timesharing_replicas': 4,  # Default: 4 pods can share each physical GPU
        })

    Environment Variables:
        - TILT_GPU_ENABLED: Set to 'true' to enable (overrides Tiltfile call)
        - TILT_GPU_NAMESPACE: Override namespace
        - TILT_GPU_SKIP_VALIDATION: Set to 'true' to skip validation
    """
    pass  # Implementation in extension.star


def configure_gpu_resources(resource_name, gpu_count=1, gpu_memory=None):
    """
    Configure GPU resource requests/limits for a Tilt resource.

    Helper function to add GPU resource constraints to existing Tilt resources.
    Useful for modifying deployments to request GPUs without editing YAML.

    Args:
        resource_name (str): Name of the Tilt resource to modify
        gpu_count (int): Number of GPUs to request (default: 1)
        gpu_memory (str): Optional GPU memory limit (e.g., '8Gi', not widely supported)

    Returns:
        None (side effects: modifies Tilt resource configuration)

    Raises:
        fail(): If resource not found or GPU count invalid

    Example:
        # Request 1 GPU for 'embedding-server' resource
        configure_gpu_resources('embedding-server', gpu_count=1)

        # Request 2 GPUs
        configure_gpu_resources('training-job', gpu_count=2)

    Notes:
        - Must be called after the resource is defined
        - Adds resources.limits.nvidia.com/gpu to the workload
        - GPU count must be positive integer
        - GPU memory limits are not supported by most platforms (ignored)
    """
    pass  # Implementation in extension.star


# ============================================================================
# Internal Functions (Not Part of Public API)
# ============================================================================

def _detect_system_environment():
    """
    Detect host system capabilities (WSL2, nvidia-ctk, GPU UUID).

    Returns:
        dict: SystemEnvironment structure (see data-model.md)

    Internal use only - not exposed to users.
    """
    pass


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


def _generate_cdi_spec(env):
    """
    Generate CDI spec YAML for WSL2 GPU access.

    Args:
        env (dict): SystemEnvironment from _detect_system_environment()

    Returns:
        str: CDI spec YAML content

    Internal use only - not exposed to users.
    """
    pass


def _generate_device_plugin_yaml(config):
    """
    Generate NVIDIA device plugin DaemonSet manifest.

    Args:
        config (dict): Validated configuration

    Returns:
        str: Device plugin YAML content

    Internal use only - not exposed to users.
    """
    pass


def _generate_k3s_wrapper_script(config, env):
    """
    Generate k3s wrapper script that applies fixes before k3s starts.

    Args:
        config (dict): Validated configuration
        env (dict): SystemEnvironment

    Returns:
        str: Bash script content

    Internal use only - not exposed to users.
    """
    pass


def _deploy_to_tilt(config, device_plugin_yaml, wrapper_script):
    """
    Register resources with Tilt (k8s_yaml, local_resource).

    Args:
        config (dict): Validated configuration
        device_plugin_yaml (str): Device plugin manifest
        wrapper_script (str): k3s wrapper script

    Returns:
        None (side effects: Tilt resource registration)

    Internal use only - not exposed to users.
    """
    pass


# ============================================================================
# Configuration Schema (Documentation)
# ============================================================================

# This is the complete configuration schema for reference.
# Actual validation happens in _validate_config().

CONFIG_SCHEMA = {
    'cluster_name': {
        'type': 'string',
        'default': None,  # Auto-detect first k3d cluster
        'description': 'k3d cluster name (e.g., "k3d-myapp")',
        'validation': 'Must match existing k3d cluster or be empty',
    },
    'namespace': {
        'type': 'string',
        'default': 'kube-system',
        'description': 'Kubernetes namespace for device plugin',
        'validation': 'Must be valid RFC 1123 DNS label',
    },
    'enable_profiling': {
        'type': 'bool',
        'default': True,
        'description': 'Mount debugfs/tracefs for eBPF profiling tools',
        'validation': 'Must be true or false',
    },
    'force_wsl2_mode': {
        'type': 'bool',
        'default': False,
        'description': 'Force WSL2 CDI mode even if /dev/dxg missing',
        'validation': 'Must be true or false',
    },
    'device_plugin_image': {
        'type': 'string',
        'default': 'nvcr.io/nvidia/k8s-device-plugin:v0.14.3',
        'description': 'NVIDIA device plugin container image',
        'validation': 'Must be valid container image reference',
    },
    'device_plugin_labels': {
        'type': 'dict',
        'default': {},
        'description': 'Additional labels for device plugin DaemonSet',
        'validation': 'Must be string key-value pairs',
    },
    'timesharing_replicas': {
        'type': 'int',
        'default': 4,
        'description': 'GPU timesharing factor - report N replicas per physical GPU',
        'validation': 'Must be positive integer 1-100 (1=no sharing, 4=4 pods per GPU)',
    },
    'dns_nameservers': {
        'type': 'list',
        'default': ['127.0.0.11', '9.9.9.9', '1.1.1.1'],
        'description': 'Custom DNS nameservers for k3s nodes',
        'validation': 'Must be list of valid IP addresses',
    },
    'skip_dns_fix': {
        'type': 'bool',
        'default': False,
        'description': 'Skip DNS reconfiguration (if already correct)',
        'validation': 'Must be true or false',
    },
    'skip_validation': {
        'type': 'bool',
        'default': False,
        'description': 'Skip prerequisite checks (nvidia-ctk, drivers)',
        'validation': 'Must be true or false',
    },
}

# ============================================================================
# Environment Variables (Documentation)
# ============================================================================

ENV_VARS = {
    'TILT_GPU_ENABLED': 'Set to "true" to enable GPU support (overrides function call)',
    'TILT_GPU_NAMESPACE': 'Override namespace for device plugin',
    'TILT_GPU_SKIP_VALIDATION': 'Set to "true" to skip prerequisite checks',
}
