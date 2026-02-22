#!/usr/bin/env bash
# Copyright 2026 Naadir Jeewa
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-tilt-common-e2e}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "==> Creating kind cluster: ${CLUSTER_NAME}"
if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    echo "Cluster ${CLUSTER_NAME} already exists, deleting..."
    kind delete cluster --name "${CLUSTER_NAME}"
fi

# Create simple kind cluster with a local registry
cat <<EOF | kind create cluster --name "${CLUSTER_NAME}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors]
      [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:5005"]
        endpoint = ["http://kind-registry:5000"]
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30000
    hostPort: 30000
    protocol: TCP
EOF

echo "==> Creating local registry"
# Create registry container if it doesn't exist
if ! docker ps | grep -q kind-registry; then
    if docker ps -a | grep -q kind-registry; then
        docker start kind-registry
    else
        docker run -d --restart=always -p "5005:5000" --name kind-registry registry:2
    fi
fi

# Connect registry to kind network if not already connected
if ! docker network inspect kind | grep -q kind-registry; then
    docker network connect kind kind-registry || true
fi

echo "==> Waiting for cluster to be ready"
kubectl wait --for=condition=Ready nodes --all --timeout=120s

echo "==> Deploying observability stack with Tilt"
cd "${SCRIPT_DIR}/e2e"
tilt ci --timeout=5m

echo "==> Running E2E tests"
cd "${PROJECT_ROOT}"
go test -v -timeout=15m -tags=e2e ./tests/observability/... -run TestE2E

echo "==> E2E tests completed successfully"
