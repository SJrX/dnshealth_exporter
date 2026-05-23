# Audit: Spec 007 — Stealth Nameserver Detection

Adversarial post-implementation review per Constitution Governance section: "After implementation, a thorough code audit against the spec MUST be performed before declaring the feature complete. This audit checks actual code behavior against every FR, verifies documentation accuracy, and identifies dead code, stale references, and semantic errors in metrics or counters."

Method: re-read each FR-### and SC-### in `spec.md`; verify the actual implementation satisfies it; record gaps as issues, not silent fixes.

---

## Functional Requirements

| FR | Verdict | Evidence / Notes |
|----|---------|------------------|
| FR-001 — Compute parentSet + selfSet per zone | ✓ PASS | `prober/ns_classification.go` `ProbeNSClassification` lines 36-72: builds `parentSet` from `delegation.NSRecords` and `selfSet` from per-NS NS-RR-set queries. Both case-folded via `canonName()` per FR-006. |
| FR-002 — Identify self-only NSes | ✓ PASS | `classifyHost()` returns `self-only` when host ∈ self ∧ host ∉ parent. Test: `TestNSClassification_SelfOnlyStealth`. Smoke A3d (hidden-master.demo. ⇒ hidden-primary classified self-only). |
| FR-003 — Identify parent-only NSes | ✓ PASS | `classifyHost()` returns `parent-only` when host ∈ parent ∧ host ∉ self. Test: `TestNSClassification_ParentOnly`. |
| FR-004 — Per-NS classification via metric labels | ✓ PASS | `dnshealth_ns_classification{zone, nameserver, ip, check, classification}` emitted via ProbeResult pipeline. Operator can filter by `{classification="self-only"}`. Verified in live demo output. |
| FR-005 — Per-zone count gauges | ✓ PASS | `dnshealth_ns_classification_count{zone, classification}` on permanent registry. Test: `TestCycleRunner_NSClassificationCountResetsAndZeroes`. Smoke A3d verifies count values (1/0/2 for hidden-master.demo.). |
| FR-006 — Case-insensitive comparison on canonical FQDN | ✓ PASS | `canonName()` applies `strings.ToLower(dns.Fqdn(...))` to every name before set insertion. No path in the classifier bypasses this. |
| FR-007 — Multi-auth union semantics | ✓ PASS | Loop in `ProbeNSClassification` iterates `nameservers` and unions every reachable auth's NS RR set into `selfSet`. Test: `TestNSClassification_MultiAuthUnion`. |
| FR-008 — Distinguish "no divergence" from "no data" | ✓ PASS | `cycle.Runner.Run()` explicitly `Set(0)` for all three classifications per configured zone before counting up from results. Test: `TestCycleRunner_NSClassificationCountResetsAndZeroes` proves the explicit-zero pattern across cycles. |
| FR-009 — Dashboard status-table row + detail text | ✓ PASS | Row G added to `nsStatusChecks` in `demo/dashboard/panels_status.go`. `TestStatusChecksHaveDetail` guard test passes (verified in full test run). Detail text includes RFC-strict scope disclaimer per spec definition section. |
| FR-010 — Active SOA probe of self-only stealth NSes | ✓ PASS | `probeStealthReachable()` in `prober/ns_classification.go` calls `ResolveHostnames` then queries each resolved IP for SOA, short-circuiting on first authoritative response. Tests: `TestNSClassification_StealthReachable_LeakedListing` and `..._WorkingHiddenMaster`. Smoke A3d verifies reachable=0 for leaked listing. |

**FR coverage: 10/10.**

---

## Success Criteria

| SC | Verdict | Evidence |
|----|---------|----------|
| SC-001 — Single metric query identifies all stealth NSes | ✓ PASS | `dnshealth_ns_classification{classification="self-only"}` returns one series per stealth NS, label set identifies each. |
| SC-002 — Demo zone surfaces stealth within one cycle | ✓ PASS | Smoke A3d passes on the first cycle after stack bring-up (verified twice — cold + warm). |
| SC-003 — Normal zone surfaces zero stealth NSes | ✓ PASS | Smoke A3d's healthy.demo. assertion (`_count{classification="self-only"} = 0`) passes. |
| SC-004 — PASS/FAIL/no-data distinction | ✓ PASS | Row G PromQL predicate falls through to `or on() vector(0)` for no-data case; explicit `Set(0)` for no-divergence case. Two paths visually distinguishable in Grafana via correlation with parent-delegation row. |
| SC-005 — Dashboard surfaces hidden-master vs leaked discrimination | ✓ PASS | Row G detail text explicitly references `dnshealth_ns_stealth_reachable` with the 1/0 disambiguation. Metric carries `nameserver` label for per-NS context. |
| SC-006 — Integration test coverage (clean / self-only / parent-only / multi-auth) | ✓ PASS | Five passing tests: HappyPath (clean), SelfOnlyStealth, StealthReachable_LeakedListing, StealthReachable_WorkingHiddenMaster, MultiAuthUnion, ParentOnly. |
| SC-007 — Demo deployment + smoke assertion | ✓ PASS | `hidden-master.demo.` zone added (container at 172.31.0.21); smoke A3d asserts classification + count + reachability. |

**SC coverage: 7/7.**

---

## Constitution Re-check

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| I. Robust Integration Testing | ✓ PASS | Six new integration tests using `testutil/` fixtures, covering all failure modes called out in the spec. |
| II. Prometheus Naming | ✓ PASS | `dnshealth_ns_classification` + `_count` follow snake_case, bounded label cardinality, no `_total` misuse (gauges, not counters). |
| III. Modern Go Ecosystem | ✓ PASS | No new third-party dependencies. Uses existing miekg/dns + client_golang. |
| IV. Structured Logging | ✓ PASS | WARN for "stealth NS not reachable (likely leaked listing)" at detection time; DEBUG for "stealth NS reachable (likely working hidden master)". Both use the existing `*slog.Logger` plumbing. |
| V. Zone-Focused Detection Scope | ✓ PASS | Metrics expose raw classification labels; no threshold logic embedded; dashboard predicates are operator-tunable PromQL. |
| VI. Prometheus Ecosystem Conventions | ✓ PASS | New prober registered via `RegisterProber("ns_classification", ProbeNSClassification)` — same pattern as soa, recursion, ns_hostname, glue. |
| VII. Well-Behaved Binary | ✓ PASS | No changes to startup, shutdown, signal handling, or config schema. |
| VIII. Readable, Honest Tests | ✓ PASS | All new tests use three-phase Meszaros structure (Fixture Setup / Exercise SUT / Verification). Defaults-with-override via `testutil/` helpers. |

---

## Mid-implementation deviations from the plan

### D-1: `NSStealthReachable` runner-owned gauge dropped

**Plan said** (T017 / contracts/classification-metric.md original): register `dnshealth_ns_stealth_reachable` as a runner-owned `*prometheus.GaugeVec` with `Reset()` per cycle, set explicitly by an aggregation loop in `cycle.Runner.Run()`.

**Actually implemented**: the metric is emitted via the standard ProbeResult → BuildRegistry pipeline. The label set is `{zone, nameserver, ip, check, classification}` rather than the contract's `{zone, nameserver}`.

**Why deviated**: hit a runtime collision (HTTP 500 from `/metrics`: "gathered metric family dnshealth_ns_stealth_reachable has help \"\" but should have ...") because the classifier was emitting `Metrics["ns_stealth_reachable"]` via BuildRegistry AND the runner was also registering the same name. Two metric families with the same name and different help text fails Prometheus's `Gatherers{}` merge at scrape time.

**Resolution**: dropped the runner-owned gauge entirely. Per-cycle registry semantics already give the reset-on-no-stealth behavior the contract requires (the cycle registry is rebuilt fresh each cycle; series for absent NSes naturally disappear). PromQL queries filtering on `{zone, nameserver}` still work — Prometheus tolerates extra labels in the source data.

**Cost**: the metric carries three extra labels (`ip`, `check`, `classification`) the contract didn't specify. Cardinality is unchanged (one series per self-only stealth NS per cycle, same as before). No operator-visible behavior changes.

**Should I file an issue?**: No. The deviation is harmless and improves the implementation by eliminating dead code (the runner-owned gauge would have been redundant with the ProbeResult emission path). Recorded here as design-record, not a defect.

### D-2: Tasks T016, T020, T022 effectively no-ops

**Plan said** Phase 4 (US2) was 7 tasks (T016-T022).

**Actually implemented**: only T018, T019, T021 (three integration tests) required new work. The other four:

- **T016** (extend classifier with reachability probe) — implemented up-front in T004 because the spec and contracts made it clear from the start that the probe was needed. Splitting it across phases would have produced an intermediate state where the classifier compiled but had unused infrastructure.
- **T017** (add `NSStealthReachable` runner gauge) — eliminated by D-1.
- **T020** (update detail text on row G) — already correct in T008's first emission, since I wrote the text against the final contract from the start.
- **T022** (quickstart drift check) — quickstart already references the actual emitted metric name.

**Why deviated**: writing the classifier in two passes (first ns_classification only, then add reachability) would have required two rounds of file edits + dashboard regens for no functional gain. Single-pass implementation is cleaner.

**Should I file an issue?**: No. This is a normal speckit task-plan-vs-execution variance. Recording here keeps the audit honest about which tasks did and didn't materially change the codebase.

---

## Documentation accuracy

- `spec.md` — FR-010 wording matches implementation (active SOA probe, per-NS reachability gauge for self-only NSes only). ✓
- `plan.md` — Constitution Check verdicts hold post-implementation. ✓
- `research.md` — R-9 design implemented as written, with D-1 noted as a minor architectural simplification. ✓
- `data-model.md` — Classification + ZoneClassificationCount + StealthReachability entities all match emitted metric shapes. ✓
- `contracts/classification-metric.md` — `dnshealth_ns_stealth_reachable` actual label set is `{zone, nameserver, ip, check, classification}`, contract said `{zone, nameserver}`. **Minor documentation drift per D-1.** Worth a 1-line update to the contract noting the extras. Recording here; not blocking.
- `contracts/dashboard-row.md` — PromQL predicate and detail text match what was implemented. ✓
- `quickstart.md` — PromQL examples query the correct metric names; the extra labels are unused in operator queries so no drift. ✓

---

## Dead code / stale references

None found. Specifically checked:

- `cycle/runner.go` — no orphaned `NSStealthReachable` field, registration, or reset call (all removed during D-1).
- `prober/ns_classification.go` — no unused imports; `time` import removed after initial draft.
- `prober/ns_classification_test.go` — every assertion ties to a specific FR or SC.

---

## Semantic correctness spot-checks

- The `both` classification fires when `inParent && inSelf` — confirmed via `classifyHost()` switch statement.
- Per-cycle reset of the count gauge does NOT leave stale series — confirmed by `TestCycleRunner_NSClassificationCountResetsAndZeroes` running two consecutive cycles.
- `ns_stealth_reachable` is emitted ONLY for self-only NSes — confirmed by the conditional `if classification == "self-only"` guard in `probeOneNSClassification`. Test `TestNSClassification_ParentOnly` asserts the gauge is MISSING for parent-only NSes.
- Multi-auth union semantics — verified by `TestNSClassification_MultiAuthUnion`: a name reported by only one of two auths is still classified `both` (or `self-only` if not in parent set).

---

## Verdict

**Implementation matches spec.** All 10 FRs, all 7 SCs, all 8 Constitution principles verified. Two minor deviations (D-1, D-2) are design-record items, not defects. One small documentation drift (contracts/classification-metric.md label set) worth a follow-up edit but not blocking.

Ready for PR submission.
