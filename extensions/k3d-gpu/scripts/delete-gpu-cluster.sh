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

# Delete a k3d GPU cluster created by create-gpu-cluster.sh
#
# Usage:
#   ./delete-gpu-cluster.sh [options]
#
# Options:
#   --name NAME    Cluster name (default: gpu-dev)
#   --help         Show this help message

set -euo pipefail

# Default configuration
CLUSTER_NAME="${K3D_GPU_CLUSTER_NAME:-gpu-dev}"

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
        --help)
            show_help
            ;;
        *)
            log_error "Unknown option: $1"
            show_help
            ;;
    esac
done

# Check prerequisites
if ! command -v k3d &> /dev/null; then
    log_error "k3d not found. Please install k3d first."
    exit 1
fi

# Check if cluster exists
if ! k3d cluster list | grep -q "^${CLUSTER_NAME} "; then
    log_warn "Cluster '$CLUSTER_NAME' does not exist"
    exit 0
fi

# Delete the cluster
log_info "Deleting k3d cluster: $CLUSTER_NAME"

if k3d cluster delete "$CLUSTER_NAME"; then
    log_success "Cluster deleted successfully"
else
    log_error "Failed to delete cluster"
    exit 1
fi

# Verify deletion
if k3d cluster list | grep -q "^${CLUSTER_NAME} "; then
    log_error "Cluster still exists after deletion"
    exit 1
fi

log_success "Cleanup complete!"
