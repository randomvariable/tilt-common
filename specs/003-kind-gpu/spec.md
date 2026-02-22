# Feature Specification: kind GPU Support Extension

**Feature Branch**: `002-observability-stack` (co-located)
**Created**: 2026-02-15
**Status**: Draft
**Input**: User description: "kindGPU extension with APIMachinery versioned YAML config"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Create kind Cluster with GPU Support (Priority: P1)

A developer working on GPU-accelerated applications needs a local kind cluster with NVIDIA GPU passthrough. They define their cluster configuration in an APIMachinery-style YAML file that embeds the kind v1alpha4 cluster spec, GPU type, and optional containerd registry mirrors with inline CA certificates.

**Acceptance Scenarios**:

1. **Given** a `kind-gpu.yaml` config file with GPU type `nvidia`, **When** the developer calls `kindgpu.CreateCluster("kind-gpu.yaml")` from a magefile, **Then** a custom kindest/node image is built with nvidia-container-toolkit and the kind cluster boots with GPU access.
2. **Given** the custom image already exists locally, **When** CreateCluster is called again, **Then** the image build is skipped (idempotent).
3. **Given** mirror configurations with base64-encoded CA data, **When** the image is built, **Then** containerd hosts.toml files and decoded CA certificates are baked into the image.

### User Story 2 - Registry Mirror Configuration (Priority: P2)

A developer behind a corporate proxy uses a Harbor registry mirror. They configure mirrors in the YAML config with base64-encoded CA certificates instead of file paths, making the config portable across machines.

**Acceptance Scenarios**:

1. **Given** a mirror config for `docker.io` pointing to `harbor.example.com`, **When** the image is built, **Then** `/etc/containerd/certs.d/docker.io/hosts.toml` is correctly generated inside the image.
2. **Given** CAData in the mirror config, **When** hosts.toml is generated, **Then** the CA cert is decoded from base64 and written as a PEM file referenced by the hosts.toml.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Extension MUST accept an APIMachinery-versioned YAML config file (`apiVersion: tilt-common.randomvariable.co.uk/v1alpha1`, `kind: KindGPUCluster`)
- **FR-002**: Config MUST embed a kind v1alpha4 Cluster definition in `spec.cluster`
- **FR-003**: Config MUST include a GPU struct with a `type` field supporting `nvidia`
- **FR-004**: Config MUST support optional mirror configurations with containerd host config structs where CA file paths are replaced with base64-encoded `caData` strings
- **FR-005**: Extension MUST idempotently build a BuildKit-based Dockerfile starting FROM the upstream kindest/node image
- **FR-006**: The built image MUST install the NVIDIA container toolkit
- **FR-007**: The built image MUST include containerd host configs for configured mirrors
- **FR-008**: Extension MUST create a kind cluster using the custom image with containerd config patches for the nvidia runtime
- **FR-009**: Extension MUST provide `CreateCluster` and `DeleteCluster` Go functions following the functional options pattern (matching k3d-gpu conventions)
- **FR-010**: Extension MUST provide `LoadConfig` to parse the YAML config file

### Assumptions

- Docker with BuildKit support is available on the host
- NVIDIA drivers are installed on the host system
- The `kind` binary is available in PATH or managed tools directory
- Docker runtime is configured to support GPU passthrough (nvidia runtime or Docker Desktop WSL2)

## Success Criteria *(mandatory)*

- **SC-001**: Config YAML parses correctly into Go types with full kind v1alpha4 compatibility
- **SC-002**: Image builds are idempotent (skip if image tag exists)
- **SC-003**: Mirror hosts.toml files are correctly generated with decoded CA certificates
- **SC-004**: All Go code passes golangci-lint with zero exclusions
- **SC-005**: Unit tests cover config parsing, Dockerfile generation, and mirror config generation
