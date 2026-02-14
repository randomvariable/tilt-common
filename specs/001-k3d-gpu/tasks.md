# Tasks: k3s GPU Support Extension

**Input**: Design documents from `/specs/001-k3d-gpu/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Tests are MANDATORY per Constitution Principle V (TDD for Stability)

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Extension**: `extensions/k3d-gpu/` at repository root
- **Examples**: `examples/k3d-gpu/` at repository root
- **Tests**: `tests/k3d-gpu/` at repository root

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [X] T001 Create extensions/k3d-gpu directory structure
- [X] T002 Create extensions/k3d-gpu/assets subdirectory for supporting files
- [X] T003 [P] Create examples/k3d-gpu directory for usage examples
- [X] T004 [P] Create tests/k3d-gpu directory for test suite
- [X] T005 [P] Create tests/k3d-gpu/golden subdirectory for expected outputs
- [X] T006 Add .gitignore for Tilt extension patterns in extensions/k3d-gpu/.gitignore

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Test infrastructure that MUST be complete before ANY user story implementation

**⚠️ CRITICAL**: No user story work can begin until this phase is complete (TDD requirement)

### Test Golden Files (Write Tests First)

- [X] T007 [P] Create golden file for device plugin YAML in tests/k3d-gpu/golden/device-plugin.yaml
- [X] T008 [P] Create golden file for WSL2 CDI spec in tests/k3d-gpu/golden/cdi-spec-wsl2.yaml
- [X] T009 [P] Create golden file for native Linux device plugin in tests/k3d-gpu/golden/device-plugin-native.yaml

### Test Scripts (Write Tests First)

- [X] T010 [P] Create smoke test script in tests/k3d-gpu/test-load.sh (verifies extension loads)
- [X] T011 [P] Create output validation script in tests/k3d-gpu/test-output-validation.sh (compares YAML to golden files)
- [X] T012 [P] Create config validation script in tests/k3d-gpu/test-config-validation.sh (tests invalid configs, timesharing replica bounds: 1, 4, 8, 100, negative values)
- [X] T013 Create main test runner in extensions/k3d-gpu/test.sh (runs all tests via tilt ci)

**Checkpoint**: Test infrastructure ready - all tests should FAIL (RED phase). User story implementation can now begin.

---

## Phase 3: User Story 1 - Enable GPU Workloads in Local k3s (Priority: P1) 🎯 MVP

**Goal**: Core GPU functionality - developers can test GPU workloads locally without manual configuration

**Independent Test**: Load extension in Tiltfile, run `tilt up`, schedule pod with `nvidia.com/gpu: 1`, pod accesses GPU successfully

### Core Extension Implementation

- [X] T014 Create Starlark extension stub in extensions/k3d-gpu/extension.star with enable_gpu_support() function signature
- [X] T015 [US1] Implement system environment detection in extensions/k3d-gpu/extension.star (_detect_system_environment function)
- [X] T016 [US1] Implement WSL2 detection (check /dev/dxg exists) in extensions/k3d-gpu/extension.star
- [X] T017 [US1] Implement configuration validation in extensions/k3d-gpu/extension.star (_validate_config function: cluster name, namespace, feature flags, replica count bounds, validation mode)
- [X] T018 [US1] Implement GPU timesharing configuration (default 4 replicas) in extensions/k3d-gpu/extension.star

### k3s Wrapper Script (WSL2 GPU Access)

- [X] T019 [P] [US1] Create k3s wrapper script skeleton in extensions/k3d-gpu/assets/k3s-wrapper.sh with bash strict mode
- [X] T020 [US1] Implement DNS fix logic in extensions/k3d-gpu/assets/k3s-wrapper.sh (replace bridge gateway with 127.0.0.11 + public DNS)
- [X] T021 [US1] Implement nvidia-ctk presence check in extensions/k3d-gpu/assets/k3s-wrapper.sh
- [X] T022 [US1] Implement WSL2 driver store detection in extensions/k3d-gpu/assets/k3s-wrapper.sh (find /usr/lib/wsl/drivers)
- [X] T023 [US1] Implement libdxcore.so detection in extensions/k3d-gpu/assets/k3s-wrapper.sh (find /usr/lib -name libdxcore.so)
- [X] T024 [US1] Implement GPU UUID extraction in extensions/k3d-gpu/assets/k3s-wrapper.sh (nvidia-container-cli info)
- [X] T025 [US1] Implement CDI spec generation for WSL2 in extensions/k3d-gpu/assets/k3s-wrapper.sh (write /etc/cdi/nvidia.yaml)
- [X] T026 [US1] Implement nvidia-container-runtime mode configuration in extensions/k3d-gpu/assets/k3s-wrapper.sh (set mode=cdi)
- [X] T027 [US1] Add exec to k3s.real in extensions/k3d-gpu/assets/k3s-wrapper.sh (start actual k3s process)

### Device Plugin Deployment

- [X] T028 [P] [US1] Create device plugin DaemonSet manifest template in extensions/k3d-gpu/assets/device-plugin.yaml
- [X] T029 [US1] Add timesharing configuration to device plugin in extensions/k3d-gpu/assets/device-plugin.yaml (--sharing-strategy=time-slicing --replicas=4)
- [X] T030 [US1] Implement device plugin YAML generation in extensions/k3d-gpu/extension.star (_generate_device_plugin_yaml function)
- [X] T031 [US1] Implement Tilt resource registration in extensions/k3d-gpu/extension.star (_deploy_to_tilt function with k8s_yaml)

### Integration and Validation

- [X] T032 [US1] Wire up enable_gpu_support() main function in extensions/k3d-gpu/extension.star (call all internal functions)
- [X] T033 [US1] Add error handling and fail() calls for missing prerequisites in extensions/k3d-gpu/extension.star
- [X] T034 [US1] Run test-load.sh and verify extension loads without errors
- [X] T035 [US1] Run test-output-validation.sh and verify device plugin YAML matches golden file
- [X] T036 [US1] Run test-config-validation.sh and verify invalid configs produce clear errors (extreme replica counts, invalid cluster names, missing prerequisites)

**Checkpoint**: User Story 1 complete - GPU workloads can now schedule on k3s clusters with WSL2 support

---

## Phase 4: User Story 2 - Profile GPU Workloads with eBPF Tools (Priority: P2)

**Goal**: Enable observability for GPU workloads via eBPF profiling tools

**Independent Test**: Deploy parca-agent after enabling extension, profiler successfully attaches tracepoints and collects GPU workload profiles

### eBPF Filesystem Support

- [X] T037 [P] [US2] Implement debugfs mounting in extensions/k3d-gpu/assets/k3s-wrapper.sh (mount -t debugfs)
- [X] T038 [P] [US2] Implement tracefs mounting in extensions/k3d-gpu/assets/k3s-wrapper.sh (mount -t tracefs)
- [X] T039 [US2] Add enable_profiling configuration option to extensions/k3d-gpu/extension.star (default true)
- [X] T040 [US2] Wire up profiling toggle to wrapper script generation in extensions/k3d-gpu/extension.star
- [X] T041 [US2] Create example profiler deployment in examples/k3d-gpu/test-gpu-pod.yaml (pod that tests filesystem access)
- [X] T042 [US2] Verify debugfs and tracefs are accessible from example pod

**Checkpoint**: User Story 2 complete - eBPF profiling tools can now access kernel tracing facilities

---

## Phase 5: User Story 3 - Use GPU Extension on Native Linux (Priority: P3)

**Goal**: Cross-platform support for native Linux systems without WSL2-specific workarounds

**Independent Test**: Load extension on native Linux system, GPU workloads schedule without WSL2 CDI configuration being applied

### Native Linux Detection and Configuration

- [X] T043 [US3] Implement native Linux mode detection in extensions/k3d-gpu/extension.star (absence of /dev/dxg)
- [X] T044 [US3] Add conditional CDI generation (skip on native Linux) in extensions/k3d-gpu/assets/k3s-wrapper.sh
- [X] T045 [US3] Add conditional runtime mode configuration (use auto mode on native) in extensions/k3d-gpu/assets/k3s-wrapper.sh
- [X] T046 [P] [US3] Create golden file for native Linux output in tests/k3d-gpu/golden/device-plugin-native.yaml
- [X] T047 [US3] Add native Linux test scenario to tests/k3d-gpu/test-output-validation.sh
- [X] T048 [US3] Verify extension works correctly on native Linux (or document native Linux testing approach)

**Checkpoint**: All user stories complete - extension supports both WSL2 and native Linux

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, examples, and final validation that affects all user stories

### Documentation

- [X] T049 [P] Create README with quick start in extensions/k3d-gpu/README.md (copy-paste Tiltfile snippet)
- [X] T050 [P] Add configuration options table to extensions/k3d-gpu/README.md (all dict parameters with types, defaults)
- [X] T051 [P] Add prerequisites section to extensions/k3d-gpu/README.md (NVIDIA drivers, toolkit, k3s)
- [X] T052 [P] Add troubleshooting section to extensions/k3d-gpu/README.md (common errors with remediation)
- [X] T053 [P] Add platform support matrix to extensions/k3d-gpu/README.md (WSL2, native Linux, macOS status)

### Examples

- [X] T054 [P] Create basic example Tiltfile in examples/k3d-gpu/Tiltfile (zero-config usage)
- [X] T055 [P] Create advanced example Tiltfile in examples/k3d-gpu/Tiltfile.advanced (custom config, timesharing, profiling)
- [X] T056 [P] Create GPU test pod manifest in examples/k3d-gpu/test-gpu-pod.yaml (requests GPU, runs nvidia-smi)
- [X] T057 [P] Add example with multiple GPU pods in examples/k3d-gpu/Tiltfile.advanced (demonstrates timesharing)

### CDI Spec Template (Optional Enhancement)

- [X] T058 [P] Create CDI spec template in extensions/k3d-gpu/assets/cdi-spec.tmpl (parameterized template for wrapper script)
- [X] T059 Update wrapper script to use template in extensions/k3d-gpu/assets/k3s-wrapper.sh (if template created)

### Final Validation

- [X] T060 Run all tests via extensions/k3d-gpu/test.sh and verify all pass
- [X] T061 Validate extension loads in under 1 second (performance goal)
- [X] T062 Validate GPU initialization completes in under 30 seconds (performance goal)
- [X] T063 Review inline docstrings in extensions/k3d-gpu/extension.star (ensure all functions documented)
- [X] T064 Verify README examples are copy-paste ready (test basic and advanced Tiltfiles)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories (TDD requirement)
- **User Stories (Phase 3-5)**: All depend on Foundational phase completion
  - User stories can proceed in parallel (if staffed)
  - Or sequentially in priority order (P1 → P2 → P3)
  - Each story is independently testable after completion
- **Polish (Phase 6)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P2)**: Can start after Foundational (Phase 2) - Adds to wrapper script from US1 but doesn't break US1
- **User Story 3 (P3)**: Can start after Foundational (Phase 2) - Adds conditional logic to US1 but doesn't break US1

### Within Each User Story

- Tests (golden files + test scripts) MUST be written FIRST and FAIL before implementation (Phase 2)
- Wrapper script tasks can run in parallel where marked [P]
- Device plugin tasks depend on wrapper script structure
- Integration tasks depend on all implementation tasks completing
- Story complete only after all tests pass (GREEN phase)

### Parallel Opportunities

- **Setup (Phase 1)**: Tasks T003, T004, T005 can run in parallel (different directories)
- **Foundational (Phase 2)**: Tasks T007-T012 can run in parallel (different files)
- **User Story 1**: Tasks T019, T028 can run in parallel (wrapper script + device plugin manifest are independent)
- **User Story 2**: Tasks T037, T038 can run in parallel (different filesystem mounts)
- **User Story 3**: Task T046 can run in parallel with T043-T045
- **Polish (Phase 6)**: All documentation tasks (T049-T053) can run in parallel, all example tasks (T054-T057) can run in parallel

---

## Parallel Example: User Story 1 Core

```bash
# Launch wrapper script skeleton and device plugin manifest in parallel:
Task: "Create k3s wrapper script skeleton in extensions/k3d-gpu/assets/k3s-wrapper.sh"
Task: "Create device plugin DaemonSet manifest template in extensions/k3d-gpu/assets/device-plugin.yaml"

# These don't depend on each other and work on different files
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (directory structure)
2. Complete Phase 2: Foundational (test infrastructure - tests FAIL initially)
3. Complete Phase 3: User Story 1 (core GPU functionality)
   - All tests should now PASS (GREEN phase)
4. **STOP and VALIDATE**: Test User Story 1 independently
   - Load extension in test Tiltfile
   - Run `tilt up`
   - Schedule GPU pod
   - Verify pod can run nvidia-smi
5. Deploy/demo MVP if ready

### Incremental Delivery

1. Complete Setup + Foundational → Test infrastructure ready (tests failing)
2. Add User Story 1 → Test independently → Tests pass → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Tests pass → Deploy/Demo
4. Add User Story 3 → Test independently → Tests pass → Deploy/Demo
5. Add Polish → Complete documentation and examples
6. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together (test infrastructure)
2. Once Foundational is done:
   - Developer A: User Story 1 (core GPU)
   - Developer B: User Story 2 (profiling) - can start in parallel
   - Developer C: User Story 3 (native Linux) - can start in parallel
3. User Story 1 is the dependency for others, but US2 and US3 can proceed with stubs
4. Stories complete and integrate independently
5. Team completes Polish phase together

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing (TDD RED phase)
- Verify tests pass after implementing (TDD GREEN phase)
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- **Constitution Principle V**: TDD is mandatory - tests in Phase 2 must be written first and fail initially

## Task Count Summary

- **Phase 1 (Setup)**: 6 tasks
- **Phase 2 (Foundational - Tests)**: 7 tasks
- **Phase 3 (User Story 1 - MVP)**: 23 tasks
- **Phase 4 (User Story 2)**: 6 tasks
- **Phase 5 (User Story 3)**: 6 tasks
- **Phase 6 (Polish)**: 16 tasks
- **Total**: 64 tasks

## Parallel Opportunities

- **Phase 1**: 3 tasks can run in parallel (directories)
- **Phase 2**: 6 tasks can run in parallel (test files)
- **Phase 3**: 2 task groups can run in parallel (wrapper + manifest)
- **Phase 4**: 2 tasks can run in parallel (filesystem mounts)
- **Phase 6**: 9 tasks can run in parallel (documentation + examples)

## Independent Test Criteria

- **US1**: Load extension → Schedule GPU pod → Pod runs nvidia-smi successfully
- **US2**: Deploy eBPF profiler → Profiler attaches tracepoints → Collects GPU workload data
- **US3**: Load extension on native Linux → GPU pod schedules → Uses auto/legacy mode (not CDI)

## Suggested MVP Scope

**User Story 1 ONLY** (Phase 1 + Phase 2 + Phase 3 = 36 tasks)

This delivers complete GPU functionality for WSL2:
- Zero-config GPU support (`enable_gpu_support()`)
- WSL2 CDI spec generation
- DNS fixes for Docker 29+
- Device plugin with timesharing (4x default)
- Full test coverage

Estimated implementation time: 1-2 development sessions with TDD approach

---

## Specification Alignment Notes (2026-02-14)

**Updated to align with spec.md remediation**:
- T012: Expanded to include timesharing replica validation (edge cases: 1, 4, 8, 100, negative values)
- T017: Expanded to reflect FR-007 configuration scope (feature flags, replica bounds, validation mode)
- T036: Clarified edge case testing (extreme replica counts, invalid cluster names, missing prerequisites)

**Removed scope**:
- FR-010 (standalone k3s support) removed from spec - project is k3d-focused for local development only

**Clarifications**:
- SC-002a: Pods exceeding timesharing limit will remain Pending (standard Kubernetes scheduling)
- FR-007: Configuration now explicitly lists controllable parameters (cluster, namespace, feature flags, GPU limits, validation mode)
