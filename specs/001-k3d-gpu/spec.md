# Feature Specification: k3s GPU Support Extension

**Feature Branch**: `001-k3d-gpu`
**Created**: 2026-02-14
**Status**: Draft
**Input**: User description: "k3d-gpu"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Enable GPU Workloads in Local k3s (Priority: P1)

A developer working on GPU-accelerated applications (ML inference, embedding generation, video processing) needs to test their workloads locally in a k3s cluster that has access to their laptop's NVIDIA GPU. They want to add GPU support to their existing Tilt development environment without manually configuring containerd, CDI specs, or device plugins.

**Why this priority**: This is the core value proposition. Without GPU access, developers cannot test GPU workloads locally and must deploy to cloud instances for every code change, slowing iteration cycles from seconds to minutes.

**Independent Test**: Can be fully tested by loading the k3d-gpu extension in a Tiltfile, running `tilt up`, and successfully scheduling a pod with `resources.limits.nvidia.com/gpu: 1`. The pod should have access to the GPU and be able to run nvidia-smi.

**Acceptance Scenarios**:

1. **Given** a k3s cluster without GPU support, **When** developer loads the k3d-gpu extension and runs `tilt up`, **Then** the NVIDIA device plugin deploys successfully and reports available GPUs
2. **Given** the extension is loaded, **When** developer deploys a pod requesting GPU resources, **Then** the pod schedules successfully and can access the GPU via CUDA/NVML APIs
3. **Given** the developer is on WSL2, **When** the extension configures the cluster, **Then** CDI specs are generated correctly with WSL2-specific device paths and library mounts
4. **Given** Docker 29+ is installed, **When** the extension initializes, **Then** DNS resolution works correctly in both k3s and containerd (public DNS and Docker network resolution)

---

### User Story 2 - Profile GPU Workloads with eBPF Tools (Priority: P2)

A developer using continuous profiling tools (like Parca) needs to profile their GPU workloads for performance optimization. The profiler requires access to kernel tracing facilities (debugfs and tracefs) which are not mounted by default in k3s containers.

**Why this priority**: Observability is critical for production-grade development, but it's secondary to basic GPU functionality. Developers can manually mount these filesystems if needed, but automation saves time and prevents configuration errors.

**Independent Test**: Can be tested independently by deploying parca-agent or another eBPF-based profiler after enabling the k3d-gpu extension. The profiler should successfully access tracing facilities and collect GPU workload profiles.

**Acceptance Scenarios**:

1. **Given** the k3d-gpu extension is loaded, **When** the cluster initializes, **Then** debugfs is mounted at /sys/kernel/debug and tracefs at /sys/kernel/tracing
2. **Given** debugfs and tracefs are mounted, **When** an eBPF profiler deploys, **Then** it can attach tracepoints and collect performance data from GPU workloads

---

### User Story 3 - Use GPU Extension on Native Linux (Priority: P3)

A developer working on native Linux (not WSL2) wants to use the same k3d-gpu extension for local development without WSL2-specific workarounds interfering with standard NVIDIA device paths.

**Why this priority**: While WSL2 is a common development environment, native Linux users should also benefit from the extension. However, native Linux has fewer configuration challenges (standard /dev/nvidia* devices), so this is lower priority.

**Independent Test**: Can be tested independently on a native Linux system by loading the extension and verifying that GPU workloads schedule without WSL2-specific CDI configuration being applied.

**Acceptance Scenarios**:

1. **Given** the extension detects native Linux (kernel version without "microsoft"/"wsl"), **When** the cluster initializes, **Then** standard NVIDIA runtime configuration is applied without WSL2 CDI workarounds
2. **Given** native Linux environment, **When** a GPU pod deploys, **Then** it uses legacy mode or auto mode NVIDIA runtime successfully

---

### Edge Cases

- What happens when the NVIDIA driver is not installed or nvidia-ctk command is missing?
- How does the extension handle k3s clusters that are already configured with GPU support?
- What happens when multiple GPUs are present — does the device plugin expose all of them?
- What happens when the timesharing replica count is set to extreme values (1, 100, or negative numbers)?
- How does the extension validate timesharing configuration (minimum/maximum bounds, integer validation)?
- How does the extension behave on systems that are neither WSL2 nor native Linux (e.g., macOS)?
- What happens when DNS resolution is already configured correctly — does the extension's DNS fix interfere?
- How does the extension handle k3s version differences (containerd runtime versions, CNI changes)?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Extension MUST detect whether the system is running on WSL2 by checking the kernel version string (uname -r) for "microsoft" or "wsl"
- **FR-002**: Extension MUST generate CDI (Container Device Interface) specs for WSL2 environments that include libdxcore.so and WSL driver store mounts
- **FR-003**: Extension MUST configure nvidia-container-runtime to use CDI mode on WSL2 systems
- **FR-004**: Extension MUST deploy the NVIDIA device plugin DaemonSet that advertises GPU resources to Kubernetes
- **FR-005**: Extension MUST fix DNS resolution for Docker 29+ by replacing bridge gateway nameserver with working resolvers (Docker embedded DNS + public fallback)
- **FR-006**: Extension MUST mount debugfs at /sys/kernel/debug and tracefs at /sys/kernel/tracing for eBPF profiling tools
- **FR-007**: Extension MUST be configurable via a dictionary parameter that controls:
  - Target cluster name and namespace
  - Feature enablement flags (profiling, DNS fix, timesharing)
  - GPU resource limits (replica count for timesharing)
  - Validation behavior (strict vs permissive mode)
- **FR-007a**: Extension MUST support GPU timesharing with configurable replica count (default: 4 pods per physical GPU)
- **FR-008**: Extension MUST validate that required binaries (nvidia-ctk, nvidia-container-cli) are available before attempting GPU configuration
- **FR-009**: Extension MUST provide clear error messages when GPU support prerequisites are not met
- **FR-010**: Extension MUST not interfere with existing GPU configuration if the cluster already has device plugins deployed
- **FR-011**: Extension MUST generate Kubernetes YAML manifests that can be inspected with `tilt dump` for debugging

### Assumptions

- k3s cluster uses containerd as the container runtime (not dockerd)
- NVIDIA drivers are installed on the host system
- nvidia-container-toolkit is installed and provides nvidia-ctk and nvidia-container-cli commands
- k3s nodes run with privileged containers (standard for k3d)
- Users have basic familiarity with Tilt extensions (load syntax, configuration dictionaries)

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Developer can go from zero GPU support to running GPU workloads in under 2 minutes by adding one load() statement to their Tiltfile
- **SC-002**: GPU-requesting pods (resources.limits.nvidia.com/gpu: 1) successfully schedule and access the GPU on first attempt without manual intervention
- **SC-002a**: Multiple pods (up to timesharing limit) can request GPU resources simultaneously and all schedule successfully on a single physical GPU. Pods exceeding the replica limit will remain in Pending state until a GPU slot becomes available (standard Kubernetes scheduling behavior).
- **SC-003**: Extension works correctly on both WSL2 and native Linux without environment-specific configuration changes in the Tiltfile
- **SC-004**: DNS resolution works for both external domains and Docker network container names (e.g., local registry resolution) after the extension configures the cluster
- **SC-005**: eBPF profiling tools can successfully attach to GPU workloads and collect performance data without manual filesystem mounting
- **SC-006**: Extension provides actionable error messages (with remediation steps) when prerequisites are missing, reducing troubleshooting time by 80%

### Key Entities

- **GPU Device**: Represents an NVIDIA GPU available on the host system, identified by UUID or device number, exposed to Kubernetes via device plugin
- **CDI Spec**: Container Device Interface specification file that defines how to inject GPU devices and driver libraries into containers, generated differently for WSL2 vs native Linux
- **Device Plugin**: Kubernetes DaemonSet that discovers GPUs and advertises them as schedulable resources (nvidia.com/gpu), deployed by the extension
- **Runtime Configuration**: nvidia-container-runtime settings (mode: auto/legacy/cdi) that control how GPU access is granted to containers
- **k3s Wrapper**: Modified k3s entrypoint that applies DNS fixes, mounts kernel filesystems, and generates CDI specs before starting the k3s process
