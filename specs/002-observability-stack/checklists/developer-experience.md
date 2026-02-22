# Developer Experience Checklist: Observability Stack Extension

**Purpose**: Validate that requirements are complete, clear, and measurable for the full developer journey -- from onboarding through daily use, troubleshooting, and export/analysis -- for both extension consumers and contributors.
**Created**: 2026-02-14
**Updated**: 2026-02-14 (post-analysis remediation)
**Feature**: [spec.md](../spec.md)

## Onboarding & First-Use Experience

- [x] CHK001 - Is the minimum number of Tiltfile lines required for basic setup explicitly specified? [Clarity, Spec §SC-001] — SC-001: "two lines to their Tiltfile (load + function call)"; quickstart §1 shows exact lines
- [x] CHK002 - Are prerequisites (Tilt version, cluster types, kubectl) documented as requirements, not just assumptions? [Completeness, Spec §Assumptions] — Assumptions §219-227 list cluster/image requirements; quickstart §5-9 lists prerequisites
- [x] CHK003 - Is the "under 5 minutes" onboarding target (SC-001) defined with a clear starting point (from what state)? [Measurability, Spec §SC-001] — SC-001: "adding two lines to their Tiltfile" implies existing Tilt project as starting state
- [x] CHK004 - Are the default credentials for Grafana (admin/admin) specified as a requirement, or only mentioned in assumptions? [Clarity, Spec §Assumptions] — Assumptions §226: "admin/admin as default credentials for local development"
- [x] CHK005 - Does the spec define what feedback the developer receives during initial deployment (progress indicators, print messages)? [Gap] — Tilt provides native resource status feedback; FR-011 ensures resources are labeled and visible in Tilt dashboard
- [x] CHK006 - Are requirements defined for what happens on first `tilt up` when images need to be pulled (cold start vs warm start)? [Gap, Edge Case] — NFR-001 updated: "with cached container images (warm start). Cold starts excluded from this target."

## Configuration & Defaults

- [x] CHK007 - Are default values for all configuration keys explicitly listed in one place in the spec? [Completeness, Spec §FR-007] — data-model §28-47 (ExtensionConfig table); quickstart §152-169 (Configuration Reference)
- [x] CHK008 - Is the behavior for unknown/unrecognized config keys specified (error, warning, or silently ignored)? [Gap] — FR-015: "reject unrecognized configuration keys with fail()"; Clarification Q7 confirms
- [x] CHK009 - Is the configuration dictionary structure (flat vs nested) specified, or left ambiguous between `config["vmagent"]` vs `config["components"]["vmagent"]`? [Clarity, Spec §FR-016] — contracts §19-46 show nested dict structure; data-model uses dot notation (components.vmagent); consistent
- [x] CHK010 - Are requirements consistent between FR-016 (component enable/disable) and FR-019 (image overrides) regarding which component names are valid config keys? [Consistency, Spec §FR-016, §FR-019] — contracts §34-41 explicitly documents image key mapping (profiling -> parca_server, parca_agent)
- [x] CHK011 - Is the interaction between `components.grafana=True` and all backends disabled specified beyond the edge case note? [Clarity, Spec §Edge Case 6] — Edge Case 6 + FR-017: Grafana deploys with no auto-provisioned datasources or backend dependencies
- [x] CHK012 - Does the spec define whether port overrides apply to container ports, host port-forwards, or both? [Ambiguity, Spec §FR-006] — FR-006: "host port-forward assignments"; data-model §61-69 shows fixed container ports

## Error Messages & Validation Feedback

- [x] CHK013 - Are error message requirements specified for each validation failure (port range, namespace format, dependency constraints)? [Completeness, Spec §FR-015] — FR-015 (port/namespace/keys), FR-020 (containerd), FR-023 (vmagent): each defines when fail() triggers
- [x] CHK014 - Is the term "clear error message" (used in FR-009, FR-020, FR-023) quantified with specific content requirements (what must the message include)? [Ambiguity, Spec §FR-009] — FR-020 + research Decision 8: error lists checked socket paths; context-specific per FR
- [x] CHK015 - Are requirements defined for the order of validation checks (fail fast on first error, or report all errors at once)? [Gap] — Starlark's fail() is inherently fail-fast; validation order is an implementation concern
- [x] CHK016 - Does the spec define whether validation errors should include suggested fixes or corrective actions? [Gap] — Implementation detail; error message content is implementation-level quality
- [x] CHK017 - Is the error behavior specified when containerd socket auto-detection fails (FR-020) -- does the message list which paths were checked? [Clarity, Spec §FR-020] — FR-020: "fail with a clear error"; research Decision 8: "listing checked paths"

## Daily Use & Workflow Integration

- [x] CHK018 - Are requirements for Tilt dashboard organization specified beyond "observability label group" (FR-011) -- e.g., ordering, grouping, resource naming? [Clarity, Spec §FR-011] — FR-011: label-based grouping; Tilt's native UI handles ordering/display
- [x] CHK019 - Is the `otel_env()` return format precisely specified (list of strings, dict, or Tilt-native env format)? [Ambiguity, Spec §FR-010] — contracts §95: `list[str]` of 'KEY=VALUE' strings; data-model §107-115 shows exact format
- [x] CHK020 - Does the spec define how `otel_env()` integrates with different Tilt patterns (`k8s_resource(env=...)`, `docker_build(env=...)`)? [Coverage, Spec §FR-010] — contracts §111-117: generic usage pattern (returns list for user injection); not tied to specific Tilt API
- [x] CHK021 - Are requirements defined for what happens when `otel_env()` is called before `enable_observability()`? [Gap, Edge Case] — contracts §90-92: otel_env() takes optional config dict, self-contained; works independently
- [x] CHK022 - Is the idempotency requirement (Edge Case 4) specific about what "re-registering without error" means -- are resources updated, skipped, or replaced? [Clarity, Spec §Edge Case 4] — NFR-004: "MUST NOT produce errors or duplicate resources"; Tilt handles k8s_yaml/k8s_resource idempotency natively
- [x] CHK023 - Are resource dependency requirements (FR-005) specified for all component combinations, not just Grafana -> backends? [Completeness, Spec §FR-005] — FR-005 (Grafana->backends), FR-023 (VMAgent->VictoriaMetrics), FR-009 (profiling standalone); data-model §169-179 shows full graph

## Selective Component Enablement

- [x] CHK024 - Does the spec define the minimum valid configuration (can all core components be disabled, or must at least one be enabled)? [Gap, Spec §FR-016] — FR-016: "When all components are disabled, the function MUST be a valid no-op without error"; Clarification Q8 confirms
- [x] CHK025 - Are requirements consistent between FR-009 ("profiling requires the base observability stack") and FR-016 (individual component enable/disable) -- what exactly constitutes the "base stack"? [Ambiguity, Spec §FR-009, §FR-016] — FR-009 updated: "allow profiling to be enabled standalone without requiring any core observability backends"; no base stack concept
- [x] CHK026 - Is the behavior specified when logs are disabled but traces are enabled (does VictoriaTraces still work standalone)? [Coverage, Spec §FR-016] — FR-016: individual enable/disable; FR-017: conditional datasources; each component is independent except VMAgent->VictoriaMetrics
- [x] CHK027 - Does the spec define whether disabling a component also removes its port-forward, or just the deployment? [Gap] — FR-006: port-forwards are per-component; disabled components generate no resources or port-forwards

## VMAgent & Prometheus Scraping

- [x] CHK028 - Is the format for `scrape_targets` (FR-022) precisely specified -- `host:port` string, or a richer structure with path/scheme? [Clarity, Spec §FR-022] — data-model §45: "List of target strings (host:port)"; contracts §43: example `['my-service:8080']`
- [x] CHK029 - Are requirements defined for how user-provided static targets and Kubernetes service discovery coexist (additive, exclusive, or configurable)? [Gap, Spec §FR-022] — FR-022: supports both; research Decision 7: "appended alongside kubernetes_sd_configs" (additive)
- [x] CHK030 - Does the spec define what "annotated services" means in the context of Kubernetes service discovery (which annotations, what format)? [Ambiguity, Spec §FR-022] — research Decision 7 + data-model §128: `prometheus.io/scrape`, `/port`, `/path`, `/scheme` annotations

## Grafana Dashboard Provisioning

- [x] CHK031 - Is the expected format of dashboard JSON files specified (full Grafana dashboard model, or a subset)? [Clarity, Spec §FR-024] — Standard Grafana dashboard JSON format (industry standard); extension reads files via read_file() and passes to ConfigMap
- [x] CHK032 - Are requirements defined for what happens when a dashboard JSON file path is invalid or the file is malformed? [Gap, Edge Case, Spec §FR-024] — Starlark's read_file() fails on invalid paths; Grafana handles malformed JSON at startup with native error logging
- [x] CHK033 - Does the spec define whether provisioned dashboards use hardcoded datasource UIDs or are wired dynamically to enabled backends? [Gap, Spec §FR-025] — data-model §97-101: stable UIDs defined (victoriametrics, victorialogs, victoriatraces); dashboard authors reference these

## Export & Offline Analysis

- [x] CHK034 - Is the export trigger mechanism precisely specified -- Tilt button label, CLI command syntax, or both? [Clarity, Spec §FR-029] — FR-029: "Tilt dashboard button or a single CLI command"; contracts §121-145; quickstart §7 shows both
- [x] CHK035 - Are requirements defined for export progress feedback (percentage, per-backend status, estimated time)? [Gap] — Implementation detail; CLI stdout provides native progress output
- [x] CHK036 - Is "partial output rather than failing entirely" (Edge Case 7) specified with requirements for what the partial output report contains? [Clarity, Spec §Edge Case 7] — Edge Case 7: "report which backends were skipped"; tasks T028: exit codes 0/1/2
- [x] CHK037 - Does the spec define the DuckDB file naming convention and schema versioning for forward compatibility? [Gap, Spec §FR-027] — FR-027: timestamped filenames; research Decision 2: schema definition; v1 schema without versioning (initial release)
- [x] CHK038 - Are requirements defined for export behavior when the DuckDB output file already exists (overwrite, append, error)? [Gap, Edge Case] — Clarification Q6: "Timestamped -- always create a new file with a timestamp suffix"; FR-027 confirms
- [x] CHK039 - Is the relationship between the export CLI command and the Tilt button specified -- are they identical in behavior, or does the button invoke the CLI? [Ambiguity, Spec §FR-029] — contracts §129-130: button "runs the observability-export CLI tool"; tasks T033: local_resource invokes CLI
- [x] CHK040 - Does the spec define whether the Jupyter notebook examples are validated as part of the test suite or only shipped as documentation? [Clarity, Spec §SC-007] — SC-007: "included in the repository and validated"; e2e test validates DuckDB queryability; notebooks are examples

## Performance & Resource Experience

- [x] CHK041 - Is the 90-second health check target (SC-002) specified with environmental conditions (cold pull vs cached images, cluster type)? [Measurability, Spec §SC-002] — NFR-001 updated: "with cached container images (warm start). Cold starts excluded."
- [x] CHK042 - Are resource request/limit values specified per component in the spec, or only aggregated as "~1 CPU + 1.5 GiB"? [Completeness, Spec §Assumptions] — NFR-002: aggregate constraint; data-model §72-81 per-component fields; research Decision 7: VMAgent specifics
- [x] CHK043 - Does the spec define what happens when the cluster lacks sufficient resources for enabled components? [Clarity, Spec §Edge Case 2] — Edge Case 2: "Kubernetes scheduling will surface pending pods via Tilt's existing resource status"

## Contributor Developer Experience

- [x] CHK044 - Are requirements defined for test execution without a Kubernetes cluster (Tier 1/2 tests)? [Completeness, Spec §FR-034] — research Decision 10: Tier 1/2 via `tilt alpha tiltfile-result` (no cluster needed)
- [x] CHK045 - Does the spec define what "validates the full observability pipeline" (SC-006) means in measurable terms -- which specific assertions must pass? [Measurability, Spec §SC-006] — SC-006: deploy, send telemetry via OTLP, query backends, export to DuckDB, verify pprof files
- [x] CHK046 - Are requirements specified for the Go export CLI's build prerequisites (CGO, GCC version, platform support)? [Gap] — plan.md Technical Context: "Go 1.23+ (requires CGO for DuckDB)"; research Decision 1: CGO notes and platform list
- [x] CHK047 - Is the testing strategy (3-tier model) referenced in the spec or only in the plan? If only in the plan, are spec-level testability requirements sufficient? [Traceability, Spec §FR-034] — FR-034 + SC-006 define testability at spec level; research Decision 10 defines tiers at plan level
- [x] CHK048 - Does the spec define golden file update workflow requirements (how to regenerate golden files when expected output changes)? [Gap] — Development workflow concern; covered in tasks (T016, T026, T038)
- [x] CHK049 - Are inline docstring requirements specified for internal functions, or only for public API functions? [Gap, Spec §FR-013] — FR-013: follows k3d-gpu pattern; constitution §IV mandates inline docstrings

## Cross-Cutting Consistency

- [x] CHK050 - Are the component names used consistently across FR-016 (enable/disable), FR-006 (ports), FR-019 (images), and the OTel env helper (FR-010)? [Consistency] — Consistent: component keys (metrics/logs/traces/grafana/vmagent/profiling); port key "parca" maps to profiling
- [x] CHK051 - Does the spec use "profiling" and "parca" interchangeably, and is the canonical config key name defined? [Ambiguity, Spec §FR-008, §FR-016] — "profiling" is the config key; "Parca" is the tool name; data-model §37, §67-68 documents both
- [x] CHK052 - Is the namespace parameter behavior consistent across all components -- do VMAgent RBAC resources (ClusterRole/ClusterRoleBinding) respect the namespace config? [Consistency, Spec §FR-014] — FR-014: namespace for namespaced resources; ClusterRole/ClusterRoleBinding are inherently cluster-scoped in K8s
- [x] CHK053 - Are the FR numbering gaps (FR-029 appears after FR-031) intentional, and does this affect traceability? [Traceability] — Fixed in analysis remediation: FRs now sequential FR-001 through FR-034

## Notes

- All 53 items verified against spec.md, data-model.md, contracts/extension-api.star, research.md, and quickstart.md
- 2 spec updates made during review: NFR-001 cold/warm start clarification, quickstart.md stale references
- Items marked with references to specific document sections for traceability
