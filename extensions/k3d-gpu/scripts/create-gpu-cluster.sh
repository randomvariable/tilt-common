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

# Create a k3d cluster with NVIDIA GPU support using a custom node image.
# This script builds a custom k3s image with nvidia-container-toolkit and
# creates a k3d cluster configured for GPU workloads.
#
# Usage:
#   ./create-gpu-cluster.sh [options]
#
# Options:
#   --name NAME           Cluster name (default: gpu-dev)
#   --k3s-version VERSION k3s version (default: v1.31.6+k3s1)
#   --api-port PORT       Kubernetes API port (default: 6550)
#   --registry-port PORT  Local registry port (default: 5005)
#   --skip-build          Skip building the custom image (use existing)
#   --help                Show this help message

set -euo pipefail

# Default configuration
CLUSTER_NAME="${K3D_GPU_CLUSTER_NAME:-gpu-dev}"
K3S_VERSION="${K3D_GPU_K3S_VERSION:-v1.31.6+k3s1}"
API_PORT="${K3D_GPU_API_PORT:-6550}"
REGISTRY_PORT="${K3D_GPU_REGISTRY_PORT:-5005}"
SKIP_BUILD=false

# Derived values
IMAGE_TAG="k3s-gpu:${K3S_VERSION}-local"
REGISTRY_NAME="${CLUSTER_NAME}-registry"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSETS_DIR="${SCRIPT_DIR}/../assets/cluster"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*" >&2
}

show_help() {
    sed -n '/^# Usage:/,/^$/p' "$0" | sed 's/^# \?//'
    exit 0
}

# Parse command-line arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --name)
            CLUSTER_NAME="$2"
            shift 2
            ;;
        --k3s-version)
            K3S_VERSION="$2"
            IMAGE_TAG="k3s-gpu:${K3S_VERSION}-local"
            shift 2
            ;;
        --api-port)
            API_PORT="$2"
            shift 2
            ;;
        --registry-port)
            REGISTRY_PORT="$2"
            shift 2
            ;;
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        --help)
            show_help
            ;;
        *)
            log_error "Unknown option: $1"
            show_help
            ;;
    esac
done

# Validate prerequisites
check_prerequisites() {
    local missing=()

    for cmd in docker k3d kubectl; do
        if ! command -v "$cmd" &> /dev/null; then
            missing+=("$cmd")
        fi
    done

    if [ ${#missing[@]} -gt 0 ]; then
        log_error "Missing required tools: ${missing[*]}"
        log_error "Please install them before running this script."
        exit 1
    fi

    # Check for nvidia-ctk (optional but recommended)
    if ! command -v nvidia-ctk &> /dev/null; then
        log_warn "nvidia-ctk not found. GPU support may not work correctly."
        log_warn "Install nvidia-container-toolkit: https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html"
    fi
}

# Check if cluster already exists
cluster_exists() {
    k3d cluster list | grep -q "^${CLUSTER_NAME} "
}

# Build custom k3s GPU image
build_gpu_image() {
    if [ "$SKIP_BUILD" = true ]; then
        log_info "Skipping image build (--skip-build)"

        # Verify image exists
        if ! docker image inspect "$IMAGE_TAG" &> /dev/null; then
            log_error "Image $IMAGE_TAG not found and --skip-build specified"
            exit 1
        fi

        log_info "Using existing image: $IMAGE_TAG"
        return 0
    fi

    if [ ! -d "$ASSETS_DIR" ]; then
        log_error "Assets directory not found: $ASSETS_DIR"
        exit 1
    fi

    log_info "Building custom k3s GPU image: $IMAGE_TAG"
    log_info "k3s version: $K3S_VERSION"

    docker build \
        --build-arg "K3S_VERSION=${K3S_VERSION}" \
        -t "$IMAGE_TAG" \
        -f "${ASSETS_DIR}/Dockerfile" \
        "$ASSETS_DIR"

    log_success "Image built successfully: $IMAGE_TAG"
}

# Create k3d cluster
create_cluster() {
    log_info "Creating k3d cluster: $CLUSTER_NAME"
    log_info "API port: $API_PORT"
    log_info "Registry: ${REGISTRY_NAME}:${REGISTRY_PORT}"

    k3d cluster create "$CLUSTER_NAME" \
        --api-port "$API_PORT" \
        --registry-create "${REGISTRY_NAME}:0.0.0.0:${REGISTRY_PORT}" \
        --gpus=all \
        --image="$IMAGE_TAG" \
        --wait

    log_success "Cluster created successfully"
}

# Wait for cluster to be ready
wait_for_cluster() {
    local context="k3d-${CLUSTER_NAME}"

    log_info "Switching kubectl context to: $context"
    kubectl config use-context "$context"

    log_info "Waiting for nodes to be ready..."
    kubectl wait --for=condition=Ready nodes --all --timeout=120s

    log_info "Waiting for device plugin to be ready..."
    kubectl rollout status daemonset/nvidia-device-plugin-daemonset -n kube-system --timeout=120s || {
        log_warn "Device plugin not ready yet. This may be normal if GPUs are not available."
    }
}

# Print cluster summary
print_summary() {
    local context="k3d-${CLUSTER_NAME}"

    echo
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  k3d GPU Cluster Ready"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo
    echo "  Cluster:   $CLUSTER_NAME"
    echo "  Context:   $context"
    echo "  API Port:  $API_PORT"
    echo "  Registry:  localhost:${REGISTRY_PORT}"
    echo "  Image:     $IMAGE_TAG"
    echo
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo
    echo "Next steps:"
    echo
    echo "  1. Verify GPU resources:"
    echo "     kubectl get nodes -o json | jq '.items[].status.allocatable'"
    echo
    echo "  2. Create a Tiltfile with GPU support:"
    echo "     load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')"
    echo "     enable_gpu_support()"
    echo
    echo "  3. Start Tilt:"
    echo "     tilt up"
    echo
}

# Main execution
main() {
    log_info "k3d GPU Cluster Setup"
    echo

    check_prerequisites

    if cluster_exists; then
        log_warn "Cluster '$CLUSTER_NAME' already exists"
        log_info "Delete it first with: k3d cluster delete $CLUSTER_NAME"
        exit 1
    fi

    build_gpu_image
    create_cluster
    wait_for_cluster
    print_summary

    log_success "Setup complete!"
}

main
