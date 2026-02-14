# k3d GPU Extension Examples

This directory contains examples for using the k3d GPU extension.

## Complete Workflow Examples

### Approach 1: Using Mage Targets (Recommended for Go Projects)

See [magefile.go](./magefile.go) for a complete example.

```bash
# Create GPU cluster using mage
mage setupGPUCluster

# Check cluster status
mage clusterStatus

# Your development work here...
tilt up

# Clean up when done
mage teardownGPUCluster
```

### Approach 2: Using Bash Script

```bash
# Navigate to the tilt-common repository
cd /path/to/tilt-common

# Create a GPU-enabled k3d cluster
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh --name demo-gpu

# Output:
# [INFO] Building custom k3s GPU image: k3s-gpu:v1.31.6+k3s1-local
# [INFO] Creating k3d cluster: demo-gpu
# [SUCCESS] Cluster created successfully
# [INFO] Cluster demo-gpu is ready.
```

### 2. Create Your Application Tiltfile

```python
# Tiltfile
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')

# Enable GPU support with custom configuration (optional)
enable_gpu_support({
    'timesharing_replicas': 4,  # 4 pods can share each GPU
    'enable_profiling': True,   # Mount debugfs/tracefs for eBPF
})

# Your application resources
k8s_yaml('deployment.yaml')
docker_build('myapp', '.')
```

### 3. Start Tilt

```bash
tilt up
```

### 4. Verify GPU Access

```bash
# Check node GPU resources
kubectl get nodes -o json | jq '.items[].status.allocatable."nvidia.com/gpu"'
# Output: "4" (with default timesharing of 4)

# Deploy test pod
kubectl apply -f test-gpu-pod.yaml

# Check GPU is accessible
kubectl logs gpu-test
# Output: Should show nvidia-smi output
```

### 5. Clean Up

```bash
# Stop Tilt
tilt down

# Delete cluster
./extensions/k3d-gpu/scripts/delete-gpu-cluster.sh --name demo-gpu
```

## Model Cache Pattern (HuggingFace/ML Models)

### Why Model Caching Matters

ML models can be large (100MB - 10GB+) and slow to download. The model cache pattern:
- **Persists models** across cluster recreations (no re-download)
- **Shares models** between pods (download once, use everywhere)
- **Speeds up development** (instant cluster recreation)

### How It Works

**Two-step volume mounting:**

1. **Host → k3d node**: `~/.cache/models` → `/var/lib/models` (via k3d volume)
2. **k3d node → Pod**: `/var/lib/models` → `/models` (via hostPath)

**Setup:**

```go
// In your magefile.go
k3dgpu.CreateCluster(
    k3dgpu.WithName("my-cluster"),
    k3dgpu.WithModelCache("~/.cache/models", "/var/lib/models"),
)
```

**Usage in Kubernetes:**

```yaml
# In your deployment
spec:
  volumes:
    - name: models
      hostPath:
        path: /var/lib/models  # Matches the container path from k3d mount
        type: DirectoryOrCreate
  containers:
    - name: my-app
      volumeMounts:
        - name: models
          mountPath: /models
```

See [model-cache-example.yaml](./model-cache-example.yaml) for complete examples including:
- llama.cpp with model download initContainer
- HuggingFace Transformers with TRANSFORMERS_CACHE
- Multiple pods sharing models with GPU timesharing

## Example Files

### Basic Example

**File**: `Tiltfile`

Simple GPU support with defaults:

```python
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')
enable_gpu_support()
```

### Advanced Example

**File**: `Tiltfile.advanced`

Full configuration with GPU timesharing and 4 test pods:

```python
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')

# Configure GPU support with timesharing
enable_gpu_support({
    'cluster_name': 'k3d-demo-gpu',
    'namespace': 'kube-system',
    'device_plugin_image': 'nvcr.io/nvidia/k8s-device-plugin:v0.14.3',
    'timesharing_replicas': 4,  # 4 pods per physical GPU
    'enable_profiling': True,
})

# Deploy 4 GPU test pods (will share one physical GPU)
k8s_yaml('test-gpu-pod.yaml')
k8s_yaml('test-gpu-pod-2.yaml')
k8s_yaml('test-gpu-pod-3.yaml')
k8s_yaml('test-gpu-pod-4.yaml')
```

### GPU Test Pod

**File**: `test-gpu-pod.yaml`

Simple pod to verify GPU access:

```yaml
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

## Environment Variables

You can customize cluster creation via environment variables:

```bash
# Custom cluster name
export K3D_GPU_CLUSTER_NAME=my-cluster

# Custom k3s version
export K3D_GPU_K3S_VERSION=v1.30.0+k3s1

# Custom ports
export K3D_GPU_API_PORT=6551
export K3D_GPU_REGISTRY_PORT=5006

# Run cluster creation
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh
```

## Workflow Patterns

### Pattern 1: Ephemeral Clusters (Recommended for CI)

```bash
# Create cluster
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh --name ci-gpu

# Run tests
tilt ci

# Clean up
./extensions/k3d-gpu/scripts/delete-gpu-cluster.sh --name ci-gpu
```

### Pattern 2: Long-Lived Dev Cluster

```bash
# Create once
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh --name dev-gpu

# Develop with Tilt
tilt up  # Start development
# ... make changes, Tilt rebuilds ...
tilt down  # Stop when done

# Cluster remains for next session
# Delete only when you're done for good
./extensions/k3d-gpu/scripts/delete-gpu-cluster.sh --name dev-gpu
```

### Pattern 3: Multiple Clusters

```bash
# Create multiple clusters for different projects
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh --name project-a --api-port 6550 --registry-port 5005
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh --name project-b --api-port 6551 --registry-port 5006

# Switch between clusters
kubectl config use-context k3d-project-a
kubectl config use-context k3d-project-b

# Clean up specific cluster
./extensions/k3d-gpu/scripts/delete-gpu-cluster.sh --name project-a
```

## Troubleshooting

### Image Build Failed

If the custom image build fails:

```bash
# Check Docker is running
docker ps

# Verify nvidia-container-toolkit is installed
nvidia-ctk --version

# Try building manually
cd extensions/k3d-gpu/assets/cluster
docker build -t k3s-gpu:v1.31.6+k3s1-local --build-arg K3S_VERSION=v1.31.6+k3s1 .
```

### Cluster Creation Failed

If cluster creation fails:

```bash
# Check k3d is installed
k3d version

# Check if cluster name is already taken
k3d cluster list

# Delete existing cluster if needed
k3d cluster delete old-cluster-name

# Try creating again
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh
```

### Device Plugin Not Running

If GPU resources are not available:

```bash
# Check device plugin logs
kubectl logs -n kube-system -l app=nvidia-device-plugin-ds

# Check if GPUs are visible to Docker
docker run --rm --gpus=all nvidia/cuda:12.0.0-base-ubuntu22.04 nvidia-smi

# Recreate cluster if needed
./extensions/k3d-gpu/scripts/delete-gpu-cluster.sh
./extensions/k3d-gpu/scripts/create-gpu-cluster.sh
```

## Reference

- [k3d Documentation](https://k3d.io/)
- [NVIDIA Container Toolkit](https://github.com/NVIDIA/nvidia-container-toolkit)
- [NVIDIA Device Plugin](https://github.com/NVIDIA/k8s-device-plugin)
- [Tilt Documentation](https://docs.tilt.dev/)
