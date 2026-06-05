# Feature Specification: SPF DNS-Lookup Budget Check (RFC 7208 §4.6.4)

**Feature Branch**: `010-spf-lookup-budget`
**Created**: 2026-06-01
**Status**: Draft
**Input**: User description: "Implement SPF DNS Lookup budget checks mentioned in Issue #58"

## Scope of this feature

Adds the one SPF check that spec 009 (email-auth Tier 1) deliberately deferred: the **RFC 7208 §4.6.4 ten-lookup budget**. An SPF record may only cause **10 DNS-lookup-incurring mechanisms** (`include`, `a`, `mx`, `ptr`, `exists`, and the `redirect` modifier) to be evaluated — counted **recursively** through every `include`/`redirect` target. Exceeding 10 is a hard **PermError** at receiving mail servers, which most treat as an outright SPF failure. It is the most subtle SPF misconfiguration: the apex record looks fine by eye, but the offending lookups are usually buried inside third-party `include:` records the operator doesn't control and never sees.

This feature is the natural completion of the SPF story: spec 009 already ships presence, single-record, and terminal-qualifier checks; spec 009's `contracts/email-auth-metrics.md` already **reserves** the `dnshealth_spf_lookup_*` metric names, and `contracts/dashboard-panel.md` already **reserves a dashboard row slot** ("SPF within the 10-lookup budget", between the SPF-qualifier row and the DMARC rows) for it. This feature fills both.

**In scope**:

- Counting the DNS-lookup-incurring mechanisms required to evaluate a zone's SPF record, recursing through `include`/`redirect` targets, against the limit of 10.
- A per-zone signal for "exceeds the budget" and the evaluated count itself.
- A per-zone "evaluation completeness" signal so an unreachable/slow `include` degrades gracefully (partial count) instead of producing a false over-budget verdict.
- A new dashboard row in the existing "Email auth — status" panel, and a demo zone whose SPF chains includes to exceed the limit.

**Explicitly out of scope**:

- Re-litigating the SPF parser or the other SPF/DMARC checks (spec 009 owns those).
- Evaluating SPF against a real sender IP, computing a pass/fail *result*, or expanding macros for evaluation — only the *lookup count* is needed.
- DKIM (#57) and all other email-auth records.

This feature requires the exporter to resolve **arbitrary external names** (the `include`/`redirect`/`a`/`mx`/etc. targets), which the existing checks — all anchored on the monitored zone's own authoritative servers — do not. That resolution capability is the substantive new surface; its shape is settled in the Clarifications below.

## Clarifications

### Session 2026-06-01

- Q: How should SPF lookup evaluation (the recursive resolution + counting) be implemented? → A: **Hand-roll, no new dependency.** Write the bounded recursive counter on top of an iterative-from-root resolver built by generalizing the exporter's existing delegation-walk primitives — so SPF target resolution uses the same root-anchored DNS path the exporter already trusts, and the demo's `.demo` include targets resolve from the in-stack fake root with no internet and no recursive-CoreDNS configuration. No new third-party dependency (`github.com/wttw/spf` is rejected: spec 009 R-4 already found it buys little once the resolver must be built regardless, and it is a 2022 pre-1.0 dependency). Full control of the count and graceful-degradation semantics.
- Q: Does this feature also enforce the RFC 7208 §4.6.4 "void lookup" limit (≤2 NXDOMAIN/NODATA lookups)? → A: **No — deferred to a follow-up.** This feature does the 10-lookup budget only (issue #58's explicit subject). The void-lookup cap is a separate, rarer sub-check that would need its own signal, dashboard row, and demo zone to surface honestly (Principle IX); bundling it roughly doubles the dashboard/demo surface. Left as a future issue; the recursive walk this feature builds can expose a void count later without rework.
- Q: For an over-budget record, does the count metric report the exact total or stop early at 11? → A: **Stop at 11 ("≥11").** In-budget records (≤10) report their exact count; an over-budget record stops the walk the moment the running count exceeds 10 and reports 11, meaning "≥11". This bounds the walk to ≤11 lookups (naturally satisfying the cycle-safety requirement — no unbounded work on a pathological 50-include record) while delivering the only signal the binary FAIL/PASS row needs. Detail text explains the "≥11" semantics.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See whether a zone's SPF exceeds the 10-lookup budget (Priority: P1) 🎯 MVP

A zone operator wants to know, per monitored zone, whether the zone's SPF record requires more than 10 DNS lookups to evaluate — because if it does, receiving mail servers return PermError and SPF silently stops working, even though the record looks syntactically perfect.

**Why this priority**: This is the entire point of the feature and the most subtle SPF failure mode there is. Operators almost never catch it by eye because the lookups are hidden inside third-party `include:` records. Surfacing it at probe time gives lead time before mail starts failing.

**Independent Test**: Deploy a demo zone whose SPF chains enough `include:` mechanisms (each pointing at a demo sub-record) to require ≥11 lookups, and a healthy zone well under the limit. Verify the exporter exposes the evaluated lookup count and a budget-exceeded signal for the over-budget zone and not for the healthy one, and that the dashboard row reads FAIL for the over-budget zone and PASS for the healthy one.

**Acceptance Scenarios**:

1. **Given** a zone whose SPF record and its recursively-included records require 11 DNS lookups to fully evaluate, **When** a probe cycle completes, **Then** the exporter exposes a lookup-count signal of 11 and a budget-exceeded signal reading 1, and the dashboard row reads FAIL.
2. **Given** a zone whose SPF record requires 8 DNS lookups, **When** a probe cycle completes, **Then** the lookup-count signal reads 8, the budget-exceeded signal reads 0, and the dashboard row reads PASS.
3. **Given** a zone with no SPF record (or multiple SPF records — the existing spec 009 row A FAIL), **When** a probe cycle completes, **Then** the budget check does not apply and the dashboard row reads N/A rather than a misleading 0-lookup PASS.
4. **Given** a zone whose SPF record contains a cyclic `include:` chain (A includes B includes A), **When** a probe cycle completes, **Then** the evaluation terminates (does not hang the probe) and the zone is reported as exceeding the budget, not as an error.

### User Story 2 - Trust the signal under partial failure (Priority: P2)

A zone operator must be able to trust that a FAIL on this row means "genuinely over budget," not "one included server happened to be slow or unreachable this cycle." An `include:` target that times out or returns NXDOMAIN must not be counted as if it pushed the zone over the limit.

**Why this priority**: Without this, the check is a false-alarm generator — third-party SPF includes are frequently slow or briefly unavailable, and a check that flips to FAIL on a transient include outage would train operators to ignore it. Lower than US1 because the core count is the headline; this makes it trustworthy.

**Independent Test**: Deploy a zone whose SPF includes a target that is unreachable (times out), with the reachable portion totalling fewer than 10 lookups. Verify the exporter reports the partial count, flags the evaluation as incomplete, and does NOT report the zone as over budget; the dashboard row does not read FAIL.

**Acceptance Scenarios**:

1. **Given** a zone whose SPF `include:` target is unreachable this cycle while the resolvable mechanisms total 6 lookups, **When** a probe cycle completes, **Then** the exporter exposes an evaluation-complete signal of 0 (incomplete), a lookup count reflecting what resolved, and a budget-exceeded signal of 0 — the row does NOT FAIL on a transient outage.
2. **Given** the same zone, **When** the unreachable include later resolves and the true total is still under 10, **Then** the evaluation-complete signal returns to 1 and the count reflects the full evaluation.
3. **Given** a zone genuinely over budget where every mechanism resolves, **When** a probe cycle completes, **Then** the evaluation-complete signal reads 1 and the budget-exceeded signal reads 1 — a real failure is reported with full confidence.

### Edge Cases

- **Recursing vs non-recursing mechanisms**: `include` and `redirect` pull in another SPF record (recurse and add its mechanisms); `a`, `mx`, `ptr`, `exists` each cost exactly one lookup but do not nest further SPF terms. The count MUST reflect this distinction.
- **The `redirect` modifier is only consulted when there is no `all` mechanism** (RFC 7208) — a record with both `all` and `redirect` does not follow the redirect, so the redirect costs no lookup in that case.
- **Cyclic or self-referential include chains** MUST be bounded (the 10-lookup limit itself bounds it, plus an explicit visited-set / depth guard) so a malicious or accidental loop cannot hang a probe.
- **A target that contains a macro** (`%{...}`) in a recursing mechanism cannot be resolved without sender context the exporter does not have; the feature treats such a branch as unresolvable and reflects it in the evaluation-completeness signal rather than guessing — see clarification.
- **Zone with no SPF, or multiple SPF records**: the budget row reads N/A (no single record to evaluate) — consistent with spec 009's severity model.
- **The exporter cannot resolve external names at all this cycle** (e.g. the resolution path is down): the row degrades to "incomplete," not a false FAIL.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST, for each configured zone that publishes a single valid SPF record, count the DNS-lookup-incurring mechanisms (`include`, `a`, `mx`, `ptr`, `exists`, `redirect`) required to evaluate the record, resolving `include`/`redirect` targets recursively and adding each target's own lookup-incurring mechanisms to the total.
- **FR-002**: The system MUST expose the evaluated lookup count and a boolean signal indicating whether the count exceeds the RFC 7208 §4.6.4 limit of 10.
- **FR-003**: The recursive evaluation MUST be bounded against cycles and runaway depth (a visited-set and/or depth guard in addition to the 10-lookup limit) so no SPF record can hang or unboundedly slow a probe cycle.
- **FR-004**: The system MUST degrade gracefully when an `include`/`redirect` target is unreachable, times out, or is otherwise unresolvable: it MUST count what resolved, expose an "evaluation incomplete" signal, and MUST NOT report the zone as over budget on the strength of unresolved lookups (no false FAIL from a transient include outage).
- **FR-005**: The system MUST report the budget check as **not applicable** (the dashboard row reads N/A) for any zone that does not have exactly one valid SPF record (no SPF, or the multiple-record / malformed cases spec 009 already FAILs on row A).
- **FR-006**: The budget evaluation MUST NOT block or abort the zone's overall probe cycle, and MUST respect the existing per-zone deadline so a slow chain of includes cannot outlive the cycle.
- **FR-007**: Every new metric MUST ship with Grafana dashboard wiring (constitution Principle IX): a new four-state row in the existing "Email auth — status" panel that reads **FAIL** when the budget is exceeded, **PASS** when within budget, and **N/A** when the check does not apply, carrying detail text that names the lookup count, explains the PermError consequence, and notes the "evaluation incomplete" caveat.
- **FR-008**: The feature MUST ship at least one demo zone whose SPF chains `include:` mechanisms (served within the offline demo namespace) to exceed the 10-lookup limit, and reuse a healthy in-budget zone, with smoke assertions covering both the over-budget FAIL and the in-budget PASS.
- **FR-009**: All examples, demo records, and documentation MUST use RFC 2606 reserved names and MUST NOT reference any real or personal domain.
- **FR-010**: For an in-budget record the lookup-count metric MUST report the exact count (0–10). For an over-budget record the evaluation MUST stop as soon as the running count exceeds 10 and report exactly **11** ("≥11" semantics) — it MUST NOT keep walking to compute an exact over-budget total. This bounds the walk to at most 11 lookups.

### Key Entities *(include if feature involves data)*

- **SPF lookup evaluation (per zone)**: the result of walking a zone's SPF record and its recursively-included records. Attributes: the running count of lookup-incurring mechanisms, whether the count exceeds 10, and whether the walk completed fully (all branches resolved) or hit an unresolvable/timed-out branch.
- **SPF mechanism**: a term in an SPF record. The feature distinguishes the six lookup-incurring kinds (`include`, `a`, `mx`, `ptr`, `exists`, `redirect`) from the rest, and the two that recurse (`include`, `redirect`) from the four that don't.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For any monitored zone with an SPF record, an operator can determine from the dashboard within one probe cycle whether the record exceeds the 10-lookup budget and how many lookups it currently requires — without writing a PromQL query or manually expanding third-party includes.
- **SC-002**: A zone genuinely over the limit reads FAIL, and a zone under the limit reads PASS — and a zone whose only "over limit" appearance is due to an unreachable include this cycle does NOT read FAIL.
- **SC-003**: A zone with no single valid SPF record reads N/A on this row, never a misleading PASS or FAIL.
- **SC-004**: No SPF record — however maliciously or accidentally constructed (deep nesting, cyclic includes, huge fan-out) — can hang, unboundedly slow, or abort a probe cycle.
- **SC-005**: The demo stack demonstrates the over-budget failure live, and the smoke suite fails if the over-budget demo zone regresses to PASS or the healthy zone regresses to FAIL.
- **SC-006**: The feature evaluates SPF lookups against the offline DNS the demo runs on without depending on any single external service being reachable for the demo or tests to pass.

## Assumptions

- **Builds on spec 009**: the SPF parser, the `email_auth` prober, the "Email auth — status" panel, and the reserved `dnshealth_spf_lookup_*` metric names and dashboard row slot already exist. This feature fills them rather than introducing a new prober or panel.
- **Resolution approach** (settled in Clarifications): hand-rolled bounded counter over an iterative-from-root resolver that generalizes the exporter's existing delegation-walk primitives; no new third-party dependency; demo/tests resolve `.demo` targets from the in-stack fake root and stay fully offline.
- **Severity is settled by spec 009**: over-budget → FAIL, no-SPF → N/A. This feature does not re-open the severity model.
- **Macro-bearing targets**: a recursing mechanism whose target embeds a macro (`%{...}`) cannot be resolved without sender context; treated as an unresolvable branch (contributes to "evaluation incomplete") rather than guessed — confirmed in clarification.
- **The RFC 7208 §4.6.4 "void lookup" limit** (at most 2 lookups returning an empty/NXDOMAIN result) is a *separate* cap from the 10-lookup limit and is **out of scope** for this feature (deferred to a follow-up — see Clarifications). This feature enforces only the 10-lookup budget.
