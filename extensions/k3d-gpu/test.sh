#!/usr/bin/env bash
# Main test runner for k3d-gpu extension
# Runs all tests via tilt ci and reports results

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
TEST_DIR="${REPO_ROOT}/tests/k3d-gpu"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  k3d-gpu Extension Test Suite                                 ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Check if test directory exists
if [ ! -d "${TEST_DIR}" ]; then
    echo -e "${RED}✗ FAIL${NC}: Test directory not found: ${TEST_DIR}"
    exit 1
fi

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

# Function to run a test script
run_test() {
    local test_script="$1"
    local test_name="$2"

    echo -e "${YELLOW}Running:${NC} ${test_name}"

    if [ ! -f "${test_script}" ]; then
        echo -e "${RED}✗ FAIL${NC}: Test script not found: ${test_script}"
        ((FAIL_COUNT++))
        return
    fi

    if [ ! -x "${test_script}" ]; then
        chmod +x "${test_script}"
    fi

    if "${test_script}"; then
        if grep -q "SKIP" "${test_script}" 2>/dev/null && "${test_script}" 2>&1 | grep -q "SKIP"; then
            echo -e "${YELLOW}⚠ SKIP${NC}: ${test_name}"
            ((SKIP_COUNT++))
        else
            echo -e "${GREEN}✓ PASS${NC}: ${test_name}"
            ((PASS_COUNT++))
        fi
    else
        echo -e "${RED}✗ FAIL${NC}: ${test_name}"
        ((FAIL_COUNT++))
    fi
    echo ""
}

# Run test suite
echo -e "${BLUE}Phase 1: Smoke Tests${NC}"
run_test "${TEST_DIR}/test-load.sh" "Extension Load Test"

echo -e "${BLUE}Phase 2: Output Validation${NC}"
run_test "${TEST_DIR}/test-output-validation.sh" "YAML Generation Test"

echo -e "${BLUE}Phase 3: Configuration Validation${NC}"
run_test "${TEST_DIR}/test-config-validation.sh" "Invalid Config Test"

# Summary
echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  Test Results                                                  ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  ${GREEN}✓ Passed:${NC}  ${PASS_COUNT}"
echo -e "  ${RED}✗ Failed:${NC}  ${FAIL_COUNT}"
echo -e "  ${YELLOW}⚠ Skipped:${NC} ${SKIP_COUNT}"
echo ""

if [ ${FAIL_COUNT} -gt 0 ]; then
    echo -e "${RED}Test suite FAILED${NC}"
    exit 1
else
    echo -e "${GREEN}Test suite PASSED${NC}"
    exit 0
fi
