# k3d GPU Support Extension

Enable NVIDIA GPU support in local k3d clusters with zero configuration. Works on both WSL2 and native Linux.

## Overview

This extension provides **two approaches** for GPU-enabled k3d clusters:

1. **Quick Setup** (Recommended): Build a custom k3d image with pre-baked GPU support
2. **Runtime Configuration**: Configure GPU support on existing clusters via Tilt extension

**For most users, the Quick Setup approach is recommended** as it provides better reliability and faster cluster startup.

## Quick Setup (Recommended)

### 1. Create GPU-Enabled k3d Cluster

Choose one of three approaches:

#### Option A: Bash Script (Quickest)

```bash
# Default configuration (cluster name: gpu-dev)
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh

# Custom configuration
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh \
  --name my-cluster \
  --k3s-version v1.31.6+k3s1 \
  --api-port 6550 \
  --registry-port 5005
```

#### Option B: Mage Targets (Recommended for Go Projects)

Add to your `magefile.go`:

```go
import k3dgpu "github.com/randomvariable/tilt-common/extensions/k3d-gpu/magefiles"

// SetupGPUCluster creates a k3d cluster with GPU support
func SetupGPUCluster() error {
    return k3dgpu.CreateCluster(
        k3dgpu.WithName("my-cluster"),
        k3dgpu.WithModelCache("", ""),  // Persist models across recreations
        k3dgpu.WithVerbose(true),
    )
}

// TeardownGPUCluster deletes the cluster
func TeardownGPUCluster() error {
    return k3dgpu.DeleteCluster("my-cluster")
}
```

Then run:
```bash
mage setupGPUCluster
```

See [examples/k3d-gpu/magefile.go](../../examples/k3d-gpu/magefile.go) for a complete example.

#### Option C: Pure Go

Import the package directly in your Go code:

```go
import k3dgpu "github.com/randomvariable/tilt-common/extensions/k3d-gpu/magefiles"

func main() {
    if err := k3dgpu.CreateCluster(
        k3dgpu.WithName("my-cluster"),
        k3dgpu.WithModelCache("", ""),
    ); err != nil {
        log.Fatal(err)
    }
}
```

**What these approaches do:**
- Build a custom k3s Docker image with nvidia-container-toolkit pre-installed
- Use **BuildKit caching** for fast rebuilds (NVIDIA images are large ~2GB)
- Create a k3d cluster with GPU support enabled (`--gpus=all`)
- Auto-deploy the NVIDIA device plugin on cluster startup
- Set up a local container registry for your images

### 2. Use GPU Support in Tiltfile (Optional)

If you need additional configuration (GPU timesharing, custom device plugin), add to your Tiltfile:

```python
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')
enable_gpu_support()
```

### 3. Delete Cluster When Done

```bash
./extensions/k3d-gpu/scripts/delete-gpu-cluster.sh --name my-cluster
```

## Alternative: Runtime Configuration

If you have an existing k3d cluster and want to configure GPU support at runtime:

```python
# Tiltfile
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')
enable_gpu_support()
```

Run `tilt up` and GPU support is automatically configured.

**Note**: This approach has limitations on WSL2 due to the need to modify cluster node configuration at runtime.

## Prerequisites

Before using this extension, ensure you have:

### Required

1. **NVIDIA GPU** with drivers installed
   ```bash
   nvidia-smi  # Should show GPU info
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

3. **k3d** cluster (for local Kubernetes development)
   ```bash
   k3d cluster list  # Should show your cluster
   ```

4. **Tilt 0.30+**
   ```bash
   tilt version  # Should be >= v0.30.0
   ```

## Features

- ✅ **Zero-config GPU access**: One function call enables everything
- ✅ **WSL2 support**: Automatic CDI spec generation with all required mounts
- ✅ **DNS fix for Docker 29+**: Resolves containerd image pull failures
- ✅ **GPU timesharing**: 4 pods can share each physical GPU by default
- ✅ **eBPF profiling support**: Mount debugfs/tracefs for parca, bpftrace, etc.
- ✅ **Native Linux compatible**: Works on both WSL2 and native Linux
- ✅ **Clear error messages**: Helpful troubleshooting with remediation steps

## Configuration Options

All configuration is optional. Defaults work for most cases.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `cluster_name` | string | auto-detect | k3d cluster name |
| `namespace` | string | `kube-system` | Namespace for device plugin |
| `enable_profiling` | bool | `true` | Mount debugfs/tracefs for eBPF profiling |
| `device_plugin_image` | string | `nvcr.io/nvidia/k8s-device-plugin:v0.14.3` | Device plugin image |
| `timesharing_replicas` | int | `4` | GPU timesharing factor (1-100) |
| `skip_dns_fix` | bool | `false` | Skip DNS reconfiguration |
| `skip_validation` | bool | `false` | Skip prerequisite checks |

### GPU Timesharing

By default, the extension enables GPU timesharing with a factor of 4, allowing 4 pods to share each physical GPU:

```python
# Default behavior (4x timesharing)
enable_gpu_support()  # Reports 4 GPUs if you have 1 physical GPU

# Disable timesharing (1:1 mapping)
enable_gpu_support({'timesharing_replicas': 1})

# More aggressive sharing for lightweight workloads
enable_gpu_support({'timesharing_replicas': 8})
```

**Why timesharing?** Local development workloads (inference, small training jobs) rarely use 100% of GPU resources. Timesharing allows multiple services to run simultaneously on one GPU, matching the reality that most pods are idle or use <25% GPU capacity.

## Usage Examples

### Basic Usage (Zero Configuration)

```python
# Tiltfile
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')

enable_gpu_support()

# Your existing resources...
k8s_yaml('deployment.yaml')
```

### Custom Configuration

```python
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')

enable_gpu_support({
    'cluster_name': 'k3d-myapp',
    'namespace': 'gpu-system',
    'device_plugin_image': 'nvcr.io/nvidia/k8s-device-plugin:v0.15.0',
    'timesharing_replicas': 4,
    'enable_profiling': True,
})
```

### Test GPU Access

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

## Platform Support

| Platform | Status | Notes |
|----------|--------|-------|
| WSL2 (Windows 11) | ✅ Fully Supported | Primary target, uses CDI mode |
| Native Linux | ✅ Fully Supported | Standard NVIDIA runtime (auto/legacy mode) |
| macOS | ❌ Not Supported | No NVIDIA GPU support in containers |
| WSL1 | ❌ Not Supported | GPU access requires WSL2 |

## How It Works

### WSL2

1. Detects WSL2 environment (`/dev/dxg` device)
2. Generates CDI spec with:
   - libdxcore.so mount (critical for WSL2)
   - WSL driver store mount
   - GPU device and UUID
3. Configures nvidia-container-runtime to use CDI mode
4. Fixes DNS for Docker 29+ (127.0.0.11 + public DNS)
5. Mounts debugfs/tracefs for eBPF profiling
6. Deploys NVIDIA device plugin with timesharing

### Native Linux

1. Detects native Linux (no `/dev/dxg`)
2. Skips CDI generation (uses standard `/dev/nvidia*` devices)
3. Uses auto/legacy nvidia-container-runtime mode
4. Fixes DNS for Docker 29+
5. Mounts debugfs/tracefs for eBPF profiling
6. Deploys NVIDIA device plugin with timesharing

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

### Error: "DNS resolution failed"

**Problem**: DNS fix didn't apply or Docker version incompatible

**Solution**: Try manual DNS configuration:
```python
enable_gpu_support({
    'dns_nameservers': ['8.8.8.8', '8.8.4.4'],
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
# Should show "4" (or your timesharing_replicas value), not null
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

## Performance

### Cluster Creation

- **First build**: 3-5 minutes (downloads NVIDIA CUDA base image ~2GB + packages)
- **Subsequent builds with BuildKit cache**: 10-30 seconds (cache hit on apt packages)
- **Cluster startup**: 20-40 seconds (k3d creation + node ready + device plugin)
- **Total time (cached)**: ~1 minute from build to ready

### Runtime

- **Extension load time**: <1 second (detection + validation)
- **GPU initialization**: 10-30 seconds (device plugin startup + GPU detection)
- **First GPU pod schedule**: <5 seconds after device plugin ready
- **Overhead**: ~50MB memory for device plugin, negligible CPU

### BuildKit Caching

The Dockerfile uses BuildKit cache mounts for optimal performance:

- **apt package cache**: Shared across builds, eliminates re-downloading packages
- **Alpine apk cache**: Speeds up k3s binary download stage
- **Layer caching**: Docker layers are reused when base image hasn't changed

To ensure BuildKit is enabled:
```bash
export DOCKER_BUILDKIT=1  # Bash script automatically enables this
```

Rebuild times with cache hit:
- Without BuildKit: ~3-5 minutes (re-downloads everything)
- With BuildKit: ~10-30 seconds (uses cached packages)

## Known Limitations

1. **Multi-GPU selection**: Device plugin exposes all GPUs, but pod cannot request specific GPU by ID (Kubernetes limitation)
2. **GPU memory limits**: Not supported by most platforms (ignored if specified)
3. **MIG (Multi-Instance GPU)**: Not configured by extension (requires manual setup)
4. **Jetson/Tegra**: Not tested, may require custom device plugin image

## Architecture

### Approach 1: Custom k3d Image (Quick Setup)

```
create-gpu-cluster.sh
  ├── Build custom k3s image
  │   ├── Base: nvidia/cuda:12.8.1-runtime-ubuntu24.04
  │   ├── Install: nvidia-container-toolkit
  │   ├── Install: k3s binary (as k3s.real)
  │   ├── Copy: k3s-wrapper.sh (as /bin/k3s)
  │   ├── Configure: containerd nvidia runtime
  │   └── Bundle: device plugin manifest
  └── Create k3d cluster
      ├── Use custom image (--image=k3s-gpu:local)
      ├── Enable GPU passthrough (--gpus=all)
      ├── Create local registry
      └── Wait for cluster ready

k3s-wrapper.sh (runs on cluster startup)
  ├── Fix DNS (Docker 29+)
  ├── Mount debugfs/tracefs (eBPF profiling)
  ├── Generate CDI spec (WSL2 only)
  ├── Configure nvidia-container-runtime
  └── Exec k3s.real (start k3s)
```

### Approach 2: Runtime Configuration (Tilt Extension)

```
Tiltfile (enable_gpu_support())
  ├── Detect system environment (WSL2 vs native Linux)
  ├── Validate configuration
  ├── Generate device plugin YAML
  │   └── Deploy DaemonSet with timesharing
  └── Generate k3s wrapper script (manual deployment)
      ├── Fix DNS (Docker 29+)
      ├── Mount debugfs/tracefs (eBPF profiling)
      ├── Generate CDI spec (WSL2 only)
      └── Configure nvidia-container-runtime
```

### Custom Image Build Process

The custom k3d image approach works by:

1. **Base Image**: Start with NVIDIA CUDA runtime image (ubuntu 24.04 + CUDA 12.8.1)
2. **Toolkit Installation**: Install nvidia-container-toolkit from NVIDIA apt repository
3. **k3s Integration**: Download k3s binary and rename to `k3s.real`
4. **Wrapper Script**: Copy `k3s-wrapper.sh` as `/bin/k3s` to intercept cluster startup
5. **Containerd Config**: Pre-configure containerd nvidia runtime via conf.d
6. **Device Plugin**: Bundle device plugin manifest in k3s auto-deploy directory

When the cluster starts:
- k3d runs the custom image with `--gpus=all` (Docker GPU passthrough)
- k3s-wrapper.sh runs first (as /bin/k3s), performs all GPU setup
- k3s-wrapper.sh execs k3s.real to start the actual k3s server
- Device plugin is auto-deployed by k3s from the manifests directory

## Development

### Running Tests

```bash
# Run all tests
./extensions/k3d-gpu/test.sh

# Run specific test
./tests/k3d-gpu/test-load.sh
./tests/k3d-gpu/test-output-validation.sh
./tests/k3d-gpu/test-config-validation.sh
```

### Project Structure

```
extensions/k3d-gpu/
├── extension.star          # Main Tilt extension
├── assets/
│   ├── k3s-wrapper.sh      # k3s entrypoint wrapper
│   ├── device-plugin.yaml  # Device plugin template
│   └── cluster/            # Custom k3d image assets
│       ├── Dockerfile      # Custom k3s GPU image
│       ├── k3s-wrapper.sh  # Wrapper script (copy)
│       ├── nvidia-runtime.toml # Containerd runtime config
│       └── device-plugin-daemonset.yaml # Device plugin manifest
├── scripts/
│   ├── create-gpu-cluster.sh # Build image & create cluster
│   └── delete-gpu-cluster.sh # Delete cluster
├── test.sh                 # Test runner
└── README.md               # This file

examples/k3d-gpu/
├── Tiltfile                # Basic example
├── Tiltfile.advanced       # Advanced example with timesharing
└── test-gpu-pod.yaml       # GPU test pod

tests/k3d-gpu/
├── golden/                 # Expected output files
│   ├── device-plugin.yaml
│   ├── cdi-spec-wsl2.yaml
│   └── device-plugin-native.yaml
├── test-load.sh            # Smoke test
├── test-output-validation.sh # YAML validation
└── test-config-validation.sh # Config validation
```

## License

MIT License - See repository root for full license text.

## Contributing

Contributions welcome! Please:
1. Follow existing code style
2. Add tests for new features
3. Update documentation
4. Ensure tests pass before submitting PR

## Support

- **Issues**: [GitHub Issues](https://github.com/randomvariable/tilt-common/issues)
- **Discussions**: [GitHub Discussions](https://github.com/randomvariable/tilt-common/discussions)

## Related

- [NVIDIA Container Toolkit](https://github.com/NVIDIA/nvidia-container-toolkit)
- [NVIDIA Device Plugin](https://github.com/NVIDIA/k8s-device-plugin)
- [Tilt Extensions](https://docs.tilt.dev/extensions.html)
- [Container Device Interface (CDI)](https://github.com/cncf-tags/container-device-interface)
