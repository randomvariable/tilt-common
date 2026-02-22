# tilt-common Development Guidelines

Auto-generated from all feature plans. Last updated: 2026-02-14

## Active Technologies
- Starlark (Tilt extension) + Go 1.23+ (export CLI tool, requires CGO for DuckDB) + Python 3.11+ (Jupyter notebook examples) + Bash 4.0+ (test scripts, containerd detection) + VictoriaMetrics ecosystem (metrics/logs/traces), Grafana 11.x, Parca 0.25.x, VMAgent, DuckDB (via `github.com/duckdb/duckdb-go` v2.5.5+), Tilt (for extension runtime) (002-observability-stack)
- DuckDB database file (export only), ephemeral in-cluster storage for all backends (002-observability-stack)
- Go 1.23+ (kindgpu library, mage targets, E2E tests) + Starlark (observability extension, unchanged) + `sigs.k8s.io/kind v0.31.0` (already in go.mod), `kindgpu` Go package (002-observability-stack)
- Go 1.23+ (kindgpu library, export CLI, mage targets, E2E tests) + Starlark (observability Tilt extension) + Python 3.11+ (example Jupyter notebooks) (002-observability-stack)
- Ephemeral in-cluster; DuckDB file for export only (002-observability-stack)
- Starlark (Tilt extension) + Go 1.23+ (export CLI, requires CGO for DuckDB) + Python 3.11+ (Jupyter notebook examples only) + Bash 4.0+ (test scripts, containerd detection) + VictoriaMetrics ecosystem (metrics/logs/traces), Grafana Operator (`grafana.integreatly.org`), Vector, VMAgent, OpenTelemetry Operator (Helm chart v0.105.1), Parca 0.25.x, DuckDB (`github.com/duckdb/duckdb-go` v2.5.5+), Tilt (extension runtime), Helm v3.x (for OTel + Grafana Operator chart rendering) (002-observability-stack)
- DuckDB database file (export only), ephemeral in-cluster storage for all backends with built-in retention flags (002-observability-stack)

- Starlark (Tilt extension language) + Bash 4.0+ (k3s wrapper script) (001-k3s-gpu)

## Project Structure

```text
src/
tests/
```

## Commands

```bash
go run mage.go -l                 # List all targets
go run mage.go tools:install      # Install all tools (golangci-lint, helm, tilt, kubectl, k3d, buildifier)
go run mage.go lint:all           # Run all linters (Go + Starlark)
go run mage.go lint:go            # Run golangci-lint
go run mage.go lint:starlark      # Run buildifier on .star files
go run mage.go build:all          # Build all Go packages
go run mage.go build:exportCLI    # Build observability-export CLI (requires CGO)
go run mage.go test:unit          # Run Go tests with race detector
go run mage.go e2e:all                # Run all extension tests
go run mage.go e2e:observability      # Run observability extension tests (no cluster)
go run mage.go e2e:observabilityFull  # Run full E2E suite (creates k3d cluster, requires CGO)
go run mage.go e2e:k3dGpu             # Run k3d-gpu extension tests
```

## Code Style

Starlark (Tilt extension language) + Bash 4.0+ (k3s wrapper script): Follow standard conventions

## Recent Changes
- 002-observability-stack: Added Starlark (Tilt extension) + Go 1.23+ (export CLI, requires CGO for DuckDB) + Python 3.11+ (Jupyter notebook examples only) + Bash 4.0+ (test scripts, containerd detection) + VictoriaMetrics ecosystem (metrics/logs/traces), Grafana Operator (`grafana.integreatly.org`), Vector, VMAgent, OpenTelemetry Operator (Helm chart v0.105.1), Parca 0.25.x, DuckDB (`github.com/duckdb/duckdb-go` v2.5.5+), Tilt (extension runtime), Helm v3.x (for OTel + Grafana Operator chart rendering)
- 002-observability-stack: Added Go 1.23+ (kindgpu library, export CLI, mage targets, E2E tests) + Starlark (observability Tilt extension) + Python 3.11+ (example Jupyter notebooks)
- 002-observability-stack: Added Go 1.23+ (kindgpu library, mage targets, E2E tests) + Starlark (observability extension, unchanged) + `sigs.k8s.io/kind v0.31.0` (already in go.mod), `kindgpu` Go package


<!-- MANUAL ADDITIONS START -->
<!-- MANUAL ADDITIONS END -->
