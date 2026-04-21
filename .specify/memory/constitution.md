<!--
  Sync Impact Report
  ==================
  Version change: (new) → 1.0.0
  Modified principles: N/A (initial ratification)
  Added sections:
    - Core Principles (5 principles)
    - Technology Stack
    - Development Workflow
    - Governance
  Removed sections: None
  Templates requiring updates:
    - .specify/templates/plan-template.md ✅ reviewed (no changes needed,
      Constitution Check section is dynamic)
    - .specify/templates/spec-template.md ✅ reviewed (no changes needed)
    - .specify/templates/tasks-template.md ✅ reviewed (no changes needed)
    - .specify/templates/commands/*.md — no command templates exist
  Follow-up TODOs: None
-->

# dnshealth_exporter Constitution

## Core Principles

### I. Robust Integration Testing

All DNS check functionality MUST be validated through integration tests
that exercise real DNS resolution paths. Unit tests alone are
insufficient for a network-dependent exporter.

- Integration tests MUST cover each DNS check type end-to-end.
- Tests MUST validate both successful responses and common failure
  modes (NXDOMAIN, SERVFAIL, timeout, truncation).
- Test infrastructure MUST support running against controlled DNS
  fixtures (e.g., a local DNS server or well-known stable zones)
  to ensure deterministic results in CI.
- New collector functionality MUST NOT be merged without
  corresponding integration test coverage.

### II. Prometheus Naming Conventions and Standards

All exported metrics MUST comply with Prometheus naming conventions
and the OpenMetrics specification.

- Metric names MUST use the `dnshealth_` namespace prefix.
- Metric names MUST use snake_case and include a unit suffix where
  applicable (e.g., `_seconds`, `_bytes`, `_total`).
- Counter metrics MUST use the `_total` suffix.
- Labels MUST be lowercase, use snake_case, and avoid high
  cardinality (no unbounded label values such as raw query names
  from user input).
- The exporter MUST expose a `dnshealth_build_info` gauge with
  version, revision, and Go version labels.
- The `/metrics` endpoint MUST be compatible with standard
  Prometheus scrape configurations.

### III. Modern Go Ecosystem

The exporter MUST target a current, supported Go version and
prefer well-maintained, idiomatic libraries over hand-rolled
solutions.

- The project MUST use a Go version within the two most recent
  stable release series (e.g., 1.25.x or 1.26.x as of writing).
- DNS resolution MUST use a proven library (e.g., `miekg/dns`)
  rather than the standard library's limited resolver.
- Prometheus instrumentation MUST use the official
  `prometheus/client_golang` library.
- Dependencies MUST be tracked with Go modules (`go.mod`) and
  kept up to date; `go.sum` MUST be committed.
- Prefer standard library where sufficient; add third-party
  dependencies only when they provide clear, demonstrable value.

### IV. Structured Logging

All log output MUST be structured (key-value or JSON) to support
machine parsing, filtering, and integration with log aggregation
systems.

- The exporter MUST use `promslog` (the Prometheus slog wrapper,
  see Principle VI) for ecosystem consistency.
- Log levels MUST be used consistently: `ERROR` for failures
  requiring operator attention, `WARN` for degraded states,
  `INFO` for operational lifecycle events, `DEBUG` for
  diagnostic detail.
- DNS check failures MUST be logged at `WARN` or `DEBUG` level
  (they are expected operational signals, not exporter errors).
- Log output MUST NOT include sensitive data (credentials, API
  keys).

### V. Zone-Focused Detection Scope

The exporter monitors **DNS zones** — not individual hosts or
arbitrary queries. Its purpose is to surface zone health signals
as Prometheus metrics, inspired by tools like intodns.com, so
that Grafana dashboards can detect problems. Policy decisions
(thresholds, alerting rules, SLA definitions) belong in
Grafana/Alertmanager, not in the exporter.

- The exporter MUST expose raw, granular metrics rather than
  pre-computed health scores or pass/fail verdicts.
- The exporter MUST NOT embed alerting logic or threshold
  evaluation in metric values.
- Configuration determines which zones and check types to run,
  not what constitutes "healthy" — that judgment belongs to the
  dashboard and alerting layer.
- Labels MUST enable flexible grouping (by zone, nameserver,
  check category) to support building rich Grafana dashboards.

### VI. Prometheus Ecosystem Conventions

The exporter MUST follow the conventions established by official
Prometheus exporters. When making architectural decisions, consult
these reference projects:

- **blackbox_exporter** — primary reference for project
  structure, prober pattern, YAML config with safe reload,
  and Prometheus ecosystem library choices (`kingpin/v2`,
  `exporter-toolkit`, `promslog`).
- **node_exporter** — reference for collector organization,
  flag-based feature toggles, and platform-aware code.
- **exporter-toolkit** — the shared library for TLS, basic
  auth, web server setup, and landing pages. MUST be used.
- **snmp_exporter** — reference for complex, declarative
  configuration patterns if zone config grows in complexity.

This is NOT a multi-target exporter — it runs checks on
configured zones, not on-demand per scrape request. Where
patterns from the reference projects assume multi-target
probing, adapt to the zone-monitoring model.

### VII. Well-Behaved Binary

The exporter MUST behave as a production-grade, long-running Go
service that operators can deploy, manage, and debug with
standard tooling.

- The binary MUST handle OS signals gracefully: SIGTERM and
  SIGINT MUST trigger a clean shutdown (drain in-flight scrapes,
  close listeners, flush logs) within a bounded timeout.
- SIGHUP SHOULD trigger configuration reload where feasible.
- The binary MUST expose a `/healthz` or equivalent health
  endpoint for liveness probes.
- The binary MUST exit with meaningful exit codes (0 for clean
  shutdown, non-zero for errors).
- Startup MUST fail fast on invalid configuration rather than
  running in a broken state.
- The binary MUST support standard operational flags (`--help`,
  `--version`, listen address, config file path, log level).

## Technology Stack

- **Language**: Go (latest two stable release series)
- **DNS Library**: `github.com/miekg/dns` (or equivalent mature
  library)
- **Metrics**: `github.com/prometheus/client_golang`
- **CLI Flags**: `github.com/alecthomas/kingpin/v2`
- **Web Server**: `github.com/prometheus/exporter-toolkit/web`
- **Logging**: `github.com/prometheus/common/promslog` (slog-based)
- **Config**: `go.yaml.in/yaml/v3`
- **Testing**: `go test` with integration test build tags
- **Target Platform**: Linux (primary), cross-platform where
  feasible
- **Project Type**: Prometheus exporter (long-running daemon)

## Development Workflow

- All changes MUST pass `go vet` and `go test ./...` before merge.
- Integration tests MUST be runnable in CI with a
  `-tags=integration` build tag or equivalent gating mechanism.
- Dependency updates SHOULD be reviewed for breaking changes and
  tested before merging.
- Commits SHOULD be atomic and describe the "why" of the change.
- The exporter MUST build and start cleanly with `go build` and
  expose metrics on the default `/metrics` endpoint without
  additional setup beyond configuration.

## Governance

- This constitution is the authoritative source of project
  principles. All design decisions, code reviews, and
  specifications MUST be consistent with these principles.
- Amendments require: (1) a documented rationale, (2) review of
  impact on existing specifications and plans, and (3) a version
  bump following semantic versioning (MAJOR for principle
  removal/redefinition, MINOR for additions, PATCH for
  clarifications).
- Compliance with these principles MUST be verified during
  specification review (via the Constitution Check gate in
  plan.md) and during code review.

**Version**: 1.0.0 | **Ratified**: 2026-04-21 | **Last Amended**: 2026-04-21
