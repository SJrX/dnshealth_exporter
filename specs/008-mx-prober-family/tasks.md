---

description: "Tasks for spec 008 — MX prober family"
---

# Tasks: MX Prober Family

**Input**: Design documents from `/specs/008-mx-prober-family/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md — all present

**Tests**: Test tasks are included throughout. Per Constitution Principle I (Robust Integration Testing) and Principle VIII (Readable, Honest Tests), no FR may be considered complete without integration test coverage.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3, US4)
- All file paths are repository-relative

## Path Conventions

- Source: `prober/`, `cycle/`, `demo/dashboard/`
- Tests: `prober/*_test.go` (built with `-tags=integration`)
- Demo infrastructure: `demo/coredns/<name>/`, `demo/docker-compose.yml`, `demo/exporter/dnshealth.yml`, `demo/smoke.sh`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: File scaffolding for the new prober.

- [X] T001 Create empty file scaffold `prober/mx.go` with package declaration, `init()` registering the prober via `RegisterProber("mx", ProbeMX)`, and a stub `ProbeMX` returning `(nil, nil)` so the package compiles before any logic is added.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Per-cycle gauge infrastructure for the per-zone count + Null-MX + is-primary gauges. Must be in place before US1's implementation can wire data into the dashboard predicates.

**⚠️ CRITICAL**: No user story work touching the runner-owned gauges can begin until this phase is complete.

- [X] T002 Extend `cycle/runner.go` to register four new GaugeVecs on `Runner` — `MXCount`, `MXResolvedCount`, `MXCNAMECount` (all `{zone}`), `NullMX` (`{zone}`), and `MXIsPrimary` (`{zone, target}`). Register all in `NewRunner` and call `.Reset()` on each at the start of `Run()`. Follows the established Reset+Set(0) pattern from spec 007 (R-2 / FR-008).
- [X] T003 Add aggregation pass to the end of `cycle/runner.go` `Run()` (after `wg.Wait()`, before returning `CycleResult`). Iterate `allResults` for `Check == "mx"` results: (a) initialize all four per-zone count/boolean gauges to 0 for every `cfg.Zones` entry; (b) count MX records, resolved targets, CNAMEd targets per zone; (c) Set NullMX from the prober's emitted boolean; (d) derive per-zone min(priority) and Set MXIsPrimary=1 for every target tied at that minimum, 0 otherwise.

**Checkpoint**: Runner-owned gauges in place. US1 implementation can now plug into the pipeline.

---

## Phase 3: User Story 1 - Per-MX visibility + RFC-validity flags (Priority: P1) 🎯 MVP

**Goal**: Operators can see every MX record per zone with priority, target hostname, and per-target validity flags (resolves yes/no, not-CNAME yes/no, LDH-valid yes/no). Dashboard rows summarize zone-level MX health. Demo zones exercise both happy and broken cases.

**Independent Test**: After bring-up, querying `/metrics` for `mx-healthy.demo.` returns per-MX series for two healthy MXes; querying `mx-broken.demo.` returns one CNAMEd target with `is_cname=1` and one unresolvable target with `resolves=0`. Dashboard MX-status panel reads PASS for healthy, FAIL for broken.

### Implementation: MX prober body

- [X] T004 [US1] Implement `ProbeMX` body in `prober/mx.go`. (a) Issue one TypeMX query per zone against the first reachable parent-listed nameserver, using `ExchangeWithRetry` + `ResolveAddress`. (b) For each MX RR in the Answer: emit a ProbeResult with `Check: "mx"`, `Nameserver: target`, `Metrics: {"mx_info": 1, "mx_resolves": 0|1, "mx_is_cname": 0|1, "mx_syntax_valid": 0|1}`, `Labels: {"target": canonName(target), "priority": fmt.Sprint(rr.Preference)}`. (c) Use per-cycle per-target caches for `ResolveHostnames` (resolves check), `lookupCNAME` (cname check), and `isValidNSHostname` (syntax — reuse from `prober/ns_hostname.go`). (d) Handle the Null MX special case in T021 (US2) — for now treat `target == "."` as syntactically valid but skip the resolution / CNAME checks for it.

### Integration tests: MX prober behavior

- [X] T005 [US1] Create `prober/mx_test.go` (built with `//go:build integration`) and add `TestMX_HappyPath` — zone with two MX records at priorities 10 and 20, both targets resolving and neither a CNAME. Assert per-MX info gauges with correct priority labels, `mx_resolves=1` for both, `mx_is_cname=0` for both.
- [X] T006 [US1] Append `TestMX_UnresolvableTarget` to `prober/mx_test.go` — MX target whose hostname has no A or AAAA record anywhere. Assert `mx_resolves=0` for that target while other healthy MXes in the same zone still read 1.
- [X] T007 [US1] Append `TestMX_CNAMEdTarget` to `prober/mx_test.go` — MX target that IS a CNAME (target hostname has a CNAME RR at its name). Assert `mx_is_cname=1` for that target.
- [X] T008 [US1] Append `TestMX_InvalidSyntaxTarget` to `prober/mx_test.go` — MX target hostname with an underscore (LDH violation). Assert `mx_syntax_valid=0` for that target. (Also exercises US4 P4 — Phase 6 is the explicit US4 verification.)

### Cycle-runner integration test

- [X] T009 [US1] Add `TestCycleRunner_MXCountResetsAndZeroes` to `cycle/runner_test.go` — verify that across consecutive cycles, a zone with zero MX records consistently emits `dnshealth_mx_count{zone=X} 0` (explicit zero, not series absent), proving the Reset+Set(0) pattern works for MX gauges. Mirrors `TestCycleRunner_NSClassificationCountResetsAndZeroes` from spec 007.

### Dashboard wiring

- [X] T010 [US1] Add new `mxStatusChecks` slice and `mxStatusTable(yOffset)` builder to `demo/dashboard/panels_status.go` with four initial rows (A: MX records present OR Null MX, B: all resolve, C: no CNAMEs, D: syntax valid). Each row carries detail text per `contracts/dashboard-panel.md`. **Important per speckit-analyze finding C1**: row B's PromQL MUST include the Null-MX suppression branch (`OR on(zone) (dnshealth_mx_null_mx == bool 1)`) — without it, Null-MX zones spuriously FAIL because the `.` target has no `mx_resolves` emission. Row E ("no Null MX conflict") is added in T023 (US2). Verify the new slice is iterated by `TestStatusChecksHaveDetail`.
- [X] T011 [US1] Add a new `mxRecordsTable(yOffset)` builder to `demo/dashboard/panels_records.go` following the `selfNSRecordsTable` pattern. Join 5 queries by `target`: `mx_info` (provides priority label), `mx_resolves`, `mx_is_cname`, `mx_syntax_valid`, `mx_is_primary`. Columns: Target / Priority / Resolves / Is CNAME / Syntax valid / Role (primary/backup). Width overrides per contract.
- [X] T012 [US1] Wire `mxStatusTable` and `mxRecordsTable` into `buildOverview` in `demo/dashboard/dashboard.go` at the GridPos coordinates from `contracts/dashboard-panel.md`. Adjust yOffset for any subsequent panels that need to shift down.
- [X] T013 [US1] Run `make dashboards` to regenerate `demo/grafana/dashboards/dnshealth-overview.json` and `dnshealth-overview-demo.json`. Verify `go test -tags=integration ./demo/dashboard/...` passes (drift test).

### Demo zone: mx-healthy.demo.

- [X] T014 [P] [US1] Create `demo/coredns/mx-healthy/Corefile` and `demo/coredns/mx-healthy/zones/mx-healthy.demo.zone`. Zone publishes two MX records at priorities 10 and 20, both targets in-zone (`mail-a.mx-healthy.demo.` and `mail-b.mx-healthy.demo.`), with A records for each. Plus apex SOA, NS, A.
- [X] T015 [P] [US1] Add `coredns-mx-healthy` service to `demo/docker-compose.yml` at `ipv4_address: 172.31.0.22` with appropriate aliases. Add to exporter's `depends_on` list.

### Demo zone: mx-broken.demo.

- [X] T016 [P] [US1] Create `demo/coredns/mx-broken/Corefile` and `demo/coredns/mx-broken/zones/mx-broken.demo.zone`. Zone publishes two MX records: `10 cname-mail.mx-broken.demo.` (target IS a CNAME pointing to `real-mail.mx-broken.demo.` which has an A record) and `20 missing-mail.mx-broken.demo.` (target has NO records anywhere). Plus apex SOA, NS, A, and the CNAME chain bits.
- [X] T017 [P] [US1] Add `coredns-mx-broken` service to `demo/docker-compose.yml` at `ipv4_address: 172.31.0.24` (172.31.0.23 is reserved for mx-null in US2). Add to exporter's `depends_on` list.

### Root zone delegations + exporter config

- [X] T018 [US1] Add delegations to `demo/coredns/root/zones/demo.zone` for `mx-healthy.demo.` (glued at 172.31.0.22) and `mx-broken.demo.` (glued at 172.31.0.24). Use in-zone NS hostnames following the established pattern.
- [X] T019 [US1] Add `mx-healthy.demo.` and `mx-broken.demo.` to `demo/exporter/dnshealth.yml` zones list.

### Smoke verification

- [X] T020 [US1] Add smoke assertions to `demo/smoke.sh`: A4g for `mx-healthy.demo.` (two MX info gauges with priorities 10 + 20, both resolves=1, both is_cname=0, mx_count=2, resolved_count=2, cname_count=0, null_mx=0, is_primary=1 for priority 10 only) and A4h for `mx-broken.demo.` (CNAMEd target reads is_cname=1, unresolvable target reads resolves=0, cname_count=1).
- [X] T021 [US1] Run `cd demo && docker compose down -v && ./smoke.sh` and verify all assertions including A4g + A4h pass cleanly on a cold-cached run.

**Checkpoint**: US1 complete. MVP shippable. Healthy + broken demo zones surface per-MX validity end-to-end through metrics, dashboard panel, and per-MX records table.

---

## Phase 4: User Story 2 - Null MX detection (Priority: P2)

**Goal**: Zones explicitly opting out of email via Null MX (`0 .`) are recognized and don't generate alert noise on the MX-presence check. The "Null MX coexists with real MX" configuration error is surfaced as a FAIL row.

**Independent Test**: `mx-null.demo.` surfaces `dnshealth_mx_null_mx=1`; MX-presence row reads PASS for that zone. An integration test with a zone publishing Null MX + a real MX surfaces the conflict via the new row E.

### Implementation

- [X] T022 [US2] Extend `ProbeMX` body in `prober/mx.go` to detect Null MX at parse time: if the TypeMX response contains exactly one MX RR with `Preference == 0` AND `Mx == "."`, emit a ProbeResult with `Metrics: {"mx_null_mx": 1}` for the zone (in addition to the per-MX info gauge for the Null MX record itself, which still gets emitted with `target="."`, `priority="0"`). For multi-MX zones (including the Null-MX-coexists-with-MX malformed case), emit `mx_null_mx: 1` only if the canonical single-`0 .` form is present.

### Dashboard row E (depends on T010's `mxStatusChecks` slice)

- [X] T023 [US2] Append row E to `mxStatusChecks` in `demo/dashboard/panels_status.go` per the contract: "No conflict between Null MX and real MX records" with PromQL `((dnshealth_mx_null_mx{zone="$zone"} == bool 0) or on(zone) (dnshealth_mx_count{zone="$zone"} == bool 1)) or on() vector(0)`. Detail text explains RFC 7505 + the malformed-config case. Run `make dashboards` to regenerate JSON; drift test must pass.

### Integration tests

- [X] T024 [US2] Append `TestMX_NullMX` to `prober/mx_test.go` — zone publishing exactly one MX with `0 .`. Assert `mx_null_mx = 1` for the zone, info gauge present with `target="."` `priority="0"`, no `mx_resolves` / `mx_is_cname` series for the `.` target (skipped per T004's special-case handling).
- [X] T025 [US2] Append `TestMX_NullMXConflict` to `prober/mx_test.go` — zone publishing `0 .` AND a real MX record like `10 real-mail.example.test.`. Assert: `mx_null_mx = 0` (since canonical form requires SINGLE Null MX RR), `mx_count = 2`. Operator-facing: the dashboard row E catches this via the count > 1 predicate even though `mx_null_mx` itself reads 0; verify in T029 audit.

### Demo zone: mx-null.demo.

- [X] T026 [P] [US2] Create `demo/coredns/mx-null/Corefile` and `demo/coredns/mx-null/zones/mx-null.demo.zone` publishing exactly one MX record `0 .` (Null MX per RFC 7505). Plus apex SOA, NS, A.
- [X] T027 [P] [US2] Add `coredns-mx-null` service to `demo/docker-compose.yml` at `ipv4_address: 172.31.0.23`. Add to exporter's `depends_on` list. Add delegation to `demo/coredns/root/zones/demo.zone` and entry to `demo/exporter/dnshealth.yml`.

### Smoke verification

- [X] T028 [US2] Add A4i smoke assertion to `demo/smoke.sh` verifying `mx-null.demo.` reads `dnshealth_mx_null_mx{zone="mx-null.demo."} 1`, `mx_count = 1`, info gauge present with `target="."`. Also assert the MX-presence row's predicate evaluates true via direct PromQL check: count > 0 OR null_mx == 1 should yield 1 for this zone.

**Checkpoint**: US2 complete. Null MX recognized; conflict row available.

---

## Phase 5: User Story 3 - Primary/backup classification (Priority: P3)

**Goal**: Operators can identify primary and backup MXes by priority; the per-MX dashboard table orders rows by priority and tags primary vs backup. Multi-MX-tied-at-minimum cases (legitimate load balancing) all get `is_primary=1`.

**Independent Test**: `mx-healthy.demo.` shows priority-10 MX as `is_primary=1` and priority-20 MX as `is_primary=0`. An integration test with two MXes tied at preference 10 shows both as `is_primary=1`.

- [X] T029 [US3] Append `TestMX_TiedPrimaries` to `prober/mx_test.go` — zone with two MX records at the same minimum preference (e.g. both at preference 10). Use a real `cycle.Runner.Run()` invocation (not just `env.Probe(...)`) since `is_primary` is set by the runner's aggregation pass, not by the prober itself. Assert both targets read `is_primary=1`.

**Checkpoint**: US3 complete. (Implementation already in T003's aggregation pass; this story just adds verification.)

---

## Phase 6: User Story 4 - LDH syntax validity (Priority: P4)

**Goal**: Per-MX syntactic-validity gauge fires correctly for invalid hostnames.

**Independent Test**: Already exercised by T008 in US1. This phase is a verification + documentation gate.

- [X] T030 [US4] Verify T008's test exercises the LDH-invalid case end-to-end (underscore in MX target). If T008 didn't fully cover, append a fixture variant here. No new demo zone needed — the syntax case is purely integration-test territory since adding malformed hostnames to CoreDNS is finicky (matches the rationale from spec N6 / PR #31).

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T031 [P] Run `go vet ./...` from repo root — must pass with no warnings.
- [X] T032 [P] Run `go test -tags=integration -count=1 ./...` — full integration test suite must pass with no regressions.
- [X] T033 Run `cd demo && docker compose down -v && ./smoke.sh` end-to-end twice back-to-back (cold cache + warm) to verify the new A4g/A4h/A4i assertions are stable and #40's readiness loop still works with the added zones.
- [X] T034 Per Constitution Governance section, perform an adversarial code-vs-spec audit before declaring complete: re-read each FR-### in `spec.md` and confirm the implementation actually delivers it. Verify in particular: the Null-MX-conflict case (T025 spec scenario) is correctly distinguished from canonical Null MX; the `mx_is_primary` gauge correctly handles tied-primary cases; the dashboard row E's PromQL predicate matches the contract. File any gaps as separate issues. Save audit notes to `specs/008-mx-prober-family/audit.md`.

### Spot-check follow-ups (post-audit)

User-driven dashboard spot-check after T034 surfaced two bugs that audit.md D-6 and D-7 cover in full. Tasks here mirror the work so the per-task ledger stays honest.

- [X] T035 Fix Is-CNAME column rendering "RA=1" (audit D-6). Add `cnameYesNoMappings()` to `demo/dashboard/helpers.go` with neutral `"yes"`/`"no"` text + inverse polarity (1=red, 0=green). Switch the Is-CNAME column in `mxRecordsTable` (`demo/dashboard/panels_records.go`) from `recursionYesNoMappings` to the new helper.
- [X] T036 Fix MX prober over-emitting labels (audit D-7). In `prober/mx.go`, split each MX RR into two `ProbeResult`s: an info-result carrying `mx_info` with `{target, priority}`, and a validity-result carrying `mx_resolves`/`mx_is_cname`/`mx_syntax_valid` with `{target}` only; both with `Nameserver=""` and `IP=""`. Per-target dedup via `emittedValidity` so duplicate-target zones produce one validity-result per unique target. Update `cycle/runner.go` MX aggregation to read target from `res.Labels["target"]` (one-line change). Add `nameserver N` to `panels_records.go` exclude list. Add `TestMX_LabelContract` to `prober/mx_test.go` asserting exact label keysets per metric.

---

## Dependencies

```text
T001 (scaffold)
  └─→ T004 (prober body)
       ├─→ T005, T006, T007, T008 (US1 tests; sequential — same file)
       ├─→ T014, T015, T016, T017 (demo zones; can parallelize within US1)
       └─→ T022 (Null MX detection extends the prober body)

T002 (runner gauge fields)
  └─→ T003 (aggregation pass)
       └─→ T009 (cycle runner test)
       └─→ T010-T013 (dashboard wiring uses the runner-owned gauges)

T010 → T011 → T012 → T013 (dashboard sequence)
T014 ∥ T015 ∥ T016 ∥ T017 (demo zone files / compose entries can run in parallel; different files)
T014..T017 → T018 → T019 → T020 → T021 (demo wiring → smoke run depends on all demo edits)

US1 complete (T021 passes)
  └─→ US2:
       T022 (Null MX detection) ─┬─→ T024, T025 (US2 tests)
                                  └─→ T023 (dashboard row E)
       T026 ∥ T027 (Null MX demo zone)
       T026/T027 → T028 (smoke)

US2 complete
  └─→ US3: T029 (tied-primaries test)
       └─→ US4: T030 (syntax verification)
            └─→ Phase 7 polish (T031, T032, T033, T034)
```

## Parallel execution opportunities

- **Within US1**: T014/T015/T016/T017 edit different files (per-container Corefile/zone-file vs the compose service entries) and can run concurrently. T005-T008 edit the same test file and must be sequential.
- **Within US2**: T026 (Null-MX zone files) and T027 (compose+config edits) edit different files; can be parallel. T024/T025 (tests) are sequential.
- **Within Phase 7**: T031 (vet) and T032 (full tests) are independent and can run in parallel.

## Implementation strategy

**MVP scope (US1 only, T001-T021)**: Delivers per-MX visibility + RFC-validity flags + healthy and broken demo zones + dashboard panel and table + smoke. Functional and shippable for the most operationally valuable case. Stop after T021 if you want to bundle US2/US3/US4 into a follow-up PR.

**Recommended scope**: Ship US1 + US2 + US3 + US4 + Phase 7 polish as one PR. US2 prevents alert noise; US3 makes the dashboard immediately usable for failover triage; US4 is essentially zero marginal cost (T008 in US1 already exercises it). Total: 34 tasks, estimated 400-500 LoC of source + 200-300 LoC of tests + ~80 LoC of demo / compose / smoke edits + regenerated dashboard JSON.

**Per constitutional guidance**: T034's audit gate must complete before the PR is opened. Any gaps surfaced become follow-up issues, not silent fixes.
