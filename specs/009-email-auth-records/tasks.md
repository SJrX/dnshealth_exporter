# Tasks: Email-Authentication DNS Records (Tier 1: SPF + DMARC)

**Input**: Design documents from `/specs/009-email-auth-records/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: INCLUDED — the constitution mandates integration tests for every DNS check (Principle I) and readable three-phase tests (Principle VIII); the spec provides per-story Independent Tests and FR-015 smoke assertions. Pure parsers get table-driven unit tests; the prober gets `testutil/` integration tests; the dashboard gets `promql_live` + drift + detail-guard coverage.

**Organization**: Tasks grouped by user story. US1 (SPF) is the MVP and is independently shippable; US2 (DMARC) layers on the same prober + panel. US3 (SPF lookup budget) is **deferred to [#58](https://github.com/SJrX/dnshealth_exporter/issues/58)** — no tasks here.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1 / US2 (setup, foundational, polish carry no story label)
- All paths are repository-relative.

## Path Conventions

Single Go module at repo root. Prober code in `prober/`, cycle wiring in `cycle/`, demo + dashboard in `demo/`. Matches plan.md.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Scaffolding both stories build on.

- [ ] T001 Create `prober/email_auth.go` with the prober skeleton: `func init(){ RegisterProber("email_auth", ProbeEmailAuth) }` and `ProbeEmailAuth(ctx, zone, nameservers, delegation, client, logger) ([]ProbeResult, error)` returning empty for now, matching the registered-prober signature used by `prober/mx.go` / `prober/soa.go`.
- [ ] T002 [P] Define the parse-result types `SPFRecord`, `DMARCRecord`, `EmailAuthResult` (fields per `data-model.md`) in `prober/email_auth.go`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared DNS + metric plumbing both SPF and DMARC need. No story work begins until this is done.

**⚠️ CRITICAL**: Blocks US1 and US2.

- [ ] T003 Implement a TXT-fetch helper in `prober/email_auth.go` that queries TypeTXT at a given name against the zone's authoritative `nameservers` via `ExchangeWithRetry`, concatenates each RR's character-strings (R-2), and returns the per-RR records — used by both SPF (apex) and DMARC (`_dmarc.<zone>`). Selection-by-version-prefix is left to each parser.
- [ ] T004 Implement the metric-emission plumbing in `prober/email_auth.go`: convert an `EmailAuthResult` into `ProbeResult`s carrying the `dnshealth_spf_*` / `dnshealth_dmarc_*` gauges per `contracts/email-auth-metrics.md`, with **zero-emission** for the boolean per-zone gauges (Reset+Set(0) pattern from spec 007/008) so a zone with no SPF/DMARC reads `0`, not a missing series. Info gauges (`spf_terminal_all`, `dmarc_policy`) emit only the applicable enum value.
- [ ] T005 [P] Add an integration-test scaffold `prober/email_auth_test.go` (build tag: `integration`) wiring a `testutil/` fixture that serves TXT records at a zone apex and `_dmarc.<zone>`, so US1/US2 tests can add cases (helpers only; no assertions yet).

**Checkpoint**: Prober runs for every configured zone and emits zero-valued email-auth gauges; ready for SPF/DMARC logic.

---

## Phase 3: User Story 1 - See SPF presence and safety per zone (Priority: P1) 🎯 MVP

**Goal**: Per zone, surface whether a single valid SPF record is published and whether its terminal `all` qualifier is safe, rendered as two four-state dashboard rows.

**Independent Test**: Bring up the demo; `email-healthy.demo.` reads PASS on both SPF rows, `email-none.demo.` reads WARN (absent) / N/A (qualifier), `email-permissive.demo.` reads WARN on the qualifier row (`+all`), `email-broken.demo.` reads FAIL on row A (two `v=spf1` records).

### Tests for User Story 1

- [ ] T006 [P] [US1] Table-driven unit tests in `prober/spf_test.go` for the pure SPF parser: zero/one/multiple `v=spf1` records; each terminal qualifier (`-all`/`~all`/`?all`/`+all`/none); multi-string concatenation; unrelated TXT ignored; case-insensitive `v=spf1`. **Encode the exact R-9 malformed boundary**: malformed cases (`v=spf1` with empty body, untokenizable term) ⇒ `valid=false`; and a positive case proving an *unknown-but-harmless* term (e.g. `v=spf1 ip4:192.0.2.0/24 unknownmech -all`) is **tolerated** ⇒ `valid=true` with qualifier `fail` — so the parser does not false-FAIL valid-but-exotic records. No DNS. Write first; they fail until T008.
- [ ] T007 [P] [US1] SPF integration tests in `prober/email_auth_test.go` (tag `integration`) via `testutil/`: assert `dnshealth_spf_present` / `_record_count` / `_valid` / `_terminal_all{qualifier}` gauges for healthy-`-all`, `+all`, no-SPF, and two-record zones (three-phase Meszaros, defaults-with-override fixtures per Principle VIII).

### Implementation for User Story 1

- [ ] T008 [US1] Implement the pure SPF parser `prober/spf.go` (R-3 / R-9): select `v=spf1` record(s), count them, find the last `[-~?+]?all` term and map its qualifier, classify malformed (empty-after-version / untokenizable). No DNS, no recursion, no dependency.
- [ ] T009 [US1] In `prober/email_auth.go`, query the apex TXT (T003), build `SPFRecord` via the T008 parser, and emit the SPF gauges via the T004 plumbing (zero-emitted for every zone).

### Dashboard for User Story 1

- [ ] T010 [US1] Create `emailAuthStatusChecks` with SPF rows **A** ("Zone publishes a single valid SPF record") and **B** ("SPF ends in a restrictive `all` qualifier") in `demo/dashboard/panels_status.go`, plus `emailAuthStatusTable(yOffset)` **modeled on `mxStatusTable` — its own collapsible section below the MX row, same `gridPos`/`subY(yOffset)` pattern and width**; wire it into `buildOverview` in `demo/dashboard/dashboard.go` following how the MX section is added. Each row carries detail text including the FR-017 anti-spoofing rationale. Predicates per `contracts/dashboard-panel.md` (severity model from spec Clarifications).
- [ ] T011 [US1] Regenerate the committed dashboard JSON with `make dashboards`; confirm `TestDashboardJSONMatchesGenerator` (drift) and `TestStatusChecksHaveDetail` (detail guard) pass in the default build.

### Demo + smoke for User Story 1

- [ ] T012 [P] [US1] Add the SPF demo zones (Corefile + zone file under `demo/coredns/email-*/`, root delegation + glue in `demo/coredns/root/zones/demo.zone`, compose service in `demo/docker-compose.yml`, zone entry in `demo/exporter/dnshealth.yml`): `email-healthy` (`v=spf1 -all`), `email-spf-only` (`-all`, no DMARC), `email-none` (no records), `email-permissive` (`v=spf1 +all`), `email-broken` (two `v=spf1` RRs). One container per zone at the next free `172.31.0.x` addresses after the existing block — no NS-glue IP games here (unlike the `ns-ip-mismatch` zone, #37), so any free IP works. RFC 2606 names only (FR-016).
- [ ] T013 [US1] Wire the new panel into the live gate and demo: in `demo/dashboard/promql_live_test.go` **register `emailAuthStatusChecks` in `panelChecks()`** (without this the new rows are never evaluated by the `promql_live` test — its pins would silently match nothing), add the new zones to `demoZones`, and pin the SPF row cells (A/B) per `contracts/dashboard-panel.md`'s validation table. Add SPF smoke assertions (happy + a broken case) in `demo/smoke.sh`.

**Checkpoint**: SPF is end-to-end — prober → metrics → two dashboard rows → demo → smoke. MVP shippable on its own.

---

## Phase 4: User Story 2 - See DMARC presence and enforcement policy per zone (Priority: P2)

**Goal**: Per zone, surface whether a valid DMARC record is published and which enforcement policy it declares, as two more four-state rows in the same panel.

**Independent Test**: `email-healthy.demo.` reads PASS on both DMARC rows; `email-spf-only.demo.` reads WARN (absent) / N/A (policy); `email-permissive.demo.` reads WARN on the policy row (`p=none`); `email-broken.demo.` reads FAIL on the DMARC-valid row (missing `p=`).

### Tests for User Story 2

- [ ] T014 [P] [US2] Table-driven unit tests in `prober/dmarc_test.go` for the pure DMARC parser: presence; `p=none|quarantine|reject`; malformed (begins `v=DMARC1`, no `p=`); `sp=`/`rua=`/`ruf=` presence; case-insensitive tags; NXDOMAIN and NODATA both ⇒ absent. No DNS.
- [ ] T015 [P] [US2] DMARC integration tests in `prober/email_auth_test.go` via `testutil/`: assert `dnshealth_dmarc_present` / `_valid` / `_policy{policy}` (and `sp`/`rua`/`ruf`) for `p=reject`, `p=none`, absent, and malformed `_dmarc` zones.

### Implementation for User Story 2

- [ ] T016 [US2] Implement the pure DMARC parser `prober/dmarc.go` (R-5): split `tag=value;`, extract `p=` policy + validity, optional `sp=`/`rua`/`ruf` presence.
- [ ] T017 [US2] In `prober/email_auth.go`, query `_dmarc.<zone>` TXT (T003), build `DMARCRecord` via the T016 parser, and emit the DMARC gauges via the T004 plumbing (zero-emitted; `dmarc_policy` info gauge only when valid).

### Dashboard for User Story 2

- [ ] T018 [US2] Append DMARC rows **C** ("Zone publishes a valid DMARC record") and **D** ("DMARC enforces a policy") to `emailAuthStatusChecks` in `demo/dashboard/panels_status.go` with detail text; regenerate the JSON (`make dashboards`); drift + detail-guard pass.

### Demo + smoke for User Story 2

- [ ] T019 [P] [US2] Add DMARC records to the existing demo zone files: `email-healthy` (`v=DMARC1; p=reject; rua=mailto:dmarc@example.com`), `email-permissive` (`p=none`), `email-broken` (`v=DMARC1` with no `p=`). `email-spf-only` / `email-none` keep no `_dmarc` record.
- [ ] T020 [US2] Add DMARC smoke assertions (happy + malformed) in `demo/smoke.sh`; pin the DMARC row cells (C/D) for each zone in `demo/dashboard/promql_live_test.go`.

**Checkpoint**: Full Tier-1 email-auth panel (4 rows) live across the demo.

---

## Phase 5: Polish & Cross-Cutting Concerns

- [ ] T021 [P] Add the `email-nomail` demo zone (Null MX `0 .` + `v=spf1 -all` + `v=DMARC1; p=reject`): Corefile, zone file, root delegation, compose service, `dnshealth.yml`; pin all four email-auth rows = PASS in `promql_live_test.go`, proving FR-017 (email-auth applies and passes independent of MX state).
- [ ] T022 Verify the `promql_live` universal invariant for **every existing** demo zone (healthy/ns-*/mx-*/soa-* etc.): the new email-auth rows must render a valid 0/1/2/3 (typically WARN/N/A, since those zones publish no SPF/DMARC). Add or adjust pins only where a value is meaningful; confirm no zone renders blank or out-of-range.
- [ ] T023 [P] Update operator docs: add the "Email auth — status" panel + the new metrics to `demo/README.md` (and cross-link the spec's `quickstart.md` PromQL recipes). Note the deferred lookup-budget check (#58).
- [ ] T024 Full validation gate: `go test ./...` (unit + drift + detail-guard), `go test -tags=integration ./...`, `make dashboards` (clean drift), and `demo/smoke.sh` end-to-end → all green. Confirm no existing metric series changed (additive-only, Principle / FR-011).

---

## Dependencies & Execution Order

- **Setup (T001–T002)** → **Foundational (T003–T005)** → stories.
- **US1 (T006–T013)** depends only on Foundational. Ships as MVP.
- **US2 (T014–T020)** depends on Foundational; independent of US1 except that both edit `prober/email_auth.go` (T009/T017), `panels_status.go` (T010/T018), `smoke.sh` (T013/T020), and `promql_live_test.go` (T013/T020) — so US2's edits to those shared files are sequenced after US1's, not parallel with them.
- **Polish (T021–T024)** after both stories; T024 is the final gate.

### Within-story parallelism

- US1: T006 and T007 (different test files) run in parallel; T012 (demo files) parallel with the test tasks. T008 → T009 → T010 → T011 sequential (parser → emit → dashboard → regen). T013 after T010/T012.
- US2: T014 and T015 parallel; T019 parallel with tests. T016 → T017 → T018 sequential. T020 after T018/T019.

### Cross-file contention (NOT parallel)

`prober/email_auth.go`, `demo/dashboard/panels_status.go`, `demo/smoke.sh`, `demo/dashboard/promql_live_test.go`, `demo/exporter/dnshealth.yml`, `demo/docker-compose.yml`, and `demo/coredns/root/zones/demo.zone` are each touched by multiple tasks — serialize edits to them.

## Implementation Strategy

- **MVP = US1 only** (SPF presence + qualifier, two dashboard rows). Independently valuable and shippable; DMARC can follow in a later PR if desired.
- **Incremental**: land US1 → verify via smoke → add US2 → re-verify. Each checkpoint is a green demo.
- **Deferred**: the SPF DNS-lookup-budget row + its `dnshealth_spf_lookup_*` metrics + the over-budget demo chain are tracked in **#58**, not this cycle.

## Parallel Example (US1)

```
# After Foundational (T003–T005) completes, launch in parallel:
T006  prober/spf_test.go        (unit tests, no DNS)
T007  prober/email_auth_test.go (SPF integration tests)
T012  demo/coredns/email-*/ ...  (demo zone files)
# Then sequential: T008 → T009 → T010 → T011 → T013
```
