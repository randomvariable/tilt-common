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

# Config validation test: Verify invalid configurations produce clear errors
# This test ensures the extension validates input and provides helpful error messages

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
TEST_DIR="${SCRIPT_DIR}/tmp-test-config"

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

echo -e "${YELLOW}=== Config Validation Test: Invalid Configurations ===${NC}"

# Create test directory
mkdir -p "${TEST_DIR}"

# Test 1: Invalid timesharing_replicas (negative number)
cat > "${TEST_DIR}/Tiltfile.invalid-timesharing" <<'EOF'
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')
enable_gpu_support({
    'timesharing_replicas': -1,
    'skip_validation': True,
})
EOF

# Test 2: Invalid namespace (contains uppercase)
cat > "${TEST_DIR}/Tiltfile.invalid-namespace" <<'EOF'
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')
enable_gpu_support({
    'namespace': 'Invalid-Namespace',
    'skip_validation': True,
})
EOF

# Test 3: Invalid timesharing_replicas (too high)
cat > "${TEST_DIR}/Tiltfile.invalid-replicas-high" <<'EOF'
load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')
enable_gpu_support({
    'timesharing_replicas': 101,
    'skip_validation': True,
})
EOF

# Check if tilt is available
if ! command -v tilt &> /dev/null; then
    echo -e "${YELLOW}⚠ SKIP${NC}: tilt command not found"
    echo -e "${YELLOW}  Test would verify invalid configs produce clear errors${NC}"
    exit 0
fi

PASS_COUNT=0
FAIL_COUNT=0

# Run tests
for tiltfile in "${TEST_DIR}"/Tiltfile.*; then
    test_name=$(basename "${tiltfile}" | sed 's/Tiltfile\.//')
    cd "${TEST_DIR}"

    if tilt dump -f "$(basename "${tiltfile}")" 2>&1 | grep -qi "error\|invalid\|must be"; then
        echo -e "${GREEN}✓ PASS${NC}: ${test_name} - Error detected as expected"
        ((PASS_COUNT++))
    else
        echo -e "${RED}✗ FAIL${NC}: ${test_name} - Should have failed with error"
        ((FAIL_COUNT++))
    fi
done

echo ""
echo -e "Results: ${GREEN}${PASS_COUNT} passed${NC}, ${RED}${FAIL_COUNT} failed${NC}"

if [ ${FAIL_COUNT} -gt 0 ]; then
    exit 1
fi

exit 0
