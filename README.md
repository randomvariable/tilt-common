# tilt-common

Reusable [Tilt](https://tilt.dev/) extensions and utilities for local Kubernetes development with GPU support.

## Extensions

### [k3d-gpu](extensions/k3d-gpu/)

Enable NVIDIA GPU support in local k3d clusters with zero configuration. Works on both WSL2 and native Linux.

**Features:**

- Zero-config GPU access via a single Tilt `load()` statement
- WSL2 support with automatic CDI spec generation
- DNS fix for Docker 29+ (resolves containerd image pull failures)
- GPU timesharing (multiple pods share a single physical GPU)
- eBPF profiling support (debugfs/tracefs mounting)
- Native Linux compatible

**Quick start:**

```python
# Tiltfile
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')
enable_gpu_support()
```

Or create a GPU-enabled cluster programmatically with Go:

```go
import k3dgpu "github.com/randomvariable/tilt-common/extensions/k3d-gpu"

err := k3dgpu.CreateCluster(
    k3dgpu.WithName("my-cluster"),
    k3dgpu.WithModelCache("", ""),
)
```

See the [k3d-gpu README](extensions/k3d-gpu/README.md) for full documentation.

## Prerequisites

- [Tilt](https://docs.tilt.dev/install.html) v0.30+
- [k3d](https://k3d.io/) for local Kubernetes clusters
- [Docker](https://docs.docker.com/get-docker/) with BuildKit support
- NVIDIA GPU with drivers installed (`nvidia-smi` should work)
- [nvidia-container-toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html)

## Project Structure

```
tilt-common/
├── extensions/
│   └── k3d-gpu/              # NVIDIA GPU support extension
│       ├── extension.star     # Tilt extension (Starlark)
│       ├── k3dgpu.go          # Go library (mage targets)
│       ├── assets/            # Templates and cluster image assets
│       └── scripts/           # Cluster management shell scripts
├── examples/
│   └── k3d-gpu/              # Usage examples (Tiltfile, magefile.go)
├── tests/
│   └── k3d-gpu/              # Test suite with golden files
├── specs/                     # Feature specifications
```

## Development

### Build and Test

```bash
# Build Go code
go build ./...

# Run Go tests
go test ./... -count=1 -timeout=120s -race

# Run extension tests
./extensions/k3d-gpu/test.sh

# Lint
golangci-lint run
```

### CI

GitHub Actions runs build, test (with race detector), and lint on every push and PR to `main`.

## License

Copyright 2026 Naadir Jeewa

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
