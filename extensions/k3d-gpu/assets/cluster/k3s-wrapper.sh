#!/bin/bash
# Wrapper around the k3s binary that applies runtime fixes before starting.
#
# Fix 1: DNS — Docker 29+ sets the bridge gateway IP as the nameserver.
# After k3s starts, its iptables-nft rules interfere with Docker's DNS proxy,
# causing containerd image pull failures. Replace with direct public resolvers.
#
# Fix 2: CDI — On WSL2, GPUs are exposed via /dev/dxg and the WSL driver store
# instead of traditional /dev/nvidia* devices. Generate a CDI spec so the
# nvidia-container-runtime can discover and inject GPU devices into pods.
# The auto-generated spec is augmented with libdxcore.so which nvidia-ctk
# misses but NVML requires on WSL2.
#
# Fix 3: nvidia-container-runtime mode — Force CDI mode instead of legacy.
# Legacy mode tries to use nvidia-container-cli which has incomplete WSL2
# library injection (missing libdxcore.so). CDI mode uses the spec we
# generate with all required mounts and hooks.

# --- Fix 1: DNS ---
# Docker 29+ writes the bridge gateway IP as the nameserver in resolv.conf.
# k3s's iptables-nft rules break that gateway DNS proxy. Replace with:
#   1. Docker's embedded DNS (127.0.0.11) — still works on loopback, needed for
#      resolving Docker network container names (e.g. the k3d local registry).
#   2. Public resolvers as fallback for external lookups.
search_line=$(grep '^search' /etc/resolv.conf 2>/dev/null || true)

cat > /etc/resolv.conf <<EOF
nameserver 127.0.0.11
nameserver 9.9.9.9
nameserver 1.1.1.1
${search_line}
options ndots:0
EOF

# --- Fix 1b: Mount debugfs and tracefs ---
# These aren't auto-mounted inside Docker containers but parca-agent needs them
# for eBPF tracepoints. The k3d node runs privileged so we can mount them.
mount -t debugfs debugfs /sys/kernel/debug 2>/dev/null || true
mount -t tracefs tracefs /sys/kernel/tracing 2>/dev/null || true

# --- Fix 2 & 3: CDI spec + runtime mode ---
if command -v nvidia-ctk >/dev/null 2>&1; then
    mkdir -p /etc/cdi

    # Generate base CDI spec from nvidia-ctk (auto-detects WSL mode).
    nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml 2>/dev/null || true

    # nvidia-ctk's WSL CDI spec is missing libdxcore.so (the DXG bridge lib).
    # Also, the generated spec only has device "all" but the k8s device plugin
    # assigns GPUs by UUID. We regenerate a complete spec with all fixes.
    if [ -f /etc/cdi/nvidia.yaml ] && [ -e /dev/dxg ]; then
        # Extract GPU UUID from nvidia-container-cli info.
        gpu_uuid=$(nvidia-container-cli info 2>/dev/null | grep "GPU UUID" | awk '{print $NF}')

        # Find the WSL driver store path (prefer the nv_dispi driver).
        driver_store=$(find /usr/lib/wsl/drivers -maxdepth 1 -name "nv_dispi*" -type d 2>/dev/null | head -1)

        # Find libdxcore.so (Docker injects it into the container).
        libdxcore=$(find /usr/lib -name "libdxcore.so" -type f 2>/dev/null | head -1)

        if [ -n "${driver_store}" ]; then
            # Build mount entries for all files in the driver store.
            mount_entries=""
            for f in "${driver_store}"/*; do
                [ -f "$f" ] || continue
                mount_entries="${mount_entries}
        - hostPath: ${f}
          containerPath: ${f}
          options: [ro, nosuid, nodev, rbind, rprivate]"
            done

            # Add libdxcore.so mount if found.
            if [ -n "${libdxcore}" ]; then
                mount_entries="${mount_entries}
        - hostPath: ${libdxcore}
          containerPath: ${libdxcore}
          options: [ro, nosuid, nodev, rbind, rprivate]"
                ldcache_extra="
            - --folder
            - $(dirname "${libdxcore}")"
            fi

            # Build device entries.
            device_entries="    - name: all
      containerEdits:
        deviceNodes:
            - path: /dev/dxg
    - name: \"0\"
      containerEdits:
        deviceNodes:
            - path: /dev/dxg"

            if [ -n "${gpu_uuid}" ]; then
                device_entries="${device_entries}
    - name: ${gpu_uuid}
      containerEdits:
        deviceNodes:
            - path: /dev/dxg"
            fi

            cat > /etc/cdi/nvidia.yaml <<CDIEOF
---
cdiVersion: 0.5.0
kind: nvidia.com/gpu
devices:
${device_entries}
containerEdits:
    env:
        - NVIDIA_VISIBLE_DEVICES=void
    hooks:
        - hookName: createContainer
          path: /usr/bin/nvidia-cdi-hook
          args:
            - nvidia-cdi-hook
            - create-symlinks
            - --link
            - ${driver_store}/nvidia-smi::/usr/bin/nvidia-smi
          env:
            - NVIDIA_CTK_DEBUG=false
        - hookName: createContainer
          path: /usr/bin/nvidia-cdi-hook
          args:
            - nvidia-cdi-hook
            - update-ldcache
            - --folder
            - ${driver_store}${ldcache_extra}
          env:
            - NVIDIA_CTK_DEBUG=false
    mounts:${mount_entries}
CDIEOF
        fi
    fi

    # Switch nvidia-container-runtime to CDI mode. Legacy mode has incomplete
    # WSL2 support (missing libdxcore.so injection).
    if [ -f /etc/nvidia-container-runtime/config.toml ]; then
        sed -i 's/^mode = "auto"/mode = "cdi"/' \
            /etc/nvidia-container-runtime/config.toml
    fi
fi

exec /bin/k3s.real "$@"
