# Implementation Plan: MX Prober Family

**Branch**: `008-mx-prober-family` | **Date**: 2026-05-23 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/008-mx-prober-family/spec.md`

## Summary

Add a per-zone MX prober that issues TypeMX queries against each authoritative nameserver, then for every returned MX target hostname validates resolution (A/AAAA presence), checks for CNAME at the target (RFC 2181 §10.3 violation), validates LDH syntax (reusing the existing `isValidNSHostname` helper from spec N6), detects Null MX records (RFC 7505), and classifies each MX as primary or backup based on its preference value. Emits per-MX info gauges, per-MX boolean validity gauges, per-zone count gauges (with the Reset+Set(0) pattern from spec 007 R-2), and a per-zone Null-MX boolean. Adds a new "MX — status" dashboard panel and a per-MX records table. Adds three new demo zones (clean multi-MX, Null MX, broken-MX) following the established pattern.

## Technical Context

**Language/Version**: Go 1.26.x (per constitution Principle III; codebase currently on go 1.26.2)
**Primary Dependencies**: `github.com/miekg/dns`, `github.com/prometheus/client_golang`, `github.com/prometheus/common/promslog`
**Storage**: N/A — exporter is stateless within a probe cycle
**Testing**: `go test -tags=integration` against in-process `miekg/dns` fixture servers via `testutil/`
**Target Platform**: Linux (primary); cross-platform per Go portability
**Project Type**: Prometheus exporter (long-running daemon)
**Performance Goals**: One TypeMX query per zone per cycle, plus one CNAME query per unique MX target, plus `ResolveHostnames` per unique MX target (which issues A + AAAA, so 2 queries each). Typical 4-MX zone: 1 + 4 + 4×2 = 13 queries per cycle; ~5-15 queries per cycle per zone overall. Bounded; well within budget.
**Constraints**: Must not change semantics of existing metric series (additive only). Per-MX cardinality bound = (zones × max-MX-per-zone), typically small (few hundred series at worst). Must pass `TestStatusChecksHaveDetail` guard test for new dashboard rows.
**Scale/Scope**: 8-11 demo zones after this feature lands; production deployments monitor similar zone counts; MX-set sizes per zone typically 1-4.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Robust Integration Testing | PASS | Six FR-coverage fixtures planned per SC-006 (healthy multi-MX, unresolvable target, CNAMEd target, Null MX, Null-MX-coexists-with-MX error, no-MX-and-no-Null-MX). All use `testutil/` infrastructure. |
| II. Prometheus Naming Conventions | PASS | New metrics use `dnshealth_mx_` prefix, snake_case, bounded label cardinality (zone, target, priority as label). No `_total` misuse — all gauges, not counters. |
| III. Modern Go Ecosystem | PASS | No new third-party dependencies. Reuses `miekg/dns` TypeMX support and existing `client_golang` patterns. |
| IV. Structured Logging | PASS | New prober uses the existing `*slog.Logger` plumbing. WARN for noteworthy detections (unresolvable MX, CNAMEd MX); DEBUG for per-MX classification details. |
| V. Zone-Focused Detection Scope | PASS | Metrics expose raw per-MX state via labels; threshold judgments belong in Grafana/Alertmanager. Dashboard row detail text frames Null MX as legitimate-when-intentional, similar to the SOA-MNAME row from spec 006. |
| VI. Prometheus Ecosystem Conventions | PASS | New prober registered via `RegisterProber("mx", ProbeMX)`, matching the established pattern. |
| VII. Well-Behaved Binary | PASS | No changes to startup, shutdown, signal handling, or config schema. Purely additive at the metric / dashboard / demo layer. |
| VIII. Readable, Honest Tests | PASS | All new tests follow three-phase Meszaros structure using `testutil/` fixtures. Defaults-with-override pattern. New dashboard rows carry `detail` text enforced by `TestStatusChecksHaveDetail`. |

**No principle violations identified. No entries needed in Complexity Tracking.**

## Project Structure

### Documentation (this feature)

```text
specs/008-mx-prober-family/
├── plan.md              # This file
├── research.md          # Phase 0 — decisions for MX query semantics, Null MX detection, primary/backup classification, dashboard panel placement
├── data-model.md        # Phase 1 — MXRecord, NullMXState, MXClassification entities + per-cycle lifecycle
├── quickstart.md        # Phase 1 — operator-facing "how to read the new metrics" guide with PromQL recipes
├── contracts/           # Phase 1 — external metric + dashboard contracts
│   ├── mx-metrics.md
│   └── dashboard-panel.md
├── spec.md              # Already written
├── checklists/
│   └── requirements.md  # Already written
└── tasks.md             # Phase 2 (created by /speckit-tasks)
```

### Source Code (repository root)

```text
prober/
├── mx.go                                  # NEW — the MX prober (TypeMX query, parses responses, classifies, runs CNAME + syntax checks per target)
├── mx_test.go                             # NEW — integration tests (build tag: integration)
├── ns_hostname.go                         # Existing — provides `isValidNSHostname` + `lookupCNAME` helpers, REUSED for MX target validation
├── prober.go                              # Existing — provides `ResolveHostnames` for MX target resolution
└── ...

cycle/
└── runner.go                              # MODIFIED — register the per-zone count gauges + Null-MX gauge; aggregation pass in Run() to derive counts from MX prober results

demo/
├── coredns/
│   ├── mx-healthy/                        # NEW — multi-MX clean case
│   │   ├── Corefile
│   │   └── zones/mx-healthy.demo.zone
│   ├── mx-null/                           # NEW — Null MX (RFC 7505) case
│   │   ├── Corefile
│   │   └── zones/mx-null.demo.zone
│   ├── mx-broken/                         # NEW — CNAMEd MX target case
│   │   ├── Corefile
│   │   └── zones/mx-broken.demo.zone
│   └── root/zones/demo.zone               # MODIFIED — add 3 new delegations
├── docker-compose.yml                     # MODIFIED — 3 new coredns-mx-* services
├── exporter/dnshealth.yml                 # MODIFIED — add 3 zones
├── smoke.sh                               # MODIFIED — A4g/A4h/A4i assertions per zone
└── dashboard/
    ├── panels_status.go                   # MODIFIED — 4-5 new statusChecks for MX health
    ├── panels_records.go                  # MODIFIED — new per-MX joined table (similar to selfNSRecordsTable)
    └── dashboard.go                       # MODIFIED — wire the new per-MX table into buildOverview
```

**Structure Decision**: Single-project layout, additive throughout. One new prober file follows the established per-check pattern (matching `prober/glue.go`, `prober/soa.go`, `prober/ns_hostname.go`, `prober/ns_classification.go`). Three new demo containers — chose to land all three in this PR rather than punt the broken case to a follow-up, because the per-MX validity flags need both healthy and broken-zone series to render meaningfully on the dashboard. The dashboard adds 4-5 status rows AND a new per-MX records table — splitting the table to a follow-up would leave operators without a way to see which specific MX failed (only the aggregated PASS/FAIL row).

## Complexity Tracking

> No Constitution Check violations; this section intentionally empty.
