# Research: kind GPU Support Extension

## Decision 1: API Type Structure

**Decision**: Use `metav1.TypeMeta` from k8s.io/apimachinery for the outer KindGPUCluster config, and embed `sigs.k8s.io/kind/pkg/apis/config/v1alpha4.Cluster` for the inner cluster spec.

**Rationale**: Matches the pattern established by `.tools.yaml` (mage-common `ToolConfiguration`) where APIMachinery TypeMeta provides versioned, self-describing YAML configs. Importing kind's actual types ensures full compatibility with the kind v1alpha4 schema.

**Alternatives considered**:
- Define custom Cluster types mirroring kind's schema: Rejected because maintaining parity with upstream kind types is error-prone.
- Use `yaml.RawMessage` for the cluster field: Rejected because it loses type safety and prevents programmatic modification (e.g., injecting containerdConfigPatches).

## Decision 2: Image Build Strategy

**Decision**: Generate a build context directory with a Dockerfile (from embedded Go template), mirror hosts.toml files, and decoded CA certificates. Build with `docker build --build-arg BASE_IMAGE=kindest/node:...` using BuildKit.

**Rationale**: Matches k3d-gpu's pattern of building custom images via Docker. BuildKit provides caching for apt layers. Generating the build context allows dynamic mirror configs to be baked into the image.

**Alternatives considered**:
- Mount mirrors via kind extraMounts at runtime: Rejected because user explicitly requested baking host config into the image via Dockerfile.
- Use multi-stage build with NVIDIA CUDA base: Rejected because kindest/node has specific systemd/containerd setup that's hard to replicate from scratch.

## Decision 3: Idempotent Build

**Decision**: Check if the image tag exists locally via `docker image inspect`. If it exists, skip the build. Image tag is `kindest/node-gpu:{base-version}` derived from the base kindest/node tag.

**Rationale**: Simple and effective. Users can force rebuild by removing the image with `docker rmi`.

## Decision 4: Containerd Mirror Configuration

**Decision**: Generate containerd `hosts.toml` files per the containerd hosts specification. CA data is decoded from base64 and written as PEM files alongside the hosts.toml. These files are COPYed into the image at `/etc/containerd/certs.d/{registry}/`.

**Rationale**: This is the standard containerd approach for registry mirrors. Kind supports the `certs.d` directory natively.

## Decision 5: NVIDIA Runtime Configuration

**Decision**: Add `containerdConfigPatches` to the kind cluster config at create time to configure the nvidia runtime as the default containerd runtime. The nvidia-container-toolkit package provides the runtime binary.

**Rationale**: Kind regenerates the containerd config.toml at cluster creation time, so configuring it inside the Dockerfile alone isn't sufficient. ContainerdConfigPatches are the official kind mechanism for this.

## Decision 6: GPU Passthrough

**Decision**: Rely on the host's Docker daemon configuration for GPU device passthrough. On WSL2 with Docker Desktop, GPU access is automatic. On native Linux, the nvidia Docker runtime must be configured.

**Rationale**: Kind containers inherit the Docker daemon's GPU configuration. This is the standard approach documented by NVIDIA for kind GPU clusters.
