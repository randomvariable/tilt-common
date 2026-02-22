# Data Model: kind GPU Support Extension

## Entity Diagram

```
KindGPUCluster (apiVersion: tilt-common.randomvariable.co.uk/v1alpha1)
├── TypeMeta (metav1.TypeMeta)
│   ├── APIVersion: string
│   └── Kind: string
└── Spec (KindGPUClusterSpec)
    ├── Cluster (kind v1alpha4.Cluster)
    │   ├── TypeMeta
    │   ├── Name: string
    │   ├── Nodes: []Node
    │   │   ├── Role: NodeRole (control-plane|worker)
    │   │   ├── Image: string (kindest/node:v1.x.y)
    │   │   ├── ExtraMounts: []Mount
    │   │   └── ExtraPortMappings: []PortMapping
    │   ├── Networking: Networking
    │   ├── FeatureGates: map[string]bool
    │   └── ContainerdConfigPatches: []string
    ├── GPU (GPUConfig)
    │   └── Type: GPUType (nvidia)
    └── Mirrors: []MirrorConfig
        ├── Registry: string (e.g., "docker.io")
        └── Endpoints: []ContainerdHostConfig
            ├── Server: string (e.g., "https://harbor.example.com")
            ├── Capabilities: []string (pull, resolve, push)
            ├── CAData: string (base64-encoded PEM)
            ├── SkipVerify: *bool
            ├── Header: map[string]string
            └── OverridePath: bool
```

## Example Config YAML

```yaml
apiVersion: tilt-common.randomvariable.co.uk/v1alpha1
kind: KindGPUCluster
spec:
  cluster:
    apiVersion: kind.x-k8s.io/v1alpha4
    kind: Cluster
    nodes:
      - role: control-plane
        image: kindest/node:v1.32.2
  gpu:
    type: nvidia
  mirrors:
    - registry: docker.io
      endpoints:
        - server: "https://harbor.example.com/docker"
          capabilities: ["pull", "resolve"]
          caData: "LS0tLS1CRUdJTi..."
```

## Generated Artifacts

### Dockerfile (build context)

```dockerfile
FROM kindest/node:v1.32.2
RUN <install nvidia-container-toolkit>
COPY certs.d/ /etc/containerd/certs.d/
```

### hosts.toml (per mirror registry)

```toml
server = "https://docker.io"

[host."https://harbor.example.com/docker"]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/docker.io/harbor.example.com-ca.pem"
```

### containerdConfigPatch (injected into kind cluster config)

```toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia]
  runtime_type = "io.containerd.runc.v2"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia.options]
  BinaryName = "nvidia-container-runtime"
[plugins."io.containerd.grpc.v1.cri".containerd]
  default_runtime_name = "nvidia"
```
