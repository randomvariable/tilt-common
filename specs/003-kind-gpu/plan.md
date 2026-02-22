# Implementation Plan: kind GPU Support Extension

**Branch**: `002-observability-stack` (co-located) | **Date**: 2026-02-15 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/003-kind-gpu/spec.md`

## Summary

Create a Go library extension for kind clusters with NVIDIA GPU support. The extension reads an APIMachinery-versioned YAML config file that embeds a kind v1alpha4 cluster definition, GPU configuration (nvidia), and optional containerd registry mirror configurations with base64-encoded CA certificates. It idempotently builds a custom kindest/node Docker image with NVIDIA toolkit and mirror host configs, then boots a kind cluster using it.

## Technical Context

**Language/Version**: Go 1.23+ (matches existing project)
**Primary Dependencies**:
  - k8s.io/apimachinery (already in go.mod) for TypeMeta
  - sigs.k8s.io/kind/pkg/apis/config/v1alpha4 (new) for Cluster types
  - Docker with BuildKit for image building
  - `kind` binary for cluster lifecycle
**Storage**: N/A (generates Docker images and temp build contexts)
**Testing**: Go table-driven tests (matching k3d-gpu test patterns), tilttest framework not needed (pure Go library)
**Target Platform**: Linux (WSL2 primary, native Linux secondary)
**Project Type**: Go library (imported by magefiles, like k3d-gpu)

## Constitution Check

### Principle I: Extension-First Architecture - PASS
- Self-contained Go package at `extensions/kind-gpu/`
- No dependencies on other tilt-common extensions
- Clear public API: LoadConfig, CreateCluster, DeleteCluster

### Principle II: Configuration Over Convention - PASS
- YAML config file with documented defaults
- Functional options for programmatic overrides
- Sensible defaults (kindest/node:v1.32.2, nvidia GPU type)

### Principle III: Local-First Development Experience - PASS
- Optimizes for local kind clusters with GPU passthrough
- Idempotent image builds for fast iteration

### Principle IV: Documentation and Examples - PASS (planned)
- README.md with quick start and config reference
- Example YAML config files

### Principle V: Test-Driven Development - PASS (planned)
- Unit tests for config parsing, Dockerfile generation, mirror config
- Table-driven tests matching k3d-gpu patterns

## Project Structure

### Source Code

```text
extensions/kind-gpu/
├── api/
│   └── v1alpha1/
│       └── types.go           # KindGPUCluster API types
├── kindgpu.go                 # CreateCluster, DeleteCluster, LoadConfig
├── image.go                   # Image building (Dockerfile generation, idempotent build)
├── mirrors.go                 # Mirror config generation (hosts.toml, CA decode)
├── assets/
│   └── Dockerfile.tmpl        # Go template for custom kindest/node image
└── README.md

tests/kind-gpu/
├── kindgpu_test.go            # Unit tests
└── golden/                    # Golden files for generated output
```

## Implementation Phases

### Phase 1: API Types
- Define KindGPUCluster, KindGPUClusterSpec, GPUConfig, MirrorConfig, ContainerdHostConfig
- Add sigs.k8s.io/kind dependency

### Phase 2: Core Implementation
- LoadConfig: YAML parsing with validation
- Image building: Dockerfile generation, build context, idempotent docker build
- Mirror config: hosts.toml generation, CA base64 decode
- CreateCluster: Orchestrate image build + kind create
- DeleteCluster: kind delete wrapper

### Phase 3: Tests
- Config parsing tests
- Dockerfile generation tests
- hosts.toml generation tests
- Mirror CA decode tests

### Phase 4: Integration
- Add mage targets (E2e.KindGpu)
- Add kind to .tools.yaml
- README documentation
