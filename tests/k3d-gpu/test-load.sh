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

# Smoke test: Verify k3d-gpu extension loads without errors
# This test creates a minimal Tiltfile that loads the extension and verifies it doesn't fail

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
TEST_DIR="${SCRIPT_DIR}/tmp-test-load"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

cleanup() {
    if [ -d "${TEST_DIR}" ]; then
        rm -rf "${TEST_DIR}"
    fi
}

trap cleanup EXIT

echo -e "${YELLOW}=== Smoke Test: Extension Load ===${NC}"

# Create test directory
mkdir -p "${TEST_DIR}"

# Create test Tiltfile
cat > "${TEST_DIR}/Tiltfile" <<'EOF'
# Test Tiltfile for smoke testing k3d-gpu extension load
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')

# This should load without errors
print("k3d-gpu extension loaded successfully")
EOF

# Try to validate Tiltfile syntax with tilt
cd "${TEST_DIR}"
if command -v tilt &> /dev/null; then
    if tilt dump 2>&1 | grep -q "k3d-gpu extension loaded successfully"; then
        echo -e "${GREEN}✓ PASS${NC}: Extension loads without errors"
        exit 0
    else
        echo -e "${RED}✗ FAIL${NC}: Extension failed to load or print expected message"
        exit 1
    fi
else
    echo -e "${YELLOW}⚠ SKIP${NC}: tilt command not found, skipping actual load test"
    echo -e "${YELLOW}  Test would verify extension loads without errors${NC}"
    exit 0
fi
