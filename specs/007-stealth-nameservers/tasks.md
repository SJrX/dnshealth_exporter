---

description: "Tasks for spec 007 — stealth nameserver detection"
---

# Tasks: Stealth Nameserver Detection

**Input**: Design documents from `/specs/007-stealth-nameservers/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/, quickstart.md — all present

**Tests**: Test tasks are included throughout. Per Constitution Principle I (Robust Integration Testing) and Principle VIII (Readable, Honest Tests), no FR may be considered complete without integration test coverage.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3) — present only on user-story-phase tasks
- All file paths are repository-relative absolute (rooted at the repo)

## Path Conventions

- Source: `prober/`, `cycle/`, `demo/dashboard/`
- Tests: `prober/*_test.go` (built with `-tags=integration`)
- Demo infrastructure: `demo/coredns/<name>/`, `demo/docker-compose.yml`, `demo/exporter/dnshealth.yml`, `demo/smoke.sh`
- Specs: `specs/007-stealth-nameservers/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: File scaffolding for the new prober.

- [X] T001 Create empty file scaffold `prober/ns_classification.go` with package declaration, `init()` registering the prober via `RegisterProber("ns_classification", ProbeNSClassification)`, and a stub `ProbeNSClassification` returning `(nil, nil)` so the package compiles before any logic is added.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Per-cycle gauge infrastructure for the per-zone count metric. Must be in place before US1's implementation can wire counts into the dashboard predicate.

**⚠️ CRITICAL**: No user story work touching `dnshealth_ns_classification_count` can begin until this phase is complete.

- [X] T002 Extend `cycle/runner.go` to register a new `NSClassificationCount *prometheus.GaugeVec` field on `Runner` with labels `["zone", "classification"]`. Wire registration in `NewRunner` alongside `ParentDelegation`, and call `.Reset()` at the start of `Run()` so the per-cycle no-data-vs-no-divergence distinction (per data-model.md R-2 / FR-008) works.
- [X] T003 Extend `cycle/runner.go` to add an aggregation pass at the end of `Run()` (after `wg.Wait()`, before returning `CycleResult`) that iterates `allResults`, groups any result with `Check == "ns_classification"` by `(zone, classification)`, and emits `Set(count)` on the new gauge — including `Set(0)` for each (zone, classification) tuple that received no entries (per data-model.md "all three classifications MUST receive an explicit Set per cycle"). Iterate over `cfg.Zones` to know which zones to emit zeros for.

**Checkpoint**: Count-gauge infrastructure ready. US1 implementation can now wire the classifier into this pipeline.

---

## Phase 3: User Story 1 - Surface NS-set divergence between parent and zone (Priority: P1) 🎯 MVP

**Goal**: Operators can identify NS hostnames that the parent advertises but no auth reports (or vice versa), via both per-NS metric series and a per-zone status-table row. The MVP is functional and viewable in Grafana for the canonical hidden-master case.

**Independent Test**: After implementation, querying `/metrics` for the demo's `hidden-master.demo.` zone shows `dnshealth_ns_classification{...classification="self-only"} 1` for the phantom NS, `dnshealth_ns_classification_count{...classification="self-only"} 1`, and the dashboard NS — status panel's new row reads FAIL for that zone and PASS for `healthy.demo.`.

### Implementation: classifier prober

- [X] T004 [US1] Implement `ProbeNSClassification` body in `prober/ns_classification.go`. (a) Build `parentSet` from `delegation.NSRecords` (case-folded canonical FQDN per FR-006). (b) For each parent-listed nameserver, issue one `<zone> NS` query and union the response's NS RR set into `selfSet`. (c) Iterate `parentSet ∪ selfSet`; for each hostname, derive classification (`parent-only` / `self-only` / `both`) and emit a `ProbeResult` with `Check: "ns_classification"`, `Labels: {"classification": ...}`, `Metrics: {"ns_classification": 1}`, and the IP populated from the parent-side glue when known (empty string otherwise — see contracts/classification-metric.md). Use `ExchangeWithRetry` and the existing `ResolveAddress` helper for consistency with other probers.

### Integration tests: classifier behavior

- [X] T005 [US1] Create `prober/ns_classification_test.go` (built with `//go:build integration`) and add `TestNSClassification_HappyPath` — zone where parent and auth report identical NS sets `[A, B]`; assert each NS gets `classification="both"` and that no `self-only` or `parent-only` series appears for the zone.
- [X] T006 [US1] Append `TestNSClassification_SelfOnlyStealth` to `prober/ns_classification_test.go` — zone where parent advertises `[A, B]` and the auth's NS RR set is `[A, B, C]`; assert `C` gets `classification="self-only"` while `A` and `B` get `classification="both"`.

### Cycle-runner integration test

- [X] T007 [US1] Add `TestCycleRunner_NSClassificationCountResetsAndZeroes` to `cycle/runner_test.go` — verify that across consecutive cycles, a zone with no asymmetry consistently emits `dnshealth_ns_classification_count{classification="self-only"} 0` (explicit zero, not series absent), demonstrating the per-cycle Reset+Set pattern from data-model.md R-2.

### Dashboard row + JSON regen

- [X] T008 [US1] Add new `statusCheck` row G to `nsStatusChecks` in `demo/dashboard/panels_status.go` using the PromQL predicate from `contracts/dashboard-row.md`. Populate the required `detail` field with the full multi-line markdown from the contract (metric / why-FAIL-matters with hidden-master caveat / RFC-strict-stealth scope disclaimer / investigate pointer). The `TestStatusChecksHaveDetail` guard test will reject an empty detail.
- [X] T009 [US1] Regenerate dashboard JSON via `make dashboards`; both `demo/grafana/dashboards/dnshealth-overview.json` and `dnshealth-overview-demo.json` will pick up the new row. Verify `go test -tags=integration ./demo/dashboard/...` passes (drift test).

### Demo zone: hidden-master.demo.

- [X] T010 [P] [US1] Create `demo/coredns/hidden-master/Corefile` and `demo/coredns/hidden-master/zones/hidden-master.demo.zone`. Auth container serves the zone with NS RR set `[ns1.hidden-master.demo., ns2.hidden-master.demo., hidden-primary.hidden-master.demo.]` — the third name being the stealth NS (no A record anywhere reachable, modeling a leaked listing per research R-6).
- [X] T011 [P] [US1] Add `coredns-hidden-master` service to `demo/docker-compose.yml` at `ipv4_address: 172.31.0.21` with aliases `ns1.hidden-master.demo.` and `ns2.hidden-master.demo.`. Add it to the exporter's `depends_on` list.
- [X] T012 [US1] Add the `hidden-master.demo.` delegation to `demo/coredns/root/zones/demo.zone` with NS records `ns1.hidden-master.demo.` and `ns2.hidden-master.demo.` and A glue at `172.31.0.21` for both. (Note: only `ns1` and `ns2` — NOT `hidden-primary` — appear in the parent referral; that's the asymmetry trigger.)
- [X] T013 [US1] Add `hidden-master.demo.` to the `zones:` list in `demo/exporter/dnshealth.yml`.

### Smoke verification

- [X] T014 [US1] Add smoke assertion A3d to `demo/smoke.sh` verifying that `dnshealth_ns_classification{zone="hidden-master.demo.",nameserver="hidden-primary.hidden-master.demo.",classification="self-only"} 1` is present AND `dnshealth_ns_classification_count{zone="hidden-master.demo.",classification="self-only"}` reads exactly 1. Also assert the healthy.demo. case: `dnshealth_ns_classification_count{zone="healthy.demo.",classification="self-only"}` reads 0.
- [X] T015 [US1] Run `cd demo && docker compose down -v && ./smoke.sh` and verify all A1-A6 assertions including the new A3d pass cleanly on a cold-cached run.

**Checkpoint**: US1 complete and independently shippable as MVP. Operators can detect asymmetric NSes via metrics and dashboard.

---

## Phase 4: User Story 2 - Distinguish hidden masters from forgotten servers (Priority: P2)

**Goal**: Operators can determine, from the dashboard alone, whether an asymmetric NS is a working hidden master (legitimate, NOTIFY-driven primary outside the public NS set) or a leaked / forgotten listing (operational hazard).

**Independent Test**: For the `hidden-master.demo.` zone's `hidden-primary.hidden-master.demo.` (no A record anywhere reachable), the new `dnshealth_ns_stealth_reachable` gauge reads `0` — distinguishable from a working hidden master (whose reachable gauge would read `1`). The row G dashboard detail text points operators at this gauge.

### Active stealth-NS reachability probe (FR-010, post-analyze remediation for finding C1)

- [X] T016 [US2] Extend `ProbeNSClassification` in `prober/ns_classification.go` with a follow-up pass: after computing per-NS classifications, iterate the `self-only` set. For each stealth NS, call `prober.ResolveHostnames(ctx, hostname, client, logger)` and, for each resolved IP, issue an SOA query against the zone. Emit a result with `Metrics["ns_stealth_reachable"] = 1` if any IP returned an authoritative SOA response, `0` otherwise (including resolution failure). Use the existing `ExchangeWithRetry` + `ResolveAddress` helpers; short-circuit the per-IP loop on first authoritative success. Per research R-9, this probe runs ONLY for `self-only` classifications.
- [X] T017 [US2] Add `NSStealthReachable *prometheus.GaugeVec` field to `cycle.Runner` with labels `["zone", "nameserver"]`, register in `NewRunner`, and call `.Reset()` at the start of `Run()` (mirrors `NSClassificationCount` from T002). Extend the aggregation loop in `Run()` (T003's territory) to recognize the new metric name from classifier ProbeResults and `Set` the gauge accordingly.
- [X] T018 [US2] Append `TestNSClassification_StealthReachable_LeakedListing` to `prober/ns_classification_test.go` — fixture where the auth reports a stealth NS hostname whose A record exists nowhere reachable. Assert the classifier emits `ns_stealth_reachable = 0` for that NS.
- [X] T019 [US2] Append `TestNSClassification_StealthReachable_WorkingHiddenMaster` to `prober/ns_classification_test.go` — fixture where the auth reports a stealth NS hostname whose A record IS resolvable AND the resolved server returns an authoritative SOA for the zone. Assert the classifier emits `ns_stealth_reachable = 1` for that NS.

### Detail text refinement (depends on T016-T017 metric existing)

- [X] T020 [US2] Update the `detail` text on row G in `demo/dashboard/panels_status.go` to reference the new `dnshealth_ns_stealth_reachable` metric explicitly as the disambiguation surface (instead of pointing at `dnshealth_query_success{check="soa",nameserver=X}` which would always be absent for stealth NSes). Pattern: "Check `dnshealth_ns_stealth_reachable{nameserver=X}` — 1 = working hidden master, 0 = leaked listing." Run `make dashboards` to regenerate JSON; drift test must pass.

### Multi-auth integration test

- [X] T021 [US2] Append `TestNSClassification_MultiAuthUnion` to `prober/ns_classification_test.go` — zone with two auths reporting different self sets (auth-1 reports `[A, B]`, auth-2 reports `[A, B, C]`); assert the classifier's self set is the UNION `{A, B, C}` per FR-007 and that `C` is classified `self-only`. Verifies the union semantics from research R-4.

### Quickstart accuracy

- [X] T022 [US2] Re-read `specs/007-stealth-nameservers/quickstart.md`'s "Distinguishing legitimate hidden master from leaked / forgotten NS" section against the actually-implemented metric labels. Confirm the example PromQL `dnshealth_ns_stealth_reachable{...}` matches what T016 emits.

**Checkpoint**: US2 complete. Dashboard now both detects asymmetry AND guides the operator through disambiguation without leaving the panel — and the disambiguation gauge actually exists.

---

## Phase 5: User Story 3 - Detect parent-only NSes (the symmetric divergence) (Priority: P3)

**Goal**: Operators see parent-only NS hostnames (the mirror case: parent advertises NS the auth doesn't know about) with the same labels and dashboard treatment as the self-only case.

**Independent Test**: An integration test fixture where parent advertises `[A, B, C]` and auths report `[A, B]` surfaces `C` with `classification="parent-only"`; the dashboard row reads FAIL via the same predicate as US1.

- [X] T023 [US3] Append `TestNSClassification_ParentOnly` to `prober/ns_classification_test.go` — zone where parent advertises three NSes `[A, B, C]` but the auth's NS RR set is `[A, B]` (with proper glue / a working A for C in the root so the parent can reach an authoritative answer). Assert `C` gets `classification="parent-only"`. The same predicate from row G's PromQL means no dashboard change is needed; this task verifies coverage of the symmetric case. (Not [P]: same file as T021 — sequential edit.)

**Checkpoint**: US3 complete. Both divergence directions covered via the same classification metric and dashboard row.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T024 [P] Run `go vet ./...` from repo root — must pass with no warnings.
- [X] T025 [P] Run `go test -tags=integration -count=1 ./...` — full integration test suite must pass with no regressions.
- [X] T026 Run `cd demo && docker compose down -v && ./smoke.sh` end-to-end twice back-to-back (cold cache + warm) to verify the new A3d assertion is stable and #40's readiness loop still works with the added zone.
- [X] T027 Per Constitution Governance section, perform an adversarial code-vs-spec audit before declaring complete: re-read each FR-### in `spec.md` and confirm the implementation actually delivers it. File any gaps as separate issues (do not silently fix in this PR). Save audit notes to `specs/007-stealth-nameservers/audit.md`.

---

## Dependencies

```text
T001 (file scaffold)
  └─→ T004 (classifier body)

T002 (count gauge field)
  └─→ T003 (aggregation loop)
       └─→ T007 (cycle-runner test)
       └─→ T008 (dashboard row uses the count gauge in PromQL)

T004 (classifier body)
  ├─→ T005, T006 (integration tests; sequential edits to same file)
  ├─→ T010, T011, T012, T013 (demo zone wiring)
  ├─→ T014 (smoke assertion depends on metric being emitted)
  └─→ T016 (active reachability probe extends the classifier body)

T010 ∥ T011 (different files; can be done in parallel)
  └─→ T012 (root zone delegation needs both Corefile + compose entry to exist for context)
       └─→ T013 (config edit)
            └─→ T015 (smoke run depends on every demo edit landing)

T008 → T009 (regen depends on source change)

US1 complete (T015 passes)
  ├─→ US2 implementation chain:
  │     T016 (classifier extension) ─┬─→ T017 (runner gauge)
  │                                  ├─→ T018 (leaked test)
  │                                  ├─→ T019 (working test)
  │                                  └─→ T020 (detail text + regen, depends on metric existing)
  │     T021 (multi-auth test) — independent of reachability work
  │     T022 (quickstart drift check — verify after T016 emits)
  └─→ US3 (T023)

US2 + US3 complete
  └─→ Phase 6 polish (T024, T025, T026, T027)
```

## Parallel execution opportunities

- **Within US1**: T010 (Corefile + zone file) and T011 (compose service) edit different files and can be done concurrently. T005 and T006 edit the same file (`ns_classification_test.go`) and must be sequential despite being functionally independent test cases.
- **Within US2**: T016 (extends classifier) and T017 (extends runner) edit different files — could run in parallel. T018 / T019 / T021 / T023 all edit `ns_classification_test.go` and must be sequential.
- **Within Phase 6**: T024 (vet) and T025 (full test) are independent and can run in parallel.

## Implementation strategy

**MVP scope (US1 only, T001-T015)** delivers asymmetry detection end-to-end with a dashboard row that surfaces the divergence — but the row's "hidden master vs leaked listing" disambiguation guidance points at data only T016+T017 produce. Ship US1-only ONLY if you're willing to also ship a dashboard row whose detail text references a metric the operator can't see.

**Recommended scope**: ship US1 + US2 + US3 + Phase 6 polish as one PR. The US2 active-reachability work (T016-T022) is what makes the dashboard row's detail text actually actionable — splitting it off creates a "documented workflow that doesn't work" gap surfaced by speckit-analyze finding C1. Total: 27 tasks, estimated 280-400 LoC of source + 150-200 LoC of tests + ~50 LoC of demo / compose / smoke edits + the regenerated dashboard JSON.

**Per constitutional guidance**: T027's audit gate must complete before the PR is opened. Any gaps surfaced become follow-up issues, not silent fixes.
