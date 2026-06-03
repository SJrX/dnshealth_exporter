# Implementation Plan: SPF DNS-Lookup Budget Check (RFC 7208 §4.6.4)

**Branch**: `010-spf-lookup-budget` | **Date**: 2026-06-01 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/010-spf-lookup-budget/spec.md`

## Summary

Complete the SPF story spec 009 deferred (#58): the RFC 7208 §4.6.4 ten-lookup budget. Extend the existing `email_auth` prober so that, for a zone with exactly one valid SPF record, it counts the DNS-lookup-incurring mechanisms (`include`, `a`, `mx`, `ptr`, `exists`, `redirect`) recursively and emits the three `dnshealth_spf_lookup_*` gauges spec 009 already reserved. A new four-state row in the existing "Email auth — status" panel reads FAIL over budget, PASS within, N/A when there's no single SPF record. **No new dependency** — the recursion runs on a small iterative-from-root resolver that generalizes the exporter's existing `WalkDelegation` so it can fetch the SPF TXT of an arbitrary `include`/`redirect` target, which keeps the demo fully offline. A demo zone chains includes past the limit; smoke + `promql_live` verify FAIL/PASS live.

**Key simplification (research R-1)**: counting does **not** require resolving the non-recursing mechanisms. `a`/`mx`/`ptr`/`exists` each cost `+1` and pull in no further SPF terms, so they are counted *syntactically* with zero DNS queries. Only `include` and `redirect` targets are actually resolved — to fetch their SPF record and recurse. The "resolver" surface is therefore just "fetch the SPF TXT for one name," not a general-purpose recursive resolver.

## Technical Context

**Language/Version**: Go 1.26.x (constitution Principle III; codebase on go 1.26.2)
**Primary Dependencies**: `github.com/miekg/dns`, `github.com/prometheus/client_golang`, `github.com/prometheus/common/promslog`, Grafana Foundation SDK. **No new third-party dependency** (clarification Q1 — `github.com/wttw/spf` rejected).
**Storage**: N/A — stateless within a probe cycle (an optional per-evaluation memo of fetched include records; no persistence).
**Testing**: `go test -tags=integration` against in-process `miekg/dns` fixtures (`testutil/`); pure unit tests for the counter via an **injected fetch function** (no DNS); dashboard drift + detail-guard in the default build; `promql_live` via `demo/smoke.sh`.
**Target Platform**: Linux (primary); cross-platform per Go portability.
**Project Type**: Prometheus exporter (long-running daemon) + code-generated Grafana dashboard.
**Performance Goals**: Per qualifying zone (single valid SPF record only), at most **11 TXT fetches** for `include`/`redirect` targets — the stop-at-11 rule + visited-set + depth cap bound the walk; `a`/`mx`/`ptr`/`exists` add zero queries. Zones with no SPF or multiple SPF records do zero extra work. The walk runs under the existing per-zone deadline.
**Constraints**: Additive only — must not change existing metric series or the spec-009 SPF/DMARC rows' verdicts. The recursive walk MUST be cycle-safe (visited set), depth-bounded, deadline-respecting, and MUST never abort the zone's probe (FR-003/FR-004/FR-006). New dashboard row must pass `TestStatusChecksHaveDetail`. Demo/tests stay offline.
**Scale/Scope**: One new counter file + one new resolver file + parser extension; wire into `email_auth.go`; one dashboard row; one demo zone (+ its offline include sub-records); smoke + live-test pins. ~19 demo zones → 20.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Robust Integration Testing | PASS | Counter is unit-tested table-driven via an injected fetch func (no DNS); the resolver + emitted metrics are integration-tested via `testutil/` fixtures (over-budget chain, in-budget, unreachable-include graceful-degradation, cyclic include). Demo + smoke + `promql_live` for the row. |
| II. Prometheus Naming Conventions | PASS | Uses the spec-009-reserved `dnshealth_spf_lookup_count` / `_budget_exceeded` / `_eval_complete` — snake_case, gauges, per-zone. |
| III. Modern Go Ecosystem | PASS | **No new dependency** (the explicit point of clarification Q1). Reuses `miekg/dns` + the existing `WalkDelegation`/`ExchangeWithRetry` primitives. |
| IV. Structured Logging | PASS | Existing `*slog.Logger`. DEBUG for per-include resolution failures and cycle/depth-guard trips; no record contents beyond the public SPF strings, kept terse. No WARN spam (over-budget is a dashboard verdict, not an exporter log). |
| V. Zone-Focused Detection Scope | PASS | Emits raw signals (count, exceeded, eval-complete); the FAIL/PASS/N/A threshold lives in dashboard PromQL. Per-zone, zone-anchored. |
| VI. Prometheus Ecosystem Conventions | PASS | Extends the existing `email_auth` prober (`RegisterProber("email_auth", …)`); no new prober or transport. |
| VII. Well-Behaved Binary | PASS | No startup/shutdown/config-schema change. The iterative resolver reuses the existing `RootServers` (already settable via config). |
| VIII. Readable, Honest Tests | PASS | Three-phase `testutil/` tests; the injected-fetch counter design keeps the tricky recursion under fast deterministic unit tests with no DNS. |
| IX. Dashboard Surfacing & Conventions | PASS | Fills the row slot spec 009 reserved; four-state, threshold in PromQL, detail text (incl. the "≥11" and "evaluation incomplete" caveats), generated from the SDK with the drift test covering the new JSON. |

**No principle violations. Complexity Tracking intentionally empty.**

## Project Structure

### Documentation (this feature)

```text
specs/010-spf-lookup-budget/
├── plan.md              # This file
├── research.md          # Phase 0 — counting algorithm, the resolve-only-include/redirect insight, the iterative resolver, graceful degradation + cycle/depth bounds, stop-at-11, dashboard-row placement, demo chain
├── data-model.md        # Phase 1 — SPFLookupResult + SPF mechanism classification + metric mapping
├── quickstart.md        # Phase 1 — operator "how to read the budget signals" + PromQL
├── contracts/
│   ├── lookup-metrics.md      # the 3 gauges (now shipping, vs spec 009's "reserved")
│   └── dashboard-row.md       # the new status row + predicates + placement/refId
├── spec.md              # Written + clarified
├── checklists/requirements.md
└── tasks.md             # Phase 2 (/speckit.tasks)
```

### Source Code (repository root)

```text
prober/
├── spf.go                    # MODIFIED — add a mechanism tokenizer the counter consumes: classify each term as include/redirect/a/mx/ptr/exists/other and expose its target; expose whether the record has an `all` (so redirect is ignored when present). Keep the existing presence/qualifier API untouched.
├── spf_lookup.go             # NEW — countSPFLookups(record, fetch FetchSPFFunc, ...) (count int, complete bool): the bounded recursive counter. Recurses only into include/redirect (R-1); +1 for a/mx/ptr/exists with no query; visited-set + depth cap; stop-at-11; macro/unreachable target ⇒ complete=false. Pure (fetch injected).
├── spf_lookup_test.go        # NEW — table-driven unit tests with a map-backed fake fetch (no DNS): in-budget, exactly-11, over-budget stop, cyclic include, unreachable include ⇒ incomplete, redirect-ignored-when-all, macro target.
├── spf_resolve.go            # NEW — resolveSPFRecord(ctx, name, client, logger) (record string, ok bool): the production FetchSPFFunc. Iterative-from-root: reuse WalkDelegation to find name's authoritative server, query TXT there, select the v=spf1 record. Offline-capable (resolves .demo from the fake root).
├── email_auth.go             # MODIFIED — when SPF present ∧ recordCount==1 ∧ valid, run countSPFLookups with resolveSPFRecord, emit spf_lookup_count / _budget_exceeded / _eval_complete (via the same per-zone ProbeResult pipeline as the other SPF gauges).
├── email_auth_test.go        # MODIFIED — integration tests for the emitted lookup gauges (chained-include over budget, in-budget, unreachable include).
└── prober.go                 # Existing — WalkDelegation / ResolveAddress / RootServers reused by spf_resolve.go.

demo/
├── coredns/
│   ├── email-toomanylookups/ # NEW — apex SPF chains include: to in-zone sub-records (_spf1.._spfN) summing to ≥11 lookups
│   └── root/zones/demo.zone  # MODIFIED — delegation + glue
├── docker-compose.yml        # MODIFIED — new coredns-email-toomanylookups service
├── exporter/dnshealth.yml    # MODIFIED — add the zone
├── smoke.sh                  # MODIFIED — over-budget FAIL + in-budget PASS assertions
└── dashboard/
    ├── panels_status.go      # MODIFIED — insert the "SPF within the 10-lookup budget" row into emailAuthStatusChecks (rendered in the SPF group; fresh refId to avoid renumbering the DMARC rows — see contracts/dashboard-row.md)
    └── promql_live_test.go   # MODIFIED — pins for the new row across demo zones (incl. email-healthy PASS, email-toomanylookups FAIL, no-SPF zones N/A)
```

**Structure Decision**: Single-project, additive. The counter (`spf_lookup.go`) is pure with the DNS fetch injected as a function value — so the genuinely tricky part (recursion, cycle/depth bounds, stop-at-11, graceful degradation) is unit-tested table-driven with zero DNS, while the small production resolver (`spf_resolve.go`) is integration-tested against `testutil/`. This mirrors the spec-009 split (pure parser + I/O prober) and the constitution's "real objects, readable tests." The dashboard row is **inserted in slice order within the SPF group** but carries a fresh `refId`, so existing DMARC rows keep their `refId`s and `promql_live` pins — no renumbering churn.

## Complexity Tracking

> No Constitution Check violations; section intentionally empty.
