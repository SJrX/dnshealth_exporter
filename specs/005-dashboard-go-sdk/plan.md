# Implementation Plan: 005-dashboard-go-sdk

**Branch**: `005-dashboard-go-sdk` | **Date**: 2026-05-15 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/005-dashboard-go-sdk/spec.md`

## Summary

Replace the hand-written 450-line Grafana dashboard JSON
(`demo/grafana/dashboards/dnshealth-overview.json`) with a typed Go
program in `demo/dashboard/` that uses the Grafana Foundation SDK
(Go) to generate **two** dashboard variants from one shared
definition: the existing full dashboard (with the markdown info-text
panel) and a clean variant without the info-text panel. The
generated JSON files remain committed to the repository so operators
do not need a Go toolchain to run the demo. The exporter's demo host
port changes from `9266` → `9053` (DNS-themed, prober range);
Grafana and Prometheus stay on `3000` / `9090`; all three remain
overridable via env vars.

**Technical approach** (from research):
- Pin SDK to `github.com/grafana/grafana-foundation-sdk/go@v11.6.x+cog-v0.0.x`.
- Generator is a Go `main` package under `demo/dashboard/` that
  shares `go.mod` with the exporter (the exporter never imports it,
  so it stays out of the production binary).
- Two emitted files differentiated by a single boolean flag and
  distinct `uid`s.
- Determinism comes for free from `encoding/json` + Go's sorted-map-
  key marshalling; a `go test` golden-file check (built under
  `-tags=integration`) detects drift.
- Transformations stay open-typed in the SDK; we wrap each of the
  four IDs (`joinByField`, `organize`, `filterByValue`, `reduce`) in
  small project-local helper structs to catch field typos at
  compile time.

## Technical Context

**Language/Version**: Go 1.26.2 (matches `go.mod`).
**Primary Dependencies**:
  - `github.com/grafana/grafana-foundation-sdk/go` — pinned to
    `v11.6.x+cog-v0.0.x` (will resolve to a pseudo-version in
    `go.sum`).
  - All other deps already present in `go.mod` (no additions).
**Storage**: N/A (file output to fixed paths under
`demo/grafana/dashboards/`).
**Testing**: `go test -tags=integration ./demo/dashboard/...` —
golden-file equality test (drift detection).
**Target Platform**: Linux/macOS dev machines with the Go toolchain
installed. The generator never runs in production or in containers;
operators provision Grafana from the committed JSON.
**Project Type**: Small Go CLI (`main` package) co-located with the
demo (`demo/dashboard/`). Reuses the project's existing single-
module layout.
**Performance Goals**: Generator completes in under 5 seconds on a
typical dev machine. Not perf-critical.
**Constraints**:
  - Generator MUST NOT be importable from the exporter binary
    (enforced by Go's lazy-link semantics; reinforced by a comment
    in `go.mod`).
  - Generated JSON MUST be byte-identical across runs for fixed
    inputs (FR-004, SC-006).
  - The committed JSON MUST be runnable by Grafana provisioning
    without a Go toolchain (FR-006).
**Scale/Scope**:
  - 2 dashboard variants × ~10 panels = 20 panel emissions.
  - Estimated <800 LoC of Go (10 panel functions + builder + main +
    drift test).
  - 1 port default change (`9266` → `9053`); 2 README updates.

## Constitution Check

**Principles evaluated against `.specify/memory/constitution.md` v1.1.1.**

| Principle | Relevance | Status |
|-----------|-----------|--------|
| I. Robust Integration Testing | The generator's drift test runs under `-tags=integration` (consistent with project convention). The smoke test covers the demo end-to-end and is the integration gate for the dashboard layer. | PASS |
| II. Prometheus Naming Conventions | The dashboard *consumes* exporter metric names; this work introduces no new metrics. The drift test would catch a renamed metric ref before merge. | PASS |
| III. Modern Go Ecosystem | Go 1.26.2 is the latest pinned. The SDK is well-maintained, used by Grafana itself, and replaces hand-rolled JSON. Adds one dep to `go.mod`; the exporter binary does not import it. | PASS |
| IV. Structured Logging | Generator is a one-shot CLI with `panic(err)` on failure (idiomatic for code generators). Does not run as a service; structured logging would be ceremony with no consumer. | PASS (N/A in spirit) |
| V. Zone-Focused Detection Scope | This work is dashboard-only. Exporter behaviour unchanged. | PASS (N/A) |
| VI. Prometheus Ecosystem Conventions | The dashboard remains a standard Grafana provisioning artifact. No deviation. | PASS |
| VII. Well-Behaved Binary | Applies to the exporter, not the generator. The exporter's bind-failure behaviour was verified (`main.go:170`) and called out in the spec. | PASS |
| VIII. Readable, Honest Tests | The drift test is one assertion: `bytes.Equal(committed, generated)`. Three-phase structure trivially holds: setup = run generator; exercise = marshal; verify = byte-equal with committed file. The test under `-tags=integration` lives at `demo/dashboard/dashboard_test.go`. | PASS |

**Gate verdict**: PASS. No principle violations; no entries in
Complexity Tracking required.

**Post-design re-check (after Phase 1)**: PASS. Data model and
contracts introduced no new dependencies, no service boundaries,
and no metrics. The two-variant requirement (R-5) is satisfied with
a single boolean parameter — no new entities, no new constitutional
considerations.

## Project Structure

### Documentation (this feature)

```text
specs/005-dashboard-go-sdk/
├── plan.md                        # This file
├── research.md                    # Phase 0: SDK survey + decisions
├── data-model.md                  # Phase 1: in-memory entities
├── quickstart.md                  # Phase 1: maintainer ops summary
├── contracts/
│   ├── generator-cli.md           # CLI invocation contract
│   ├── dashboard-output.md        # Output JSON invariants
│   └── port-mappings.md           # Demo port-binding contract
├── checklists/
│   └── requirements.md            # Spec quality checklist
└── tasks.md                       # Phase 2 (NOT created here — `/speckit.tasks`)
```

### Source Code (repository root)

```text
.
├── go.mod                         # +1 dep (Grafana Foundation SDK)
├── go.sum                         # locked pseudo-version
├── Makefile                       # +1 target: `dashboards`
├── README.md                      # links to demo regen command
├── main.go                        # unchanged (exporter)
├── prober/                        # unchanged
├── ...
└── demo/
    ├── README.md                  # docs new ports + regen workflow
    ├── docker-compose.yml         # exporter port default 9266 → 9053
    ├── smoke.sh                   # default EXPORTER_PORT 9266 → 9053
    ├── grafana/
    │   ├── provisioning/          # unchanged
    │   └── dashboards/
    │       ├── dnshealth-overview.json        # regenerated, committed
    │       └── dnshealth-overview-clean.json  # NEW, regenerated, committed
    └── dashboard/                 # NEW directory
        ├── main.go                # generator entry; writes both JSONs
        ├── dashboard.go           # buildOverview(includeInfoText bool)
        ├── panels_info.go         # infoTextPanel()
        ├── panels_status.go       # parent/ns/soa status tables
        ├── panels_records.go      # NS records + SOA serials tables
        ├── panels_operator.go     # operator timeseries panels
        ├── transforms.go          # JoinByField/Organize/etc helpers
        └── dashboard_test.go      # drift test, build tag integration
```

**Structure Decision**: Single-module layout (already in use). The
generator is a `main` package under `demo/dashboard/` — a sibling
of the exporter `main` package at the repo root. Both packages
share `go.mod` for simplicity; the SDK dep is a pure dev/test
dependency from the exporter's perspective and is stripped by Go
linking when building the exporter.

## Complexity Tracking

> No constitution violations to justify. This section intentionally
> empty.
