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

# k3s GPU Support Extension
# Enables NVIDIA GPU support in local k3s clusters with WSL2 and native Linux compatibility

# ============================================================================
# Public API
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
            - enable_profiling (bool): Mount debugfs/tracefs (default: True)
            - device_plugin_image (str): Device plugin image (default: official NVIDIA image)
            - timesharing_replicas (int): GPU timesharing factor (default: 4)
            - skip_dns_fix (bool): Skip DNS reconfiguration (default: False)
            - skip_validation (bool): Skip prerequisite checks (default: False)

    Returns:
        None (side effects: registers Tilt resources)

    Raises:
        fail(): If prerequisites missing or configuration invalid
    """
    print("k3d-gpu extension loading...")

    # Step 1: Validate configuration and apply defaults
    validated_config = _validate_config(config)

    # Step 2: Detect system environment
    env = _detect_system_environment()

    # Step 3: Validate prerequisites (unless skipped)
    if not validated_config["skip_validation"]:
        if not env["has_nvidia_ctk"]:
            fail("nvidia-ctk not found. Please install nvidia-container-toolkit. See README.md for instructions.")

    # Step 4: Generate device plugin YAML
    device_plugin_yaml = _generate_device_plugin_yaml(validated_config)

    # Step 5: Generate k3s wrapper script (for reference, manual deployment required)
    wrapper_script = _generate_k3s_wrapper_script(validated_config, env)

    # Step 6: Deploy resources to Tilt
    _deploy_to_tilt(validated_config, device_plugin_yaml, wrapper_script)

    print("k3d-gpu extension loaded successfully")
    print("Timesharing enabled: %d pods can share each physical GPU" % validated_config["timesharing_replicas"])
    if env["is_wsl2"]:
        print("WSL2 detected: CDI mode will be used for GPU access")
    else:
        print("Native Linux detected: Auto/legacy mode will be used for GPU access")


# ============================================================================
# Internal Functions
# ============================================================================

def _detect_system_environment():
    """
    Detect host system capabilities (WSL2, nvidia-ctk, GPU UUID).

    Returns:
        dict: SystemEnvironment structure with keys:
            - is_wsl2 (bool): True if running on WSL2
            - has_nvidia_ctk (bool): True if nvidia-ctk command available
            - gpu_uuid (str): GPU UUID from nvidia-container-cli
            - driver_store (str): Path to WSL2 driver store
            - libdxcore_path (str): Path to libdxcore.so (WSL2 only)

    Internal use only - not exposed to users.
    """
    env = {
        "is_wsl2": False,
        "has_nvidia_ctk": False,
        "gpu_uuid": None,
        "driver_store": None,
        "libdxcore_path": None,
    }

    # Detect WSL2 by checking for /dev/dxg device
    wsl2_check = local("test -e /dev/dxg && echo 'true' || echo 'false'", quiet=True, echo_off=True)
    env["is_wsl2"] = wsl2_check.strip() == "true"

    # Check nvidia-ctk availability
    nvidia_ctk_check = local("command -v nvidia-ctk >/dev/null 2>&1 && echo 'true' || echo 'false'", quiet=True, echo_off=True)
    env["has_nvidia_ctk"] = nvidia_ctk_check.strip() == "true"

    # Extract GPU UUID (if nvidia-container-cli available)
    if env["has_nvidia_ctk"]:
        gpu_info = local("nvidia-container-cli info 2>/dev/null | grep 'Device Index' -A 5 | grep 'UUID:' | head -1 | awk '{print $2}' || echo ''", quiet=True, echo_off=True)
        if gpu_info.strip():
            env["gpu_uuid"] = gpu_info.strip()

    # Detect WSL2-specific paths
    if env["is_wsl2"]:
        # Find driver store path
        driver_store = local("find /usr/lib/wsl/drivers -type d -name 'nv_dispi*' 2>/dev/null | head -1 || echo ''", quiet=True, echo_off=True)
        if driver_store.strip():
            env["driver_store"] = driver_store.strip()

        # Find libdxcore.so path
        libdxcore = local("find /usr/lib -name 'libdxcore.so*' 2>/dev/null | head -1 || echo ''", quiet=True, echo_off=True)
        if libdxcore.strip():
            env["libdxcore_path"] = libdxcore.strip()

    return env


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
    # Default configuration
    defaults = {
        "cluster_name": None,  # Auto-detect
        "namespace": "kube-system",
        "enable_profiling": True,
        "device_plugin_image": "nvcr.io/nvidia/k8s-device-plugin:v0.14.3",
        "timesharing_replicas": 4,
        "skip_dns_fix": False,
        "skip_validation": False,
    }

    # Merge user config with defaults
    merged = dict(defaults)
    for key in config:
        merged[key] = config[key]

    # Validate timesharing_replicas (must be 1-100)
    replicas = merged["timesharing_replicas"]
    if type(replicas) != "int":
        fail("timesharing_replicas must be an integer, got: %s" % type(replicas))
    if replicas < 1 or replicas > 100:
        fail("timesharing_replicas must be between 1 and 100, got: %d" % replicas)

    # Validate namespace (RFC 1123 DNS label: lowercase alphanumeric + hyphens)
    namespace = merged["namespace"]
    if type(namespace) != "string":
        fail("namespace must be a string, got: %s" % type(namespace))
    if not namespace:
        fail("namespace cannot be empty")
    # Check for invalid characters (basic validation - Starlark doesn't have full regex)
    if namespace != namespace.lower():
        fail("namespace must be lowercase, got: %s" % namespace)
    if " " in namespace or "." in namespace or "_" in namespace:
        fail("namespace must not contain spaces, dots, or underscores: %s" % namespace)

    # Validate device_plugin_image (basic check - not empty)
    image = merged["device_plugin_image"]
    if type(image) != "string":
        fail("device_plugin_image must be a string, got: %s" % type(image))
    if not image:
        fail("device_plugin_image cannot be empty")

    return merged


def _generate_device_plugin_yaml(config):
    """
    Generate NVIDIA device plugin DaemonSet manifest.

    Args:
        config (dict): Validated configuration

    Returns:
        str: Device plugin YAML content

    Internal use only - not exposed to users.
    """
    # Read the device plugin template
    template_path = "./extensions/k3d-gpu/assets/device-plugin.yaml"
    template = read_file(template_path)

    # Replace placeholders with actual configuration values
    yaml = template.replace("{NAMESPACE}", config["namespace"])
    yaml = yaml.replace("{DEVICE_PLUGIN_IMAGE}", config["device_plugin_image"])
    yaml = yaml.replace("{TIMESHARING_REPLICAS}", str(config["timesharing_replicas"]))

    return yaml


def _generate_k3s_wrapper_script(config, env):
    """
    Generate k3s wrapper script that applies fixes before k3s starts.

    Args:
        config (dict): Validated configuration
        env (dict): SystemEnvironment from _detect_system_environment()

    Returns:
        str: Bash script content

    Internal use only - not exposed to users.
    """
    # Read the static wrapper script template
    # The wrapper script handles all GPU setup tasks:
    # - DNS fix (controlled by skip_dns_fix config in future enhancement)
    # - eBPF filesystem mounting (controlled by enable_profiling config in future enhancement)
    # - CDI spec generation for WSL2
    # - nvidia-container-runtime configuration

    wrapper_path = "./extensions/k3d-gpu/assets/k3s-wrapper.sh"
    wrapper_script = read_file(wrapper_path)

    return wrapper_script


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
    # Register device plugin YAML with Tilt
    k8s_yaml(blob(device_plugin_yaml))

    # Label the device plugin resource for organization
    k8s_resource(
        "nvidia-device-plugin-daemonset",
        labels=["k3d-gpu"],
        resource_deps=[],
    )

    # Note: k3s wrapper script is deployed at cluster creation time via custom image build.
    # See extensions/k3d-gpu/assets/cluster/Dockerfile and scripts/create-gpu-cluster.sh
    # The wrapper is baked into /bin/k3s in the custom k3s image.

    print("GPU support enabled: Device plugin deployed to namespace '%s'" % config["namespace"])
