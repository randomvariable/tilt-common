# Quick Start: k3s GPU Support Extension

**Date**: 2026-02-14
**Audience**: Developers adding GPU support to local k3s development

## Prerequisites

Before using this extension, ensure you have:

1. **NVIDIA GPU** with drivers installed
   ```bash
   nvidia-smi  # Should show GPU info, not "command not found"
   ```

2. **nvidia-container-toolkit** installed
   ```bash
   # Arch Linux
   sudo pacman -S nvidia-container-toolkit

   # Ubuntu/Debian
   distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
   curl -s -L https://nvidia.github.io/libnvidia-container/gpgkey | sudo apt-key add -
   curl -s -L https://nvidia.github.io/libnvidia-container/$distribution/libnvidia-container.list | \
     sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
   sudo apt-get update && sudo apt-get install -y nvidia-container-toolkit
   ```

3. **k3d** or **k3s** cluster with containerd runtime
   ```bash
   k3d cluster list  # Should show your cluster
   ```

4. **Tilt 0.30+**
   ```bash
   tilt version  # Should be >= v0.30.0
   ```

## Basic Usage (Zero Configuration)

Add one line to your Tiltfile:

```python
# Tiltfile
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')

enable_gpu_support()  # Uses all defaults

# Your existing Tilt resources...
k8s_yaml('deployment.yaml')
```

Run `tilt up` and GPU support will be configured automatically.

## Test GPU Access

Deploy a test pod to verify GPU is accessible:

```yaml
# test-gpu.yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-test
spec:
  containers:
  - name: cuda-test
    image: nvidia/cuda:12.0.0-base-ubuntu22.04
    command: ["nvidia-smi"]
    resources:
      limits:
        nvidia.com/gpu: 1
  restartPolicy: Never
```

```bash
kubectl apply -f test-gpu.yaml
kubectl logs gpu-test  # Should show GPU information
```

## Advanced Configuration

### Custom Cluster and Namespace

```python
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')

enable_gpu_support({
    'cluster_name': 'k3d-myapp',
    'namespace': 'gpu-system',  # Deploy device plugin here instead of kube-system
})
```

### Disable Profiling Support

If you don't need eBPF profiling (saves a small amount of setup time):

```python
enable_gpu_support({
    'enable_profiling': False,  # Don't mount debugfs/tracefs
})
```

### Custom Device Plugin Image

Use a specific version or custom build:

```python
enable_gpu_support({
    'device_plugin_image': 'nvcr.io/nvidia/k8s-device-plugin:v0.15.0',
})
```

### GPU Timesharing (Default: Enabled)

By default, the extension enables GPU timesharing with a factor of 4, allowing 4 pods to share each physical GPU. This is ideal for development where workloads don't fully utilize the GPU:

```python
# Default behavior (4x timesharing)
enable_gpu_support()  # Reports 4 GPUs if you have 1 physical GPU

# Disable timesharing (1:1 mapping)
enable_gpu_support({
    'timesharing_replicas': 1,  # Each pod gets exclusive GPU access
})

# More aggressive sharing for lightweight workloads
enable_gpu_support({
    'timesharing_replicas': 8,  # 8 pods can share each GPU
})
```

**Why timesharing?** Local development workloads (inference, small training jobs) rarely use 100% of GPU resources. Timesharing allows multiple services to run simultaneously on one GPU, matching the reality that most pods are idle or use <25% GPU capacity.

### Skip DNS Fix

If your Docker version doesn't have the DNS issue (pre-29) or DNS is already configured:

```python
enable_gpu_support({
    'skip_dns_fix': True,
})
```

## Environment Variable Configuration

For CI/automation, use environment variables instead of Tiltfile edits:

```bash
# Enable GPU support
export TILT_GPU_ENABLED=true

# Custom namespace
export TILT_GPU_NAMESPACE=gpu-system

# Skip validation (for testing)
export TILT_GPU_SKIP_VALIDATION=true

tilt up
```

In your Tiltfile, the extension checks these variables automatically:

```python
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')

# Environment variables are checked even without config dict
enable_gpu_support()
```

## Helper: Configure GPU Resources for Existing Workloads

If you have existing deployments that need GPU access, use the helper:

```python
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support', 'configure_gpu_resources')

enable_gpu_support()

# Add GPU request to existing resource
k8s_yaml('embedding-server.yaml')
configure_gpu_resources('embedding-server', gpu_count=1)
```

This modifies the workload to request 1 GPU without editing YAML files.

## Complete Example: ML Inference Service

```python
# Tiltfile for ML inference service with GPU

# Enable GPU support with timesharing
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support', 'configure_gpu_resources')
enable_gpu_support({
    'cluster_name': 'k3d-ml-dev',
    'enable_profiling': True,     # Enable for parca profiling
    'timesharing_replicas': 4,    # Allow 4 services to share 1 GPU (default)
})

# Build inference server image
docker_build(
    'ml-inference',
    context='.',
    dockerfile='Dockerfile.inference',
    live_update=[
        sync('./models', '/app/models'),
        sync('./src', '/app/src'),
    ],
)

# Deploy inference server
k8s_yaml('k8s/inference-server.yaml')
configure_gpu_resources('inference-server', gpu_count=1)

# Deploy profiling (optional)
if os.environ.get('TILT_ENABLE_PROFILING', 'false') == 'true':
    k8s_yaml('k8s/parca-agent.yaml')
    k8s_resource(
        'parca-agent',
        labels=['observability'],
        resource_deps=['inference-server'],
    )
```

## Troubleshooting

### Error: "nvidia-ctk not found"

**Problem**: nvidia-container-toolkit not installed

**Solution**: Install toolkit for your distribution (see Prerequisites)

### Error: "No GPUs detected"

**Problem**: NVIDIA drivers not installed or not working

**Solution**:
```bash
nvidia-smi  # Should show GPU info
# If not, install NVIDIA drivers for your GPU model
```

### Error: "Cluster 'k3d-myapp' not found"

**Problem**: Cluster name doesn't match existing k3d cluster

**Solution**:
```bash
k3d cluster list  # See available clusters
# Use correct name in enable_gpu_support({'cluster_name': 'correct-name'})
```

### Error: "DNS resolution failed"

**Problem**: DNS fix didn't apply or Docker version too old

**Solution**: Try manual DNS configuration:
```python
enable_gpu_support({
    'dns_nameservers': ['8.8.8.8', '8.8.4.4'],  # Google DNS instead of defaults
})
```

### Pods Stuck in "Pending" State

**Problem**: Device plugin not running or GPU not detected

**Solution**:
```bash
# Check device plugin logs
kubectl logs -n kube-system -l name=nvidia-device-plugin-ds

# Check node GPU resources
kubectl get nodes -o json | jq '.items[].status.allocatable | ."nvidia.com/gpu"'
# Should show "1" or higher, not null
```

### WSL2: libdxcore.so Not Found

**Problem**: CDI spec generation failed on WSL2

**Solution**:
```bash
# Verify libdxcore.so exists
find /usr/lib -name "libdxcore.so"

# If missing, update WSL2 and ensure latest NVIDIA drivers
wsl --update
```

## Next Steps

- Read [data-model.md](data-model.md) for detailed configuration options
- Review [research.md](research.md) for technical implementation details
- See [contracts/extension-api.star](contracts/extension-api.star) for full API reference
- Check [examples/k3d-gpu/](../../examples/k3d-gpu/) for additional usage patterns

## Performance Expectations

- **Extension load time**: <1 second (detection + validation)
- **GPU initialization**: 10-30 seconds (device plugin startup + GPU detection)
- **First GPU pod schedule**: <5 seconds after device plugin ready
- **Overhead**: ~50MB memory for device plugin, negligible CPU

## Platform Support

| Platform | Status | Notes |
|----------|--------|-------|
| WSL2 (Windows 11) | ✅ Fully Supported | Primary target, uses CDI mode |
| Native Linux | ✅ Fully Supported | Standard NVIDIA runtime (auto/legacy mode) |
| macOS | ❌ Not Supported | No NVIDIA GPU support in containers |
| WSL1 | ❌ Not Supported | GPU access requires WSL2 |

## Known Limitations

1. **Multi-GPU selection**: Device plugin exposes all GPUs, but pod cannot request specific GPU by ID (Kubernetes limitation)
2. **GPU memory limits**: Not supported by most platforms (ignored if specified)
3. **MIG (Multi-Instance GPU)**: Not configured by extension (requires manual setup)
4. **Jetson/Tegra**: Not tested, may require custom device plugin image
