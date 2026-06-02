# Contract: "Email auth — status" Dashboard Panel

A new status-table panel built with the existing `statusTable` builder and the four-state `composeStatusExpr` machinery (constitution Principle IX). Four rows in v1 (the deferred lookup-budget check, [#58](https://github.com/SJrX/dnshealth_exporter/issues/58), adds a fifth). All threshold logic is PromQL in the dashboard; the exporter emits only raw signals.

## Severity model (from spec Clarifications §2026-05-31)

**FAIL = the record is broken (structural/PermError); WARN = absent or weak/reckless policy; PASS = present and safe.** Rows apply to every zone independent of MX/Null-MX (FR-017); N/A is only for within-email-auth inapplicability (FR-013).

## Rows

| Row | refId | PASS (1) | WARN (3) | FAIL (0) | N/A (2) |
|-----|-------|----------|----------|----------|---------|
| **A. Zone publishes a single valid SPF record** | A | `spf_present=1 ∧ spf_record_count=1 ∧ spf_valid=1` | `spf_present=0` (absent) | `spf_record_count>1` ∨ (`spf_present=1 ∧ spf_valid=0`) | — |
| **B. SPF ends in a restrictive `all` qualifier** | B | `spf_terminal_all{qualifier=~"fail\|softfail"}` | `qualifier=~"neutral\|none\|pass"` (incl. `+all`) | — | `spf_present=0` |
| **C. Zone publishes a valid DMARC record** | C | `dmarc_present=1 ∧ dmarc_valid=1` | `dmarc_present=0` (absent) | `dmarc_present=1 ∧ dmarc_valid=0` (malformed) | — |
| **D. DMARC enforces a policy** | D | `dmarc_policy{policy=~"quarantine\|reject"}` | `dmarc_policy{policy="none"}` | — | `dmarc_present=0` |

> The deferred lookup-budget check (#58) inserts an **"SPF within the 10-lookup budget"** row (FAIL when `spf_lookup_budget_exceeded=1`, N/A when no SPF) between rows B and C.

Notes:
- Row B WARN deliberately includes `pass` (`+all`) — a syntactically valid but reckless policy → WARN, not FAIL (clarification).
- Each predicate ends with the standard `or on() vector(0)` / scalarized form so it always renders a value; `naExpr` uses `dnshealth_spf_present{zone="$zone"} == bool 0` (or `absent(...)`) so the SPF-dependent rows read N/A, not FAIL, when there is no SPF.

## Detail text (FR-012, guard-test enforced)

Every row carries markdown detail with: the backing metric, what each non-PASS state means operationally, the anti-spoofing rationale (FR-017 — why a no-mail/Null-MX zone still wants these records, so its WARN is not read as a contradiction), and where to investigate. Example for row A:

> **Metric**: `dnshealth_spf_present` / `dnshealth_spf_record_count` / `dnshealth_spf_valid`
> **WARN (absent)**: the zone publishes no SPF record. Even a domain that sends no mail should publish `v=spf1 -all` to stop spammers forging it as the sender — SPF protects the domain regardless of whether it receives mail (MX). Verify intent.
> **FAIL (broken)**: more than one `v=spf1` record (RFC 7208 §3.2 PermError) or a malformed record — receivers get a permanent error and SPF effectively does not work.
> **Investigate**: the zone's apex TXT records.

## Placement & generation

- New `emailAuthStatusChecks` slice in `panels_status.go`; new `emailAuthStatusTable(yOffset)` wired into `buildOverview` (likely its own collapsible section, mirroring the MX section).
- Generated from the Foundation SDK; the drift test (`TestDashboardJSONMatchesGenerator`) must cover the regenerated committed JSON; the detail guard (`TestStatusChecksHaveDetail`) must pass; the `promql_live` test must pin the new cells across the new demo zones.

## Validation cells (promql_live pins)

| Zone | A (SPF valid) | B (SPF qualifier) | C (DMARC valid) | D (DMARC policy) |
|------|---|---|---|---|
| `email-healthy.demo.` | PASS | PASS | PASS | PASS |
| `email-spf-only.demo.` | PASS | PASS | WARN | N/A |
| `email-none.demo.` | WARN | N/A | WARN | N/A |
| `email-permissive.demo.` | PASS | WARN | PASS | WARN |
| `email-broken.demo.` | FAIL (multiple SPF records) | N/A or PASS | FAIL (malformed DMARC) | N/A |
| `email-nomail.demo.` | PASS | PASS | PASS | PASS |

(Exact `email-broken` cells depend on which broken facets that zone encodes — multiple SPF records makes row A FAIL and row B N/A; pinned precisely when the demo zone is authored.)
