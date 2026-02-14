# Data Model: k3s GPU Support Extension

**Date**: 2026-02-14
**Purpose**: Define configuration structures and data entities for the extension

## Configuration Entities

### 1. GPUConfig (Primary Configuration Dictionary)

**Purpose**: User-facing configuration passed to `enable_gpu_support()` function

**Structure**:
```python
{
    # Cluster identification
    'cluster_name': str,           # k3d cluster name (default: auto-detect first k3d cluster)
    'namespace': str,              # Namespace for device plugin (default: 'kube-system')

    # Feature toggles
    'enable_profiling': bool,      # Mount debugfs/tracefs for eBPF tools (default: true)
    'force_wsl2_mode': bool,       # Force WSL2 CDI mode even if /dev/dxg missing (default: false)

    # Device plugin configuration
    'device_plugin_image': str,    # NVIDIA device plugin image (default: 'nvcr.io/nvidia/k8s-device-plugin:v0.14.3')
    'device_plugin_labels': dict,  # Additional labels for device plugin DaemonSet (default: {})
    'timesharing_replicas': int,   # GPU timesharing factor - report N replicas per physical GPU (default: 4)

    # DNS configuration
    'dns_nameservers': list,       # Custom nameservers (default: ['127.0.0.11', '9.9.9.9', '1.1.1.1'])
    'skip_dns_fix': bool,          # Skip DNS reconfiguration (default: false)

    # Validation
    'skip_validation': bool,       # Skip prerequisite checks (default: false)
}
```

**Validation Rules**:
- `cluster_name`: Must match existing k3d cluster or be empty (auto-detect)
- `namespace`: Must be valid Kubernetes namespace name (RFC 1123)
- `device_plugin_image`: Must be valid container image reference
- `dns_nameservers`: List of valid IP addresses or 'localhost' addresses
- `timesharing_replicas`: Must be positive integer (1-100), where 1 = no sharing, 4 = 4 pods per GPU

**Default Behavior** (empty dict):
```python
enable_gpu_support()  # Uses all defaults, works on WSL2 + native Linux
```

### 2. SystemEnvironment (Detected Environment)

**Purpose**: Runtime detection of host system capabilities

**Structure**:
```python
{
    'is_wsl2': bool,               # True if /dev/dxg exists
    'nvidia_ctk_available': bool,  # True if nvidia-ctk command found
    'nvidia_cli_available': bool,  # True if nvidia-container-cli command found
    'driver_store_path': str,      # WSL driver store path (e.g., '/usr/lib/wsl/drivers/nv_dispi...')
    'libdxcore_path': str,         # Path to libdxcore.so (WSL2 only)
    'gpu_uuid': str,               # GPU UUID from nvidia-container-cli info
    'k3s_nodes': list,             # List of k3s node names in cluster
}
```

**Detection Logic**:
- `is_wsl2`: Check `os.path.exists('/dev/dxg')`
- `nvidia_ctk_available`: Check `which nvidia-ctk`
- `driver_store_path`: Find via `find /usr/lib/wsl/drivers -name "nv_dispi*"`
- `gpu_uuid`: Parse output of `nvidia-container-cli info`

**Error States**:
- Missing `nvidia_ctk_available` → Fail with installation instructions
- Missing `nvidia_cli_available` → Fail with installation instructions
- `is_wsl2=true` but `libdxcore_path` not found → Warn but attempt fallback

### 3. CDISpec (Container Device Interface Specification)

**Purpose**: Structure of CDI YAML file generated for WSL2 GPU access

**Structure**:
```yaml
cdiVersion: "0.5.0"
kind: "nvidia.com/gpu"
devices:
  - name: "all"                    # Catch-all device
    containerEdits:
      deviceNodes:
        - path: /dev/dxg
  - name: "0"                      # Device by index
    containerEdits:
      deviceNodes:
        - path: /dev/dxg
  - name: "${GPU_UUID}"            # Device by UUID (for k8s scheduler)
    containerEdits:
      deviceNodes:
        - path: /dev/dxg
containerEdits:
  env:
    - "NVIDIA_VISIBLE_DEVICES=void"
  hooks:
    - hookName: createContainer
      path: /usr/bin/nvidia-cdi-hook
      args:
        - nvidia-cdi-hook
        - create-symlinks
        - --link
        - "${DRIVER_STORE}/nvidia-smi::/usr/bin/nvidia-smi"
    - hookName: createContainer
      path: /usr/bin/nvidia-cdi-hook
      args:
        - nvidia-cdi-hook
        - update-ldcache
        - --folder
        - "${DRIVER_STORE}"
        - --folder
        - "${LIBDXCORE_DIR}"
  mounts:
    - hostPath: /dev/dxg
      containerPath: /dev/dxg
      options: [rw, nosuid, nodev, rbind, rprivate]
    - hostPath: "${DRIVER_STORE}"
      containerPath: "${DRIVER_STORE}"
      options: [ro, nosuid, nodev, rbind, rprivate]
    - hostPath: "${LIBDXCORE_PATH}"
      containerPath: "${LIBDXCORE_PATH}"
      options: [ro, nosuid, nodev, rbind, rprivate]
```

**Template Variables**:
- `${GPU_UUID}`: Detected GPU UUID (e.g., "GPU-12345678-1234-1234-1234-123456789abc")
- `${DRIVER_STORE}`: WSL driver store directory path
- `${LIBDXCORE_PATH}`: Full path to libdxcore.so
- `${LIBDXCORE_DIR}`: Directory containing libdxcore.so (for ldcache)

**Generation Rules**:
- Only generated on WSL2 (`is_wsl2=true`)
- Must include all driver store files (*.so, nvidia-smi)
- Must include libdxcore.so explicitly (missed by nvidia-ctk)
- Device names must match k8s device plugin expectations

### 4. DevicePluginManifest (Kubernetes DaemonSet)

**Purpose**: NVIDIA device plugin deployment configuration

**Structure**:
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: nvidia-device-plugin-daemonset
  namespace: "${NAMESPACE}"
  labels:
    app: nvidia-device-plugin
    tilt.dev/resource: gpu-support
spec:
  selector:
    matchLabels:
      name: nvidia-device-plugin-ds
  updateStrategy:
    type: RollingUpdate
  template:
    metadata:
      labels:
        name: nvidia-device-plugin-ds
    spec:
      tolerations:
      - key: nvidia.com/gpu
        operator: Exists
        effect: NoSchedule
      priorityClassName: system-node-critical
      containers:
      - image: "${DEVICE_PLUGIN_IMAGE}"
        name: nvidia-device-plugin-ctr
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop: ["ALL"]
          privileged: true
        volumeMounts:
        - name: device-plugin
          mountPath: /var/lib/kubelet/device-plugins
      volumes:
      - name: device-plugin
        hostPath:
          path: /var/lib/kubelet/device-plugins
```

**Template Variables**:
- `${NAMESPACE}`: Target namespace (default: kube-system)
- `${DEVICE_PLUGIN_IMAGE}`: Container image reference

**Customization Points**:
- Additional labels via `device_plugin_labels` config
- Image version via `device_plugin_image` config
- Resource limits (optional, not set by default for device plugin)

## Data Flows

### 1. Configuration Flow

```
User Tiltfile
    ↓
enable_gpu_support({'cluster_name': 'k3d-myapp'})
    ↓
Validate Config (cluster exists, namespace valid)
    ↓
Detect System Environment (WSL2, nvidia-ctk, GPU UUID)
    ↓
Generate CDI Spec (WSL2 only) OR Skip (native Linux)
    ↓
Generate Device Plugin Manifest
    ↓
Generate k3s Wrapper Script
    ↓
Deploy to Tilt (k8s_yaml, local_resource)
```

### 2. Runtime Initialization Flow (Inside k3s Node)

```
k3s Node Container Starts
    ↓
k3s-wrapper.sh Executes (instead of k3s binary)
    ↓
Fix DNS (/etc/resolv.conf)
    ↓
Mount debugfs + tracefs (if enable_profiling=true)
    ↓
Detect WSL2 (/dev/dxg exists?)
    ↓
[WSL2 Path] Generate CDI Spec → /etc/cdi/nvidia.yaml
    ↓
[WSL2 Path] Set nvidia-container-runtime mode=cdi
    ↓
[Native Linux Path] Leave runtime mode=auto
    ↓
exec /bin/k3s.real "$@" (Start actual k3s process)
```

### 3. GPU Workload Scheduling Flow

```
User Deploys Pod with nvidia.com/gpu=1
    ↓
Kubernetes Scheduler Queries Available GPUs (from device plugin)
    ↓
Scheduler Assigns Pod to Node with GPU
    ↓
kubelet Calls containerd to Create Container
    ↓
containerd Invokes nvidia-container-runtime
    ↓
[WSL2] Runtime Reads /etc/cdi/nvidia.yaml
    ↓
[WSL2] Runtime Injects /dev/dxg + Driver Store + libdxcore.so
    ↓
[Native Linux] Runtime Injects /dev/nvidia* devices
    ↓
Container Starts with GPU Access
```

## State Management

**No Persistent State**: Extension is stateless and idempotent.

**Temporary State** (created at runtime, destroyed on cluster teardown):
- `/etc/cdi/nvidia.yaml` (in k3s node containers)
- `/etc/resolv.conf` (overwritten in k3s node containers)
- Device plugin DaemonSet (in Kubernetes cluster)

**Idempotency**:
- Re-running `enable_gpu_support()` regenerates all resources
- Safe to call multiple times (no side effects)
- Cluster teardown removes all generated state

## Validation Rules Summary

| Field | Validation | Error Message |
|-------|------------|---------------|
| cluster_name | Must exist in k3d OR be empty | "Cluster 'foo' not found. Run: k3d cluster list" |
| namespace | RFC 1123 DNS label | "Invalid namespace 'Foo_Bar'. Must be lowercase alphanumeric with hyphens." |
| device_plugin_image | Valid image reference | "Invalid image 'foo:bar:baz'. Must be registry/name:tag format." |
| dns_nameservers | List of valid IPs | "Invalid nameserver '999.999.999.999'. Must be valid IP address." |
| timesharing_replicas | Integer 1-100 | "Invalid timesharing_replicas '0'. Must be 1-100 (1=no sharing, 4=default)." |
| nvidia-ctk presence | Command available | "nvidia-ctk not found. Install nvidia-container-toolkit: ..." |
| GPU detection | At least one GPU found | "No GPUs detected. Verify NVIDIA drivers installed: nvidia-smi" |
