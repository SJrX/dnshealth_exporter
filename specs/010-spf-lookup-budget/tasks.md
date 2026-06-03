# Tasks: SPF DNS-Lookup Budget Check (RFC 7208 §4.6.4)

**Input**: Design documents from `/specs/010-spf-lookup-budget/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: INCLUDED — constitution Principle I (integration test per DNS check) and VIII (readable three-phase tests); the spec has per-story Independent Tests and FR-008 smoke assertions. The pure counter is unit-tested table-driven via an injected fetch (no DNS); the resolver + emitted metrics are integration-tested via `testutil/`; the dashboard gets `promql_live` + drift + detail-guard.

**Organization**: By user story. US1 (the budget verdict) is the MVP and independently shippable; US2 (trust under partial failure — the eval-incomplete guarantee) layers the graceful-degradation semantics on the same counter. This feature **extends** spec 009's `email_auth` prober and "Email auth — status" panel — no new prober or panel.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on incomplete tasks)
- **[Story]**: US1 / US2 (setup, foundational, polish carry no story label)
- All paths repository-relative.

## Path Conventions

Single Go module at repo root. Prober code in `prober/`, demo + dashboard in `demo/`. Matches plan.md.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Expose the parsed SPF mechanisms the counter consumes.

- [ ] T001 In `prober/spf.go`, add a mechanism tokenizer the counter will consume: a `spfMechanism` type (`kind` ∈ include/redirect/a/mx/ptr/exists/all/other, `target` string, `hasMacro` bool per `data-model.md`) and a `parseSPFMechanisms(record string) (mechs []spfMechanism, hasAll bool)` that splits the record into terms, strips qualifiers, classifies each, extracts the `include:`/`redirect=` target, and flags `%{` macros. Leave the existing `analyzeSPF`/qualifier/validity API untouched.
- [ ] T002 [P] Add table-driven unit tests in `prober/spf_test.go` for `parseSPFMechanisms`: classification of all six lookup kinds + ip4/ip6/all/unknown; target extraction for include/redirect; `hasAll` detection; macro flagging (`include:%{ir}._spf.example.net`); qualifier stripping.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The pure counter and the production resolver both US1 and US2 build on.

**⚠️ CRITICAL**: Blocks US1 and US2.

- [ ] T003 Implement the pure bounded counter in `prober/spf_lookup.go` (research R-1/R-2/R-5/R-6): `type fetchSPFFunc func(name string) (record string, ok bool)` and `countSPFLookups(record string, fetch fetchSPFFunc) (count int, complete bool)`. Recurse only into `include`/`redirect` (redirect only when `!hasAll`); `+1` for a/mx/ptr/exists with no fetch; visited-set (lowercased FQDN) + depth cap; **stop the instant count exceeds 10, returning 11**; a macro target or `fetch` returning `ok=false` sets `complete=false` and adds no sub-count; a cycle/depth-cap hit short-circuits to count 11 + `complete=false`. No DNS — `fetch` is injected.
- [ ] T004 [P] Implement the production resolver in `prober/spf_resolve.go` (research R-4): `resolveSPFRecord(ctx, name, client, logger) (record string, ok bool)` — iterative-from-root using `WalkDelegation` to find `name`'s authoritative server, query TXT there, concatenate multi-string RRs (reuse spec-009 logic), return the `v=spf1` record or `ok=false`. Offline-capable (resolves `.demo` from the fake root). **`ctx`-aware throughout** — every query goes through `ExchangeWithRetry(ctx, …)` and the walk checks `ctx.Err()` between hops, so once the per-zone deadline expires the resolver returns `ok=false` promptly (each include then contributes only its own `+1`, `complete=false`) and the overall walk cannot outlive the cycle. This is what T013(b) tests.

**Checkpoint**: The counter and resolver exist and are independently testable.

---

## Phase 3: User Story 1 - See whether a zone's SPF exceeds the 10-lookup budget (Priority: P1) 🎯 MVP

**Goal**: Per zone with a single valid SPF record, count lookups and surface the over/under-budget verdict as a dashboard row.

**Independent Test**: `email-toomanylookups.demo.` (chained includes ≥11) reads FAIL with count 11; `email-healthy.demo.` (`-all`, 0 lookups) reads PASS; a no-SPF zone reads N/A.

### Tests for User Story 1

- [ ] T005 [P] [US1] Table-driven unit tests in `prober/spf_lookup_test.go` for `countSPFLookups` via a map-backed fake `fetch` (no DNS): in-budget exact count; chained includes totalling exactly 11 → count 11; deep over-budget → stops at 11 (assert fetch call count is bounded); `a`/`mx`/`ptr`/`exists` counted without a fetch; `redirect` ignored when `all` present, followed when absent; cyclic include (A→B→A) → count 11, `complete=false`, terminates.
- [ ] T006 [P] [US1] SPF lookup-count integration tests in `prober/email_auth_test.go` (tag `integration`) via `testutil/`: assert `dnshealth_spf_lookup_count` / `_budget_exceeded` / `_eval_complete` for a zone whose apex SPF chains in-fixture `include:` sub-records past 11 (FAIL: count 11, exceeded 1, complete 1) and a zone with `-all` only (PASS: count 0, exceeded 0). Confirm the gauges are **absent** for a no-SPF and a multiple-SPF zone.

### Implementation for User Story 1

- [ ] T007 [US1] In `prober/email_auth.go`, after the existing SPF parse, when `spf.present ∧ spf.recordCount==1 ∧ spf.valid`, parse mechanisms (T001) and run `countSPFLookups` with `resolveSPFRecord` (T003/T004); emit `spf_lookup_count`, `spf_lookup_budget_exceeded`, `spf_lookup_eval_complete` via the existing per-zone ProbeResult pipeline (empty nameserver/ip labels). Emit nothing when there is no single valid SPF record (→ dashboard N/A). Pass the zone's `ctx` so the walk respects the per-zone deadline.

### Dashboard for User Story 1

- [ ] T008 [US1] Insert the "SPF within the 10-lookup budget" row into `emailAuthStatusChecks` in `demo/dashboard/panels_status.go` per `contracts/dashboard-row.md`: rendered in the SPF group (after the qualifier row, before the DMARC rows) with a **fresh unused `refId`** (do NOT renumber the DMARC rows). Predicate: FAIL when `budget_exceeded==1`, PASS when `==0`, N/A via `absent(...)`; no WARN. Detail text covers the §4.6.4 PermError consequence, the "≥11" stop semantics, and the `eval_complete=0` caveat (FAIL only on resolved over-budget). Register the slice in the detail-guard test's two lists if it iterates by slice (it already includes `emailAuthStatusChecks`, so no change needed — verify).
- [ ] T009 [US1] Regenerate the committed dashboard JSON with `make dashboards`; confirm `TestDashboardJSONMatchesGenerator` (drift) and `TestStatusChecksHaveDetail` (detail guard) pass in the default build.

### Demo + smoke for User Story 1

- [ ] T010 [P] [US1] Add the over-budget demo zone (research R-9): `demo/coredns/email-toomanylookups/` (Corefile + zone file) whose apex SPF chains `include:_spfN.email-toomanylookups.demo.` to in-zone TXT sub-records summing to ≥11 lookups; root delegation + glue in `demo/coredns/root/zones/demo.zone`; compose service in `demo/docker-compose.yml` (next free `172.31.0.x`) + `depends_on`; zone entry in `demo/exporter/dnshealth.yml`. RFC 2606 names only (FR-009).
- [ ] T011 [US1] Add the new row's cells to `demo/dashboard/promql_live_test.go`: add `email-toomanylookups.demo.` to `demoZones` and pin the budget row FAIL there, PASS on `email-healthy.demo.`, N/A on `email-none.demo.`/`email-broken.demo.`. Add SPF lookup-budget smoke assertions in `demo/smoke.sh` (over-budget `budget_exceeded=1` + count `11`; in-budget `budget_exceeded=0`).

**Checkpoint**: The budget verdict is end-to-end — counter → resolver → metrics → dashboard row → demo → smoke. MVP shippable.

---

## Phase 4: User Story 2 - Trust the signal under partial failure (Priority: P2)

**Goal**: An unreachable `include` this cycle must not cause a false over-budget FAIL — the eval-incomplete guarantee.

**Independent Test**: A zone whose SPF includes an unreachable target, with the resolvable part under 10 lookups, reads `eval_complete=0`, `budget_exceeded=0`, and the row reads PASS (not FAIL).

### Tests for User Story 2

- [ ] T012 [P] [US2] Add unit cases to `prober/spf_lookup_test.go` (fake fetch): an `include` whose `fetch` returns `ok=false` while resolved mechanisms total 6 → `count=7` (the include's own +1), `complete=false`, NOT over budget; an unreachable include on a record already over 10 → still `exceeded` true (a real failure stands even when incomplete).
- [ ] T013 [P] [US2] Add integration tests in `prober/email_auth_test.go`: (a) a zone whose apex SPF `include:`s a target name that the fixture does not serve (resolution fails), with the reachable mechanisms < 10 → assert `dnshealth_spf_lookup_eval_complete=0`, `dnshealth_spf_lookup_budget_exceeded=0`; and (b) **the deadline facet (FR-006 / SC-004)** — a zone whose apex SPF `include:`s a target served by a `ServerWithOptions{Drop: true}` (or `DropFirstN`) fixture so the include query never answers, run the probe under a short `context.WithTimeout` → assert the probe **returns promptly** (well under a generous test bound, proving the slow include did not outlive the deadline) with `eval_complete=0` and `budget_exceeded=0`. This exercises that `resolveSPFRecord` is `ctx`-aware end-to-end, not just in prose.

### Implementation for User Story 2

- [ ] T014 [US2] Verify (and adjust if needed) that `email_auth.go`'s emission preserves the R-3 guarantee end-to-end: `budget_exceeded` is taken from the counter's resolved-only count and is 0 when the count is ≤10 even if `complete=false`. (Most of the behavior lives in the T003 counter; this task confirms the wiring doesn't override it and the dashboard row's PASS-not-FAIL holds.)

### Demo + verification for User Story 2

- [ ] T015 [US2] Add a partial-failure facet to the demo so the eval-incomplete path is exercised live: either extend `email-toomanylookups` with one `include:` to a deliberately non-existent `.demo` sub-name (kept under the limit on the resolvable side) or add a small `email-spf-unreachable-include.demo.` zone; pin its budget-row cell PASS and assert `eval_complete=0` in `demo/smoke.sh`. Update `demoZones`/pins accordingly.

**Checkpoint**: The check is trustworthy — transient unreachable includes never produce a false FAIL, proven live.

---

## Phase 5: Polish & Cross-Cutting Concerns

- [ ] T016 Verify the `promql_live` universal invariant still holds for **every existing** demo zone now that the new row evaluates for all of them (zones with no SPF read N/A; `email-permissive` single-valid-SPF reads PASS at its small count). Adjust pins only where a value is meaningful; confirm no zone renders blank/out-of-range.
- [ ] T017 [P] Update operator docs: add the "SPF within the 10-lookup budget" row + the three `dnshealth_spf_lookup_*` metrics to `demo/README.md` (the email-auth zone table) and cross-link `quickstart.md`. Note the void-lookup cap remains a future follow-up.
- [ ] T018 Full validation gate: `go test ./...` (unit + drift + detail-guard), `go test -tags=integration ./...`, `make dashboards` (clean drift), and `demo/smoke.sh` end-to-end → all green. Confirm no existing metric series or spec-009 row verdict changed (additive only).

---

## Dependencies & Execution Order

- **Setup (T001–T002)** → **Foundational (T003–T004)** → stories.
- **US1 (T005–T011)** depends on Foundational; ships as MVP.
- **US2 (T012–T015)** depends on Foundational and on US1's emission wiring (T007) and demo/test files; its edits to `spf_lookup_test.go`, `email_auth_test.go`, `email_auth.go`, `smoke.sh`, `promql_live_test.go` are **sequenced after** US1's edits to those same files (shared-file contention).
- **Polish (T016–T018)** after both stories; T018 is the final gate.

### Within-story parallelism
- US1: T005 (counter unit tests) ∥ T006 (integration tests) ∥ T010 (demo zone files) — different files. Then T003-dependent T007 → T008 → T009 sequential; T011 after T008/T010.
- US2: T012 ∥ T013 (different test files), then T014, T015.

### Cross-file contention (NOT parallel across tasks)
`prober/spf.go`, `prober/spf_lookup_test.go`, `prober/email_auth.go`, `prober/email_auth_test.go`, `demo/dashboard/panels_status.go`, `demo/dashboard/promql_live_test.go`, `demo/smoke.sh`, `demo/coredns/root/zones/demo.zone`, `demo/docker-compose.yml`, `demo/exporter/dnshealth.yml` — serialize edits within each.

## Implementation Strategy

- **MVP = US1** (the budget verdict + row). Independently valuable and shippable.
- **Incremental**: land US1 → smoke-green → add US2's graceful-degradation tests/demo → re-verify. The R-3 guarantee is mostly already in the T003 counter, so US2 is largely *proving* it (tests + a demo facet) rather than new logic.
- **Deferred** (tracked, not here): the §4.6.4 void-lookup cap (clarification Q2) → future issue.

## Parallel Example (US1)

```
# After Foundational (T003–T004):
T005  prober/spf_lookup_test.go     (counter unit tests, fake fetch, no DNS)
T006  prober/email_auth_test.go     (lookup-metric integration tests)
T010  demo/coredns/email-toomanylookups/ ...  (demo zone files)
# Then sequential: T007 → T008 → T009 → T011
```
