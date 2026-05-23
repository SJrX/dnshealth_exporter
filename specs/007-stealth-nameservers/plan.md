# Implementation Plan: Stealth Nameserver Detection

**Branch**: `007-stealth-nameservers` | **Date**: 2026-05-23 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/007-stealth-nameservers/spec.md`

## Summary

Add a per-zone NS-asymmetry classifier and a status-table row that surfaces NS hostnames present in the parent's referral but not the auth-side self-report (or vice versa). The detection consumes data the existing glue prober already gathers (`dnshealth_ns_record{source="parent"|"self"}`) and emits new classification series plus a per-zone count gauge. The new status row uses the existing PASS/FAIL pattern with detail text that explicitly disclaims the RFC-strict zero-knowledge stealth case as out of reach. A new demo zone exercises the self-only ("hidden master") case end-to-end with a smoke assertion.

## Technical Context

**Language/Version**: Go 1.26.x (per constitution Principle III; codebase currently on go 1.26.2)
**Primary Dependencies**: `github.com/miekg/dns`, `github.com/prometheus/client_golang`, `github.com/prometheus/common/promslog`
**Storage**: N/A — exporter is stateless within a probe cycle; only ephemeral data structures used during classification
**Testing**: `go test -tags=integration` against in-process `miekg/dns` fixture servers via `testutil/` package
**Target Platform**: Linux (primary); cross-platform per Go portability
**Project Type**: Prometheus exporter (long-running daemon)
**Performance Goals**: Classification computation is O(N) over the union of (parent ∪ self) NS hostnames per zone, where N is typically ≤ 10. Adds zero new DNS queries per cycle.
**Constraints**: Must not change semantics of existing `dnshealth_ns_record` series (additive only). Detection MUST distinguish "no divergence" from "no data this cycle" per FR-008. Must pass `TestStatusChecksHaveDetail` (new dashboard row needs `detail` text).
**Scale/Scope**: ~7-10 demo zones; the personal-zone deployment monitors a similar order. NS-set sizes per zone typically 2-4.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Robust Integration Testing | PASS | New prober will have integration tests covering happy path (no divergence), self-only stealth, parent-only, multi-auth-union convergence per SC-006. Uses `testutil/` fixture infrastructure. |
| II. Prometheus Naming Conventions | PASS | New metrics will use `dnshealth_` prefix, snake_case, bounded label cardinality (zone, nameserver, classification). No `_total` needed for gauges; per-zone count gauge follows existing pattern (e.g., `dnshealth_probe_zones_total` is the precedent for gauge-named-with-`_total`, but the new count gauge will be named without that suffix since it's an instantaneous count, not a counter). |
| III. Modern Go Ecosystem | PASS | No new third-party dependencies. Uses existing `miekg/dns`, `client_golang`. |
| IV. Structured Logging | PASS | New prober uses the existing `*slog.Logger` passed through the prober contract. WARN for noteworthy detections (e.g., self-only stealth observed), DEBUG for per-zone classification details. |
| V. Zone-Focused Detection Scope | PASS | The metric exposes raw classification per (zone, nameserver); threshold judgments ("is this an incident?") belong in Grafana/Alertmanager. Dashboard row surfaces the boolean but the detail text explicitly notes hidden-master legitimacy. |
| VI. Prometheus Ecosystem Conventions | PASS | New prober registered via `RegisterProber()` pattern matching existing probers (soa, recursion, ns_hostname). |
| VII. Well-Behaved Binary | PASS | No changes to startup, shutdown, signal handling, or config schema. Purely additive at the metric/dashboard layer. |
| VIII. Readable, Honest Tests | PASS | Tests written in three-phase Meszaros structure using `testutil/` fixtures. Defaults-with-override pattern. New dashboard row carries `detail` text enforced by the existing `TestStatusChecksHaveDetail` guard test. |

**No principle violations identified. No entries needed in Complexity Tracking.**

## Project Structure

### Documentation (this feature)

```text
specs/007-stealth-nameservers/
├── plan.md              # This file
├── research.md          # Phase 0 — decisions for classification scheme, metric shape, edge cases
├── data-model.md        # Phase 1 — Classification entity and its lifecycle within a probe cycle
├── quickstart.md        # Phase 1 — operator-facing "how to read the new metrics" guide
├── contracts/           # Phase 1 — classification metric shape + dashboard row contract
│   ├── classification-metric.md
│   └── dashboard-row.md
├── spec.md              # Already written
├── checklists/
│   └── requirements.md  # Already written
└── tasks.md             # Phase 2 (created by /speckit-tasks)
```

### Source Code (repository root)

```text
prober/
├── prober.go                              # Existing — no changes
├── ns_classification.go                   # NEW — the classifier prober
├── ns_classification_test.go              # NEW — integration tests (built with -tags=integration)
├── glue.go                                # Existing — unchanged; we read its output downstream
├── soa.go                                 # Existing — unchanged
├── ns_hostname.go                         # Existing — unchanged
└── ...

cycle/
└── runner.go                              # Existing — no changes; the new prober plugs in via RegisterProber

demo/
├── coredns/
│   ├── hidden-master/                     # NEW — auth container for the demo zone exercising self-only stealth
│   │   ├── Corefile
│   │   └── zones/
│   │       └── hidden-master.demo.zone
│   └── root/zones/demo.zone               # MODIFIED — add delegation for hidden-master.demo.
├── docker-compose.yml                     # MODIFIED — add coredns-hidden-master service (new container)
├── exporter/dnshealth.yml                 # MODIFIED — add hidden-master.demo. to zones list
├── smoke.sh                               # MODIFIED — A3d assertion: stealth NS surfaces correctly
└── dashboard/
    └── panels_status.go                   # MODIFIED — new statusCheck row + detail text
```

**Structure Decision**: Single-project layout matching the existing repo. New code is purely additive (one new prober file, one new prober-test file, one new demo container) plus one-line additions to the dashboard / smoke / compose / config files following established patterns (recent precedent: S1 / N2+N6 / #36 / #26 / #29). No package reorg or new top-level directory.

## Complexity Tracking

> No Constitution Check violations; this section intentionally empty.
