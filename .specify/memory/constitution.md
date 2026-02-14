<!--
Sync Impact Report (2026-02-14):
Version: 2.1.0 (added Go code quality principle)
Type: MINOR - Added code quality guidance to Go Development Standards
Changed: Added "Code Quality (NON-NEGOTIABLE)" section under "Go Support Code"
New principle: Never add linter exclusions, always run `golangci-lint --fix`
Rationale: Enforce zero-tolerance policy for linter warnings
Previous version: 2.0.0 (2026-02-14) - Tilt extension library constitution
Templates requiring updates:
  ✅ No template updates needed - code quality enforcement is operational
Follow-up TODOs: None
-->

# tilt-common Constitution

## Purpose

This repository extracts reusable Tilt development patterns from the memex project into standalone, composable extensions. The focus is on three key patterns:
1. **k3s with GPU support** - WSL2-specific setup for NVIDIA GPU access in k3s
2. **Observability stack** - Pre-configured VictoriaMetrics, VictoriaLogs, VictoriaTraces, Grafana with OpenTelemetry
3. **Component orchestration** - Dependency validation, configuration management, live updates

## Core Principles

### I. Extension-First Architecture

Every Tilt extension MUST be a standalone, composable Starlark module. Each extension MUST:
- Be independently loadable via `load('ext://tilt-common/...', '...')` pattern
- Accept configuration via function parameters with sensible defaults
- Expose a clear, minimal public API (prefer 1-3 main functions per extension)
- Have no hard dependencies on other tilt-common extensions (loose coupling)
- Work in isolation — users can load just the GPU extension without the observability stack

**Rationale**: Tilt extensions are imported into user Tiltfiles. Over-coupling creates bloat and forces users to adopt patterns they don't need. Each extension must provide focused, optional functionality.

### II. Configuration Over Convention

Extensions MUST be configurable without requiring code changes:
- Accept configuration dictionaries with documented defaults
- Support environment variable overrides for CI/automation
- Provide validation with clear error messages on misconfiguration
- Allow incremental adoption — basic use case works with zero config, advanced use cases unlock via options

**Rationale**: Different projects have different needs. k3s cluster names vary, port preferences differ, GPU setups vary between WSL2/Linux. Configuration flexibility enables broad adoption while maintaining sane defaults.

### III. Local-First Development Experience

Extensions optimize for production-like local Kubernetes development:
- Fast iteration: live_update patterns for code sync without rebuilds
- Resource efficiency: minimal replicas, appropriate resource limits for laptops
- Debuggability: proper port forwarding, structured component labels
- GPU access: WSL2-specific CDI configuration for NVIDIA device passthrough
- Observability: pre-wired OpenTelemetry exporters to local VictoriaMetrics stack

**Rationale**: Local development shouldn't require cloud resources or slow rebuild cycles. These patterns emerged from real development needs in memex and should enable similar productivity for other projects.

### IV. Documentation and Examples (NON-NEGOTIABLE)

Every extension MUST include:
- Inline Starlark docstrings explaining purpose, parameters, and return values
- README.md with:
  - Quick start example (copy-paste ready)
  - Configuration options table with types and defaults
  - Prerequisites (e.g., "requires k3s with containerd runtime")
  - Troubleshooting section for common issues
- Working example Tiltfile in `examples/<extension-name>/`
- Integration tests that verify the extension loads and produces valid Kubernetes YAML

**Rationale**: Tilt extensions are adopted by copy-paste from documentation. Poor docs mean zero adoption. memex patterns are complex (WSL2 GPU setup, OTel wiring) — documentation is the primary deliverable, not just the code.

### V. Test-Driven Development for Stability

All extensions and supporting Go code MUST follow TDD:

**Starlark extensions**:
- Smoke tests that verify extension loads without errors
- Output validation tests (generated YAML matches expected structure)
- Configuration validation tests (invalid config produces clear errors)

**Go helpers** (if any):
- Unit tests for all exported functions (table-driven tests)
- Integration tests for Kubernetes interactions
- Tests MUST be written FIRST, fail initially, then pass after implementation

**Rationale**: Broken Tilt extensions halt entire development workflows. These are critical-path tools. TDD ensures regressions are caught before users experience broken `tilt up` commands.

## Source Patterns

This repository extracts patterns from `../memex/Tiltfile` and `../memex/dev/`:

| Pattern | Source Files | Extraction Target |
|---------|--------------|-------------------|
| k3s GPU (WSL2) | `dev/k3s-gpu/`, `dev/k8s/nvidia-device-plugin.yaml` | `extensions/k3s-gpu/` |
| Observability | `Tiltfile` (lines 150-165, 369-411), `dev/k8s/observability/` | `extensions/observability/` |
| Component deps | `Tiltfile` (lines 52-96: DEPENDENCIES, validation) | `extensions/component-deps/` |
| Pipeline services | `Tiltfile` (line 168: `pipeline_service_yaml()`) | `extensions/service-gen/` |
| Live updates | `Tiltfile` (docker_build_with_restart, live_update) | `extensions/live-sync/` |

## Development Standards

### Starlark Extensions

- Each extension lives in `extensions/<name>/extension.star`
- Function names use `snake_case` (Starlark convention)
- Use Starlark's native data structures (dict, list) — avoid complex nesting
- Validate configuration early with `fail()` for clear error messages
- Document parameters using inline comments above functions
- Format with `buildifier` if available (else, consistent 2-space indentation)

### Go Support Code (if needed)

- Go code lives in `pkg/` or `cmd/` directories (separate from extensions)
- All Go code MUST pass `go vet`, `golangci-lint`, `staticcheck`
- Use `gofmt` for formatting (enforced via CI)
- Exported APIs require godoc comments
- Minimize dependencies — prefer standard library

**Code Quality (NON-NEGOTIABLE)**:
- NEVER add linter exclusions (no `//nolint:...` comments, no `.golangci.yml` exclusions)
- ALWAYS run `golangci-lint run --fix ./...` before committing
- Fix ALL linter warnings — if a linter flags an issue, address the root cause
- If a warning seems incorrect, improve the code to make the intent clearer rather than silencing the linter
- Linters catch bugs, race conditions, and API misuse — ignoring them trades short-term convenience for long-term reliability

**Rationale**: Linter warnings indicate real issues: unused code, error handling gaps, inefficient patterns, or security vulnerabilities. Suppressing warnings creates technical debt and obscures future problems. Always fixing linter issues keeps the codebase maintainable and catches bugs before production.

### Testing

**Starlark extensions**:
- Smoke tests in `extensions/<name>/test.sh` that run `tilt ci` with example Tiltfile
- Output tests comparing generated YAML against golden files
- Configuration validation tests for invalid inputs

**Go code**:
- Unit tests: `*_test.go` files with table-driven tests
- Integration tests: separate `integration_test.go` files (use build tags)
- Minimum 80% coverage for new packages

### Documentation Requirements

Each extension MUST have:
- `README.md` with quick start, configuration table, prerequisites, troubleshooting
- `examples/<name>/Tiltfile` showing real usage
- Inline docstrings in Starlark code
- Migration notes if extracting from memex changes behavior

## Governance

### Amendment Process

This constitution supersedes all other development practices. To amend:
1. Propose changes via PR with rationale in commit message
2. Changes require review and approval from at least one maintainer
3. Breaking changes to principles require broader consensus discussion
4. Version must be bumped according to semantic versioning

### Compliance and Review

- All PRs MUST verify compliance with these principles before merge
- Reviewers MUST check for adherence to error handling, testing, and API design standards
- Complexity exceptions MUST be documented in the implementation plan with justification
- Regular audits of codebase for drift from principles

### Living Document

This constitution is a living document. As the project evolves and learns from production usage, principles may be refined. However, core values (testing discipline, production readiness, API stability) remain fixed.

**Version**: 2.1.0 | **Ratified**: 2026-02-14 | **Last Amended**: 2026-02-14
