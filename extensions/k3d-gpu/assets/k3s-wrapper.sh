#!/usr/bin/env bash
# Copyright 2026 Naadir Jeewa
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

# k3s Wrapper Script for GPU Support
# This script runs before k3s starts to:
# 1. Fix DNS resolution for Docker 29+
# 2. Mount debugfs/tracefs for eBPF profiling
# 3. Generate CDI specs for WSL2 GPU access
# 4. Configure nvidia-container-runtime mode

set -euo pipefail

# Enable debug output for troubleshooting when DEBUG is set
if [[ "${DEBUG:-}" =~ ^([Tt][Rr][Uu][Ee]|1)$ ]]; then
    set -x
fi

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[k3d-gpu]${NC} $*" >&2
}

log_warn() {
    echo -e "${YELLOW}[k3d-gpu]${NC} $*" >&2
}

log_error() {
    echo -e "${RED}[k3d-gpu ERROR]${NC} $*" >&2
}

# ============================================================================
# DNS Fix (Docker 29+)
# ============================================================================

fix_dns_resolution() {
    log_info "Fixing DNS resolution for Docker 29+..."

    # Extract search domains from original resolv.conf
    local search_line
    search_line=$(grep '^search' /etc/resolv.conf 2>/dev/null || true)

    # Write new resolv.conf with working nameservers
    cat > /etc/resolv.conf <<EOF
nameserver 127.0.0.11    # Docker embedded DNS (for container name resolution)
nameserver 9.9.9.9       # Quad9 public DNS (primary fallback)
nameserver 1.1.1.1       # Cloudflare public DNS (secondary fallback)
${search_line}
options ndots:0
EOF

    log_info "DNS resolv.conf updated with Docker embedded DNS + public fallbacks"
}

# ============================================================================
# eBPF Filesystem Mounts
# ============================================================================

mount_ebpf_filesystems() {
    log_info "Mounting debugfs and tracefs for eBPF profiling..."

    # Mount debugfs at /sys/kernel/debug (for eBPF maps)
    if ! mountpoint -q /sys/kernel/debug 2>/dev/null; then
        mount -t debugfs debugfs /sys/kernel/debug 2>/dev/null || log_warn "Failed to mount debugfs"
    else
        log_info "debugfs already mounted"
    fi

    # Mount tracefs at /sys/kernel/tracing (for tracepoints)
    if ! mountpoint -q /sys/kernel/tracing 2>/dev/null; then
        mount -t tracefs tracefs /sys/kernel/tracing 2>/dev/null || log_warn "Failed to mount tracefs"
    else
        log_info "tracefs already mounted"
    fi

    log_info "eBPF filesystems mounted successfully"
    return 0
}

# ============================================================================
# NVIDIA Prerequisites Check
# ============================================================================

check_nvidia_prerequisites() {
    log_info "Checking NVIDIA prerequisites..."

    # Check for nvidia-ctk command
    if ! command -v nvidia-ctk &> /dev/null; then
        log_error "nvidia-ctk not found!"
        log_error "Please install nvidia-container-toolkit:"
        log_error "  Arch: sudo pacman -S nvidia-container-toolkit"
        log_error "  Ubuntu/Debian: https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html"
        return 1
    fi

    # Check for nvidia-container-cli command
    if ! command -v nvidia-container-cli &> /dev/null; then
        log_warn "nvidia-container-cli not found, GPU UUID extraction may fail"
    fi

    log_info "NVIDIA prerequisites check passed"
    return 0
}

# ============================================================================
# WSL2 CDI Spec Generation
# ============================================================================

generate_wsl2_cdi_spec() {
    log_info "Generating CDI spec for WSL2..."

    # Check if running on WSL2 (/dev/dxg exists)
    if [ ! -e /dev/dxg ]; then
        log_info "Not running on WSL2 (/dev/dxg not found), skipping CDI generation"
        return 0
    fi

    log_info "WSL2 detected, generating CDI spec..."

    # Detect driver store path
    local driver_store
    driver_store=$(find /usr/lib/wsl/drivers -type d -name "nv_dispi*" 2>/dev/null | head -1 || true)
    if [ -z "${driver_store}" ]; then
        log_error "WSL2 driver store not found in /usr/lib/wsl/drivers"
        return 1
    fi
    log_info "Driver store found: ${driver_store}"

    # Detect libdxcore.so path
    local libdxcore_path
    libdxcore_path=$(find /usr/lib -name "libdxcore.so*" 2>/dev/null | head -1 || true)
    if [ -z "${libdxcore_path}" ]; then
        log_error "libdxcore.so not found, WSL2 GPU access will fail"
        return 1
    fi
    log_info "libdxcore.so found: ${libdxcore_path}"

    # Get libdxcore directory for ldcache update
    local libdxcore_dir
    libdxcore_dir=$(dirname "${libdxcore_path}")

    # Extract GPU UUID
    local gpu_uuid
    if command -v nvidia-container-cli &> /dev/null; then
        gpu_uuid=$(nvidia-container-cli info 2>/dev/null | grep "Device Index" -A 5 | grep "UUID:" | head -1 | awk '{print $2}' || true)
        if [ -z "${gpu_uuid}" ]; then
            log_warn "GPU UUID not found, using device '0' only"
            gpu_uuid="GPU-UUID-NOT-DETECTED"
        fi
    else
        log_warn "nvidia-container-cli not found, unable to detect GPU UUID; using device '0' only"
        gpu_uuid="GPU-UUID-NOT-DETECTED"
    fi
    log_info "GPU UUID: ${gpu_uuid}"

    # Create CDI directory
    mkdir -p /etc/cdi

    # Generate CDI spec
    cat > /etc/cdi/nvidia.yaml <<EOF
cdiVersion: 0.5.0
kind: nvidia.com/gpu
devices:
  - name: all
  - name: "0"
  - name: ${gpu_uuid}
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
    - hookName: createContainer
      path: /usr/bin/nvidia-cdi-hook
      args:
        - nvidia-cdi-hook
        - update-ldcache
        - --folder
        - ${driver_store}
        - --folder
        - ${libdxcore_dir}
  mounts:
    - hostPath: /dev/dxg
      containerPath: /dev/dxg
    - hostPath: ${driver_store}
      containerPath: ${driver_store}
      options:
        - ro
    - hostPath: ${libdxcore_path}
      containerPath: ${libdxcore_path}
      options:
        - ro
EOF

    log_info "CDI spec generated at /etc/cdi/nvidia.yaml"
    return 0
}

# ============================================================================
# NVIDIA Container Runtime Configuration
# ============================================================================

configure_nvidia_runtime() {
    log_info "Configuring nvidia-container-runtime..."

    local config_file="/etc/nvidia-container-runtime/config.toml"

    # Check if config file exists
    if [ ! -f "${config_file}" ]; then
        log_warn "Runtime config not found at ${config_file}, skipping configuration"
        return 0
    fi

    # Check if running on WSL2
    if [ -e /dev/dxg ]; then
        # WSL2: Use CDI mode
        log_info "Setting runtime mode to 'cdi' for WSL2"
        sed -i 's/^mode = "auto"/mode = "cdi"/' "${config_file}" 2>/dev/null || true
        sed -i 's/^mode = "legacy"/mode = "cdi"/' "${config_file}" 2>/dev/null || true
    else
        # Native Linux: Use auto mode (standard NVIDIA runtime)
        log_info "Native Linux detected, keeping runtime in 'auto' or 'legacy' mode"
        # No changes needed for native Linux
    fi

    log_info "Runtime configuration complete"
    return 0
}

# ============================================================================
# Main Execution
# ============================================================================

main() {
    log_info "k3s GPU wrapper script starting..."

    # Run all setup tasks
    fix_dns_resolution || log_warn "DNS fix failed, continuing anyway"
    mount_ebpf_filesystems || log_warn "eBPF filesystem mounting failed, profiling may not work"
    check_nvidia_prerequisites || log_error "NVIDIA prerequisites check failed"
    generate_wsl2_cdi_spec || log_warn "CDI spec generation failed (expected on native Linux)"
    configure_nvidia_runtime || log_warn "Runtime configuration failed, continuing anyway"

    log_info "GPU setup complete, starting k3s..."

    # Execute actual k3s process (replace current process)
    if [ -x /usr/local/bin/k3s.real ]; then
        exec /usr/local/bin/k3s.real "$@"
    elif [ -x /usr/bin/k3s.real ]; then
        exec /usr/bin/k3s.real "$@"
    else
        log_error "k3s.real not found, cannot start k3s"
        return 1
    fi
}

# Run main function with all script arguments
main "$@"
