# Implementation Plan: Email-Authentication DNS Records (Tier 1: SPF + DMARC)

**Branch**: `009-email-auth-records` | **Date**: 2026-05-31 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/009-email-auth-records/spec.md`

## Summary

Add a per-zone `email_auth` prober that issues two TypeTXT queries — the zone apex (SPF) and `_dmarc.<zone>` (DMARC) — interprets the published email-authentication records with **pure string parsing (no recursion, no new dependency)**, and emits granular `dnshealth_*` gauges. SPF v1 detects presence, the multiple-record PermError (RFC 7208 §3.2), and the terminal `all` qualifier (`-all`/`~all`/`?all`/`+all`/none) — a syntactic read of the apex record only. DMARC detects presence, the `p=` enforcement policy (info-gauge label), well-formedness, and optional `sp=`/`rua`/`ruf`. The feature adds a new "Email auth — status" dashboard panel whose four-state rows encode the clarified severity model (broken record → FAIL; absent/weak policy → WARN; safe → PASS), MX-independent (FR-017). The SPF DNS-lookup-budget check (US3/P3) — the only part requiring recursive `include`/`redirect` resolution — is **deferred to [#58](https://github.com/SJrX/dnshealth_exporter/issues/58)**, which keeps this cycle dependency-free and resolver-free. Adds demo zones for every covered state and smoke assertions for happy + broken cases. Entirely additive; no config-schema, transport, or startup changes.

## Technical Context

**Language/Version**: Go 1.26.x (per constitution Principle III; codebase currently on go 1.26.2)
**Primary Dependencies**: `github.com/miekg/dns` (TypeTXT), `github.com/prometheus/client_golang`, `github.com/prometheus/common/promslog`, Grafana Foundation SDK (dashboard generation). **No new third-party dependencies** — v1 SPF and DMARC are both pure string parsing. (A library was evaluated for the SPF lookup-budget check; that check is deferred to #58, so no dependency is added now — see research R-3.)
**Storage**: N/A — exporter is stateless within a probe cycle.
**Testing**: `go test -tags=integration` against in-process `miekg/dns` fixture servers via `testutil/`; dashboard drift + detail-guard tests in the default build; `promql_live` gate via `demo/smoke.sh`.
**Target Platform**: Linux (primary); cross-platform per Go portability.
**Project Type**: Prometheus exporter (long-running daemon) + code-generated Grafana dashboard.
**Performance Goals**: Per zone per cycle — exactly 2 TXT queries (apex for SPF, `_dmarc.<zone>` for DMARC). No recursive resolution (the include walk is deferred to #58). Trivially bounded.
**Constraints**: Additive only — must not change any existing metric series. New per-zone gauges add bounded cardinality (zones × small constant; info-gauge labels are low-cardinality enums: qualifier ∈ {fail,softfail,neutral,pass,none}, policy ∈ {none,quarantine,reject}). New dashboard rows must pass `TestStatusChecksHaveDetail`.
**Scale/Scope**: 13 demo zones today → ~18–19 after this feature; one new prober file, two pure parser files, runner registration, one new dashboard status panel, ~6 demo zones, smoke assertions.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Robust Integration Testing | PASS | Per-FR fixtures planned via `testutil/`: healthy SPF+DMARC, SPF-only, no-SPF, `+all`, multiple-SPF (PermError), malformed DMARC, `p=none`, and a Null-MX-with-good-email-auth zone (FR-017). Both happy and broken paths per record type (FR-015). |
| II. Prometheus Naming Conventions | PASS | New metrics use `dnshealth_spf_` / `dnshealth_dmarc_` prefixes, snake_case; all gauges (no `_total`). Info-gauge labels (`qualifier`, `policy`) are low-cardinality enums. |
| III. Modern Go Ecosystem | PASS | No new dependencies. v1 SPF and DMARC are pure string parsing; the only part that would have justified a library (the recursive lookup-budget walk) is deferred to #58. Reuses `miekg/dns` TypeTXT only. |
| IV. Structured Logging | PASS | New prober uses the existing `*slog.Logger`. WARN for noteworthy detections (malformed records, multiple SPF records); DEBUG for parse detail. SPF/DMARC are public records; logging stays terse and non-sensitive. |
| V. Zone-Focused Detection Scope | PASS | Exporter emits raw signals (present, qualifier, policy); all FAIL/WARN/PASS threshold logic lives in dashboard PromQL. The clarified severity mapping is encoded in the dashboard, not the exporter. |
| VI. Prometheus Ecosystem Conventions | PASS | New prober registered via `RegisterProber("email_auth", ProbeEmailAuth)`, matching `glue`/`soa`/`mx`/`ns_classification`. |
| VII. Well-Behaved Binary | PASS | No changes to startup, shutdown, signals, health, or config schema. Reuses the existing per-zone `zones` config list — email-auth needs no new config (FR-017: applies to every configured zone). |
| VIII. Readable, Honest Tests | PASS | New tests follow three-phase Meszaros structure with defaults-with-override `testutil/` fixtures; pure parsers unit-tested table-driven; new dashboard rows carry `detail` text enforced by the guard test. |
| IX. Dashboard Surfacing & Conventions | PASS | Every new metric family ships with the "Email auth — status" panel in the same change; rows use the four-state convention with N/A reserved for within-email-auth inapplicability (FR-013), threshold logic in PromQL, detail text on every row, generated from the SDK with the drift test covering the new JSON. |

**No principle violations identified. Complexity Tracking intentionally empty.**

## Project Structure

### Documentation (this feature)

```text
specs/009-email-auth-records/
├── plan.md              # This file
├── research.md          # Phase 0 — TXT query/concatenation, SPF parse scope, DMARC parse scope, metric design, dashboard rows, demo-zone construction, lookup-budget deferral
├── data-model.md        # Phase 1 — SPFRecord, DMARCRecord, EmailAuthResult entities + per-cycle lifecycle + metric mapping
├── quickstart.md        # Phase 1 — operator-facing "how to read the new metrics" with PromQL recipes
├── contracts/           # Phase 1 — external metric + dashboard contracts
│   ├── email-auth-metrics.md
│   └── dashboard-panel.md
├── spec.md              # Written (/speckit.specify) + clarified (/speckit.clarify)
├── checklists/
│   └── requirements.md  # Written
└── tasks.md             # Phase 2 (created by /speckit.tasks)
```

### Source Code (repository root)

```text
prober/
├── email_auth.go                          # NEW — the email_auth prober: TXT query at apex + _dmarc, drives the pure SPF + DMARC parsers, emits ProbeResults
├── email_auth_test.go                     # NEW — integration tests (build tag: integration) via testutil/ fixtures
├── spf.go                                 # NEW — pure SPF parser: select v=spf1 record(s), count them, find terminal `all` qualifier. No DNS, no recursion.
├── spf_test.go                            # NEW — table-driven unit tests for the SPF parser (no DNS)
├── dmarc.go                               # NEW — pure DMARC parser: tags, p=/sp= policy, rua/ruf presence, well-formedness
├── dmarc_test.go                          # NEW — table-driven unit tests for the DMARC parser (no DNS)
├── retry.go                               # Existing — ExchangeWithRetry reused for the two TXT queries
└── ...

cycle/
└── runner.go                              # MODIFIED — register email_auth in the per-zone run; per-zone gauges that must read 0 (rather than vanish) when a record is absent follow the spec 007/008 Reset+Set(0) zero-emission pattern

demo/
├── coredns/
│   ├── email-healthy/                     # NEW — valid SPF `-all` + DMARC `p=reject`
│   ├── email-spf-only/                    # NEW — SPF `-all`, no DMARC
│   ├── email-none/                        # NEW — no SPF, no DMARC
│   ├── email-permissive/                  # NEW — SPF `+all` and/or DMARC `p=none`
│   ├── email-broken/                      # NEW — multiple SPF records (PermError) and/or malformed DMARC (missing p=)
│   ├── email-nomail/                      # NEW — Null MX + SPF `-all` + DMARC `p=reject` (FR-017: PASS independent of MX)
│   └── root/zones/demo.zone               # MODIFIED — add the new delegations + the SPF include sub-records used by email-broken
├── docker-compose.yml                     # MODIFIED — new coredns-email-* services
├── exporter/dnshealth.yml                 # MODIFIED — add the new zones
├── smoke.sh                               # MODIFIED — happy + broken assertions per record type (FR-015)
└── dashboard/
    ├── panels_status.go                   # MODIFIED — new "Email auth — status" panel (emailAuthStatusChecks) with four four-state rows (SPF valid, SPF qualifier, DMARC valid, DMARC policy)
    ├── promql_live_test.go                # MODIFIED — add new zones to demoZones + pin the meaningful cells
    └── dashboard.go                        # MODIFIED — wire the new panel into buildOverview
```

**Structure Decision**: Single-project layout, additive throughout. SPF and DMARC are both small pure parsers (`spf.go`, `dmarc.go`) — unit-tested table-driven with zero DNS, mirroring how the project separates pure helpers (`ns_hostname.go`'s `isValidNSHostname`) from I/O probers. v1 SPF needs no recursion, no resolver, and no dependency because the only resolution-requiring check (the §4.6.4 lookup budget) is deferred to #58; the qualifier read is purely syntactic. `email_auth.go` handles the two TXT queries and ProbeResult emission, integration-tested against `testutil/` fixtures. All demo zones land in this PR rather than deferring the broken cases, because the dashboard rows need both healthy and adverse series to render meaningfully (same rationale as spec 008).

## Complexity Tracking

> No Constitution Check violations; this section intentionally empty.
