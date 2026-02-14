# Research: k3s GPU Support Extension

**Date**: 2026-02-14
**Purpose**: Research technical approaches for GPU support in k3s, focusing on WSL2 compatibility

## Research Areas

### 1. CDI (Container Device Interface) Spec Format

**Decision**: Use CDI mode for nvidia-container-runtime on WSL2 systems

**Rationale**:
- Legacy mode nvidia-container-cli has incomplete WSL2 support (missing libdxcore.so injection)
- CDI mode allows explicit declaration of all required devices, mounts, and hooks
- WSL2 exposes GPUs via /dev/dxg (not traditional /dev/nvidia* devices)
- nvidia-ctk can auto-generate base CDI specs but requires augmentation for WSL2

**CDI Spec Structure** (from memex/dev/k3d-gpu/k3s-wrapper.sh):
```yaml
cdiVersion: 0.5.0
kind: nvidia.com/gpu
devices:
  - name: all                    # Catch-all device
  - name: "0"                    # Device by number
  - name: ${GPU_UUID}            # Device by UUID (for k8s device plugin)
containerEdits:
  env:
    - NVIDIA_VISIBLE_DEVICES=void
  hooks:
    - hookName: createContainer
      path: /usr/bin/nvidia-cdi-hook
      args: [nvidia-cdi-hook, create-symlinks, --link, ${DRIVER_STORE}/nvidia-smi::/usr/bin/nvidia-smi]
    - hookName: createContainer
      path: /usr/bin/nvidia-cdi-hook
      args: [nvidia-cdi-hook, update-ldcache, --folder, ${DRIVER_STORE}, --folder, ${LIBDXCORE_DIR}]
  mounts:
    - hostPath: /dev/dxg
      containerPath: /dev/dxg
    - hostPath: ${DRIVER_STORE}/*
      containerPath: ${DRIVER_STORE}/*
    - hostPath: ${LIBDXCORE_PATH}
      containerPath: ${LIBDXCORE_PATH}
```

**Key WSL2 Requirements**:
- Mount /dev/dxg device
- Mount WSL driver store (find /usr/lib/wsl/drivers -name "nv_dispi*")
- Mount libdxcore.so (missing from nvidia-ctk auto-generation)
- Update ldcache with both driver store and libdxcore directory

**Alternatives Considered**:
- **Legacy mode**: Simple but incomplete WSL2 support, requires /dev/nvidia* devices
- **Auto mode**: Attempts to detect correct mode but falls back to legacy on WSL2 (fails)
- **Manual device mounts**: Fragile, requires tracking driver updates, error-prone

**References**: ../memex/dev/k3d-gpu/k3s-wrapper.sh lines 41-140

### 2. NVIDIA Device Plugin Configuration

**Decision**: Deploy standard NVIDIA k8s device plugin DaemonSet with minimal configuration

**Rationale**:
- Official NVIDIA device plugin is mature and well-maintained
- Automatically discovers GPUs and advertises them as `nvidia.com/gpu` resources
- Works with both CDI and legacy nvidia-container-runtime modes
- Requires privileged DaemonSet to access /dev and host paths

**Device Plugin Manifest** (from memex/dev/k8s/nvidia-device-plugin.yaml):
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: nvidia-device-plugin-daemonset
  namespace: kube-system
spec:
  selector:
    matchLabels:
      name: nvidia-device-plugin-ds
  template:
    spec:
      tolerations:
      - key: nvidia.com/gpu
        operator: Exists
        effect: NoSchedule
      containers:
      - image: nvcr.io/nvidia/k8s-device-plugin:v0.14.3
        name: nvidia-device-plugin-ctr
        args:
        - "--sharing-strategy=time-slicing"
        - "--replicas=4"  # Report 4x GPUs for timesharing (memex default)
        env:
        - name: NVIDIA_MIG_MONITOR_DEVICES
          value: "all"
        securityContext:
          privileged: true
        volumeMounts:
        - name: device-plugin
          mountPath: /var/lib/kubelet/device-plugins
      volumes:
      - name: device-plugin
        hostPath:
          path: /var/lib/kubelet/device-plugins
```

**Configuration Notes**:
- Uses privileged security context (required for device access)
- Mounts kubelet device-plugins directory for registration
- Tolerations allow scheduling on GPU nodes
- Image version should be configurable (default to latest stable)
- **Timesharing enabled by default**: `--sharing-strategy=time-slicing --replicas=4`
  - Reports 4 GPUs per physical GPU (4 pods can share)
  - Configurable via `timesharing_replicas` parameter
  - Ideal for development where workloads don't fully utilize GPU

**Alternatives Considered**:
- **Custom device plugin**: More control but high maintenance burden
- **Static resource advertising**: Simple but doesn't handle multi-GPU or GPU failures
- **MIG (Multi-Instance GPU) support**: Overkill for local development

**References**: ../memex/dev/k3d-gpu/device-plugin-daemonset.yaml

### 3. DNS Resolution Fixes for Docker 29+

**Decision**: Replace Docker bridge gateway nameserver with working resolvers in k3s node containers

**Rationale**:
- Docker 29+ writes bridge gateway IP (e.g., 172.17.0.1) as nameserver in /etc/resolv.conf
- k3s iptables-nft rules interfere with Docker's DNS proxy at the gateway IP
- Causes containerd image pull failures: "temporary failure in name resolution"
- Fix: Use Docker embedded DNS (127.0.0.11) + public fallbacks (9.9.9.9, 1.1.1.1)

**DNS Configuration** (from memex/dev/k3d-gpu/k3s-wrapper.sh lines 19-33):
```bash
# Extract search domains from original resolv.conf
search_line=$(grep '^search' /etc/resolv.conf 2>/dev/null || true)

cat > /etc/resolv.conf <<EOF
nameserver 127.0.0.11    # Docker embedded DNS (for container name resolution)
nameserver 9.9.9.9       # Quad9 public DNS (primary fallback)
nameserver 1.1.1.1       # Cloudflare public DNS (secondary fallback)
${search_line}
options ndots:0
EOF
```

**Why these nameservers**:
- **127.0.0.11**: Docker embedded DNS still works on loopback, resolves container names (e.g., k3d local registry)
- **9.9.9.9**: Public DNS for external domains, privacy-focused
- **1.1.1.1**: Public DNS fallback, fast response times

**Edge Case**: If DNS is already correctly configured, this fix is harmless (duplicate correct config).

**Alternatives Considered**:
- **Preserve bridge gateway DNS**: Doesn't work due to k3s iptables rules
- **Only public DNS**: Breaks Docker network container name resolution (e.g., registry access)
- **systemd-resolved**: Not available in k3s node containers

**References**: ../memex/dev/k3d-gpu/k3s-wrapper.sh lines 19-33

### 4. eBPF Profiling Support (debugfs/tracefs)

**Decision**: Mount debugfs and tracefs in k3s node containers at standard paths

**Rationale**:
- eBPF profilers (parca-agent, bpftrace) require kernel tracing facilities
- debugfs (/sys/kernel/debug) and tracefs (/sys/kernel/tracing) not auto-mounted in containers
- k3d nodes run privileged, so mounting is allowed
- Enables continuous profiling of GPU workloads for performance optimization

**Filesystem Mounts** (from memex/dev/k3d-gpu/k3s-wrapper.sh lines 35-39):
```bash
mount -t debugfs debugfs /sys/kernel/debug 2>/dev/null || true
mount -t tracefs tracefs /sys/kernel/tracing 2>/dev/null || true
```

**Why these paths**:
- **/sys/kernel/debug**: Standard debugfs mount point, required for eBPF maps
- **/sys/kernel/tracing**: Standard tracefs mount point, required for tracepoints

**Error Handling**: Use `|| true` to avoid failing if already mounted or unsupported

**Alternatives Considered**:
- **User-mounted via initContainer**: Requires users to modify their workloads
- **Separate profiling extension**: Violates single-purpose principle for GPU extension
- **No profiling support**: Limits observability for GPU workload optimization

**References**: ../memex/dev/k3d-gpu/k3s-wrapper.sh lines 35-39

### 5. k3s Containerd Runtime Configuration

**Decision**: Configure nvidia-container-runtime to use CDI mode via config.toml

**Rationale**:
- containerd uses nvidia-container-runtime to inject GPU devices
- Runtime mode (auto/legacy/cdi) controls injection mechanism
- CDI mode reads from /etc/cdi/*.yaml specs
- Must set mode before k3s starts (done in wrapper script)

**Runtime Configuration** (from memex/dev/k3d-gpu/k3s-wrapper.sh lines 134-139):
```bash
if [ -f /etc/nvidia-container-runtime/config.toml ]; then
    sed -i 's/^mode = "auto"/mode = "cdi"/' \
        /etc/nvidia-container-runtime/config.toml
fi
```

**Config Location**: /etc/nvidia-container-runtime/config.toml

**Why CDI mode on WSL2**:
- Auto mode falls back to legacy on WSL2 (incomplete support)
- Legacy mode uses nvidia-container-cli which misses libdxcore.so
- CDI mode uses explicit specs we generate with all required mounts

**Native Linux**: Can use auto or legacy mode (standard /dev/nvidia* devices available)

**Alternatives Considered**:
- **Leave auto mode**: Works on native Linux but fails on WSL2
- **Always use legacy**: Simpler but doesn't work on WSL2
- **Dual configuration files**: Complex, error-prone

**References**: ../memex/dev/k3d-gpu/k3s-wrapper.sh lines 134-139

## Implementation Strategy

### Extraction from memex

The k3d-gpu extension will extract and modularize these components from memex:

1. **k3s-wrapper.sh** (memex/dev/k3d-gpu/k3s-wrapper.sh):
   - Extract DNS fix, debugfs/tracefs mounting, CDI generation
   - Make cluster name and device detection configurable
   - Add error handling for missing nvidia-ctk

2. **Device plugin YAML** (memex/dev/k8s/nvidia-device-plugin.yaml):
   - Copy manifest template
   - Add configuration for namespace, image version
   - Support both k3d and standalone k3s via selectors

3. **Tilt integration** (memex/Tiltfile lines 352-365):
   - Extract pattern for conditional GPU enablement
   - Generalize resource labeling and dependencies
   - Add configuration validation

### Testing Approach

**Smoke Tests**:
1. Load extension in test Tiltfile → no errors
2. Generate device plugin YAML → valid Kubernetes manifest
3. Execute k3s-wrapper.sh in test container → exits successfully

**Output Validation**:
1. Compare generated device plugin YAML to golden file
2. Compare WSL2 CDI spec to expected structure
3. Verify DNS resolv.conf contains correct nameservers

**Configuration Validation**:
1. Invalid cluster name → clear error with remediation
2. Missing nvidia-ctk → error with installation instructions
3. Unsupported platform (macOS) → error with platform requirements

## Open Questions

None. All research completed based on proven memex implementation.

## References

- **memex source**: ../memex/dev/k3d-gpu/, ../memex/Tiltfile
- **CDI specification**: https://github.com/cncf-tags/container-device-interface/blob/main/SPEC.md
- **NVIDIA device plugin**: https://github.com/NVIDIA/k8s-device-plugin
- **NVIDIA container toolkit**: https://github.com/NVIDIA/nvidia-container-toolkit
