# Implementation Plan: k3s GPU Support Extension

**Branch**: `001-k3d-gpu` | **Date**: 2026-02-14 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-k3d-gpu/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Create a Tilt extension that enables NVIDIA GPU access in local k3s clusters, with specialized support for WSL2 environments. The extension will deploy NVIDIA device plugins, generate CDI specs for WSL2, fix Docker 29+ DNS issues, and mount kernel filesystems for eBPF profiling tools. Developers will be able to add GPU support to their local development environment by adding a single `load()` statement to their Tiltfile.

Primary user value: Reduce GPU workload iteration cycles from minutes (cloud deployment) to seconds (local testing).

## Technical Context

**Language/Version**: Starlark (Tilt extension language) + Bash 4.0+ (k3s wrapper script)
**Primary Dependencies**:
  - Tilt 0.30+
  - k3s/k3d with containerd runtime
  - NVIDIA container toolkit (nvidia-ctk, nvidia-container-cli)
  - NVIDIA drivers on host
**Storage**: N/A (extension generates configuration files in k3s node containers)
**Testing**:
  - Bash smoke tests using `tilt ci`
  - YAML golden file comparisons
  - Configuration validation tests (invalid inputs)
**Target Platform**: Linux (WSL2 primary, native Linux secondary)
**Project Type**: Tilt extension (Starlark module + supporting assets)
**Performance Goals**:
  - Extension load time: <1 second
  - Cluster GPU initialization: <30 seconds from `tilt up`
  - Zero overhead when GPU workloads not scheduled
**Constraints**:
  - Must work with existing k3s clusters without breaking non-GPU workloads
  - No dependencies on other tilt-common extensions (Principle I)
  - Configuration via dict parameters only (no global state)
**Scale/Scope**:
  - Single extension module (~300-400 lines Starlark)
  - Supporting wrapper script (~150 lines Bash)
  - Kubernetes manifests (~100 lines YAML)
  - Documentation and examples (~200 lines)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### Principle I: Extension-First Architecture ✅

**Status**: PASS

- Extension will be independently loadable via `load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')`
- Configuration via dict parameter with defaults: `enable_gpu_support({'cluster_name': 'k3d-memex', 'namespace': 'kube-system'})`
- Exposes 1-2 main functions: `enable_gpu_support()` and optionally `configure_gpu_resources()`
- No dependencies on other tilt-common extensions (observability, component-deps)
- Works in isolation: users can load GPU extension without loading any other extensions

**Compliance**: Full compliance with loose coupling and focused functionality requirements.

### Principle II: Configuration Over Convention ✅

**Status**: PASS

- Accepts configuration dictionary with documented defaults:
  - `cluster_name`: defaults to detecting k3d cluster
  - `namespace`: defaults to 'kube-system'
  - `enable_profiling`: defaults to true (mounts debugfs/tracefs)
  - `enable_dns_fix`: defaults to true (Docker 29+ DNS resolution)
  - `enable_timesharing`: defaults to true (GPU sharing across pods)
  - `timesharing_replicas`: defaults to 4 (report 4x GPUs for pod sharing)
  - `device_plugin_image`: defaults to NVIDIA official image
  - `validation_mode`: defaults to 'strict' (fail fast on misconfiguration)
- Supports environment variable overrides: `TILT_GPU_ENABLED`, `TILT_GPU_NAMESPACE`
- Validation with `fail()` on misconfiguration (missing nvidia-ctk, incompatible platform, invalid replica bounds)
- Basic use case: `enable_gpu_support()` with zero config works on WSL2 + native Linux

**Compliance**: Full compliance with configuration flexibility and validation requirements.

### Principle III: Local-First Development Experience ✅

**Status**: PASS

- Optimizes for local k3s development (not production clusters)
- Minimal resource usage: device plugin uses <50MB memory
- Port forwarding: N/A (extension doesn't expose services directly)
- GPU access: Core feature - WSL2 CDI configuration + native Linux support
- Observability: Extension itself doesn't add observability, but enables profiling via filesystem mounts

**Compliance**: Full compliance with local development optimization focus.

### Principle IV: Documentation and Examples (NON-NEGOTIABLE) ✅

**Status**: PASS (planned)

Required deliverables:
- `extensions/k3d-gpu/README.md`:
  - Quick start: Copy-paste Tiltfile snippet
  - Configuration table: All dict parameters with types, defaults, descriptions
  - Prerequisites: NVIDIA drivers, nvidia-container-toolkit, k3s with containerd
  - Troubleshooting: Common errors (driver missing, CDI generation failed, DNS broken)
- `examples/k3d-gpu/Tiltfile`: Working example showing basic and advanced usage
- Inline Starlark docstrings: Function purpose, parameters, return values
- Integration tests: Smoke test that loads extension and validates YAML output

**Compliance**: Plan includes all required documentation artifacts.

### Principle V: Test-Driven Development for Stability ✅

**Status**: PASS (planned)

Test plan:
- **Smoke tests** (`extensions/k3d-gpu/test.sh`):
  - Extension loads without errors
  - Device plugin YAML generates correctly
  - k3s wrapper script executes without errors
- **Output validation tests**:
  - Generated YAML matches expected structure (golden files)
  - CDI spec format correct for WSL2 vs native Linux
- **Configuration validation tests**:
  - Invalid cluster name → clear error message
  - Missing nvidia-ctk → actionable error with remediation steps

Tests will be written FIRST (RED), then extension implemented (GREEN), then refactored (REFACTOR).

**Compliance**: Full compliance with TDD requirement and test coverage expectations.

### Overall Assessment

**✅ ALL GATES PASS** - No constitution violations. Extension design fully complies with all five principles.

## Project Structure

### Documentation (this feature)

```text
specs/001-k3d-gpu/
├── plan.md                      # This file (/speckit.plan command output)
├── research.md                  # Phase 0 output (CDI specs, device plugin research)
├── data-model.md                # Phase 1 output (config entities, CDI structure)
├── quickstart.md                # Phase 1 output (copy-paste examples)
├── contracts/
│   └── extension-api.star       # Phase 1 output (Starlark function signatures)
├── checklists/
│   └── requirements.md          # Quality validation checklist
└── tasks.md                     # Phase 2 output (/speckit.tasks - NOT created yet)
```

### Source Code (repository root)

```text
extensions/k3d-gpu/
├── extension.star               # Main Tilt extension (Starlark)
├── README.md                    # Documentation (quick start, config, troubleshooting)
├── assets/
│   ├── k3s-wrapper.sh          # Modified k3s entrypoint (DNS, CDI, debugfs)
│   ├── device-plugin.yaml      # NVIDIA device plugin DaemonSet
│   └── cdi-spec.tmpl           # CDI spec template for WSL2
└── test.sh                      # Smoke tests using tilt ci

examples/k3d-gpu/
├── Tiltfile                     # Example: Basic usage
├── Tiltfile.advanced            # Example: Custom configuration
└── test-gpu-pod.yaml            # Example: GPU-requesting test workload

tests/k3d-gpu/
├── golden/
│   ├── device-plugin.yaml      # Expected output for validation
│   └── cdi-spec-wsl2.yaml      # Expected CDI spec for WSL2
├── test-load.sh                 # Test: Extension loads without errors
├── test-output-validation.sh    # Test: Generated YAML matches golden files
└── test-config-validation.sh    # Test: Invalid configs produce clear errors
```

**Structure Decision**: Tilt Extension Structure

This feature uses a **Tilt extension structure** (not single/web/mobile project). The extension lives in `extensions/k3d-gpu/` with:

1. **Main extension file** (`extension.star`): Starlark functions that users `load()` in their Tiltfiles
2. **Assets directory**: Supporting files (bash scripts, YAML templates) embedded or referenced by the extension
3. **Tests directory**: Smoke tests and validation tests at repository root level
4. **Examples directory**: Copy-paste ready examples showing real usage

This structure aligns with Tilt extension conventions and the constitution's Extension-First Architecture principle. The extension is self-contained and independently loadable without dependencies on repository organization.

## Complexity Tracking

> **No constitution violations - table not applicable**

All principles passed in Constitution Check. No complexity exceptions required.

---

## Planning Summary

### Phase 0: Research ✅ COMPLETE

Research documented in [research.md](research.md) covering:
- CDI spec format for WSL2 GPU access
- NVIDIA device plugin configuration
- DNS resolution fixes for Docker 29+
- eBPF profiling filesystem requirements
- containerd runtime configuration

All technical approaches extracted from proven memex implementation.

### Phase 1: Design ✅ COMPLETE

Design artifacts created:
- **[data-model.md](data-model.md)**: Configuration entities, CDI spec structure, data flows
- **[contracts/extension-api.star](contracts/extension-api.star)**: Public API contract (enable_gpu_support, configure_gpu_resources)
- **[quickstart.md](quickstart.md)**: Copy-paste examples for basic and advanced usage

### Phase 2: Task Breakdown (Next Step)

Ready for `/speckit.tasks` to generate task list with:
- TDD test suite (smoke tests, output validation, config validation)
- Starlark extension implementation (enable_gpu_support function)
- Bash wrapper script (k3s-wrapper.sh with DNS fix, CDI generation, profiling mounts)
- YAML manifests (device plugin DaemonSet)
- Documentation (README with troubleshooting)
- Examples (basic and advanced Tiltfiles)

### Implementation Readiness

**Status**: ✅ READY FOR IMPLEMENTATION

- All research completed (no open questions)
- All design artifacts generated
- Constitution compliance verified (all 5 principles pass)
- Technical approach validated (extracted from working memex implementation)
- API contract defined with clear user-facing functions
- Quick start examples provide copy-paste starting points

### Estimated Implementation Scope

- **Extension**: ~300-400 lines Starlark
- **Wrapper Script**: ~150 lines Bash
- **Manifests**: ~100 lines YAML
- **Tests**: ~200 lines Bash + golden files
- **Documentation**: ~200 lines Markdown
- **Total**: ~1000 lines code/docs/tests

**Estimated Implementation Time**: 2-3 development sessions with TDD approach

---

## Specification Alignment Update (2026-02-14)

**Changes to align with spec.md remediation**:

### Configuration Expansion (FR-007)
- Added explicit feature toggles: `enable_dns_fix`, `enable_timesharing`
- Added `validation_mode` parameter (strict vs permissive)
- Expanded validation to include replica count bounds checking

### Scope Clarification
- **Removed**: FR-010 (standalone k3s support) - project is k3d-focused for local development
- **Focus**: k3d-managed clusters only, not general k3s administration

### Edge Case Coverage
- Added timesharing replica validation (edge cases: 1, 100, negative values)
- Clarified pending pod behavior when timesharing limit exceeded (SC-002a)

### Constitution Compliance
- All 5 principles remain PASS - no changes to architecture or testing approach
- Configuration expansion aligns with Principle II (Configuration Over Convention)
