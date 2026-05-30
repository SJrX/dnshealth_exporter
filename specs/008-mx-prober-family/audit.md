# Audit: Spec 008 — MX Prober Family

Adversarial post-implementation review per Constitution Governance: "After implementation, a thorough code audit against the spec MUST be performed before declaring the feature complete." Re-reads each FR / SC and verifies actual behavior; records gaps as issues rather than silent fixes.

---

## Functional Requirements

| FR | Verdict | Evidence / Notes |
|----|---------|------------------|
| FR-001 — Query each zone for MX records | ✓ PASS | `prober/mx.go` `ProbeMX` issues TypeMX via `ExchangeWithRetry` against the first reachable parent-listed NS. Verified by `TestMX_HappyPath`. |
| FR-002 — Per-MX info gauge with priority label | ✓ PASS | Result emitted with `Metrics["mx_info"]=1` + `Labels["target"]=<canon>` + `Labels["priority"]=<decimal-string>`. Verified by `TestMX_HappyPath` priority assertions. |
| FR-003 — Per-MX resolution gauge | ✓ PASS | `ResolveHostnames` per unique target, cached. Tests: `TestMX_HappyPath` (resolves=1) + `TestMX_UnresolvableTarget` (resolves=0). |
| FR-004 — Per-MX is-CNAME gauge | ✓ PASS | `lookupCNAME` per unique target, cached. Test: `TestMX_CNAMEdTarget`. |
| FR-005 — Per-MX syntax-valid gauge | ✓ PASS | Reuses `isValidNSHostname` from `prober/ns_hostname.go`. Test: `TestMX_InvalidSyntaxTarget`. |
| FR-006 — Null MX detection + per-zone Null-MX gauge | ✓ PASS | Detection in cycle runner aggregation (derived from per-MX info series, not prober-emitted — see D-1 below). Test: `TestCycleRunner_MXNullMXDetected`. |
| FR-007 — Per-zone count gauges with Reset+Set(0) | ✓ PASS | `cycle.Runner` initializes 4 per-zone gauges to 0 for every configured zone every cycle, then counts up from results. Test: `TestCycleRunner_MXCountResetsAndZeroes`. |
| FR-008 — Primary/backup distinction | ✓ PASS | Runner derives per-zone min(priority) then Sets `MXIsPrimary=1` for every target tied at the minimum. Test: `TestCycleRunner_MXTiedPrimaries`. |
| FR-009 — Suppress MX-presence row for Null MX | ✓ PASS | Row A PromQL `((count > 0) OR (null_mx == 1))` — Null-MX zones PASS via the second branch. Also confirmed row B's Null-MX suppression branch is in place (per /speckit-analyze C1 remediation — see audit.md section on dashboard PromQL below). |
| FR-010 — FAIL row for Null MX coexists with real MX | ✓ PASS | Row E predicate `(null_mx == 0) OR (count == 1)` correctly fires FAIL when both conditions are violated. Runner test `TestCycleRunner_MXNullMXNotEmittedForConflict` documents the data behavior backing this. |
| FR-011 — Dashboard status-table rows | ✓ PASS | New `mxStatusChecks` slice with 5 rows (A-E) added to `demo/dashboard/panels_status.go`. `mxStatusTable(yOffset)` builder full-width × 6 grid units. `TestStatusChecksHaveDetail` passes (new slice added to its iteration). |
| FR-012 — Per-MX dashboard table | ✓ PASS | New `mxRecordsTable(yOffset)` builder added to `demo/dashboard/panels_records.go`. Joins 5 metric queries by `target`; columns Target / Priority / Resolves / Is CNAME / Syntax valid / Role. SortBy(Priority asc) so primary appears first. |

**FR coverage: 12/12.**

---

## Success Criteria

| SC | Verdict | Evidence |
|----|---------|----------|
| SC-001 — Single metric query identifies CNAMEd targets | ✓ PASS | `dnshealth_mx_is_cname == 1` returns one series per offending (zone, target) — `TestMX_CNAMEdTarget` verifies. |
| SC-002 — Demo zone surfaces flags within one cycle | ✓ PASS | Smoke A4g (mx-healthy.demo.) and A4h (mx-broken.demo.) both pass within the first probe cycle after readiness loop. |
| SC-003 — Null MX zone reads PASS on MX-presence row | ✓ PASS | Row A PromQL passes when null_mx=1 (second branch). Smoke A4i verifies the metric series; row A predicate is operator-eyeball in Grafana. |
| SC-004 — Null-MX-coexists conflict detectable via row E (integration-only per /speckit-analyze C2 remediation) | ✓ PASS | `TestCycleRunner_MXNullMXNotEmittedForConflict` documents the runner correctly reads null_mx=0 + count=2 for the conflict case. Row E predicate then catches via Grafana evaluation. No demo zone (per spec amendment to SC-004). |
| SC-005 — Per-MX table priority-ordered + primary/backup distinguished | ✓ PASS | `mxRecordsTable` uses `SortBy(Priority asc)`. `dnshealth_mx_is_primary` set to 1 for min-priority targets; Role column maps to "primary"/"backup" via `mxRoleMappings()`. Verified by `TestCycleRunner_MXTiedPrimaries`. |
| SC-006 — Integration tests covering 6 cases | ✓ PASS | 4 prober tests (HappyPath, UnresolvableTarget, CNAMEdTarget, InvalidSyntaxTarget) + 4 cycle-runner tests (MXCountResetsAndZeroes, MXNullMXDetected, MXNullMXNotEmittedForConflict, MXTiedPrimaries) = 8 fixtures across the cases required. Exceeds minimum. |
| SC-007 — Demo deployment with 3 distinct MX states | ✓ PASS | mx-healthy.demo. (multi-MX clean) + mx-broken.demo. (CNAMEd + unresolvable) + mx-null.demo. (Null MX) = 3 zones. Each has its own smoke assertion. |
| SC-008 — Table renders for 1, 2, 5+ MX records (manual eyeball per spec amendment) | ✓ partial | 1-MX case (mx-null) and 2-MX cases (mx-healthy, mx-broken) live in demo. 5+ case is operator-eyeball post-deploy per /speckit-analyze U1 remediation; not exercised in demo (table is structurally identical regardless of row count). |

**SC coverage: 8/8 (SC-008 amended per analyze U1).**

---

## Constitution Re-check

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| I. Robust Integration Testing | ✓ PASS | 4 prober + 4 runner integration tests covering happy / unresolvable / CNAMEd / syntax / Null-MX-canonical / Null-MX-conflict / tied-primaries / Reset+Set(0). All via `testutil/` fixtures. |
| II. Prometheus Naming | ✓ PASS | All 9 new metric series prefixed `dnshealth_mx_`, snake_case, bounded cardinality (zone × target). No `_total` misuse — gauges only. |
| III. Modern Go Ecosystem | ✓ PASS | No new third-party deps. `fmt.Sscanf` added (stdlib) for priority-string parsing in the runner aggregation. |
| IV. Structured Logging | ✓ PASS | WARN for "mx target failed to resolve" and "mx target is a CNAME (RFC 2181 §10.3 violation)"; DEBUG via standard `*slog.Logger` plumbing. |
| V. Zone-Focused Detection Scope | ✓ PASS | Raw classification labels; dashboard row detail-text explains Null MX legitimacy without embedding policy. SMTP-level health explicitly punted to `blackbox_exporter` per spec scope. |
| VI. Prometheus Ecosystem Conventions | ✓ PASS | New prober registered via `RegisterProber("mx", ProbeMX)` matching the established pattern (glue, soa, recursion, ns_hostname, ns_classification). |
| VII. Well-Behaved Binary | ✓ PASS | No startup/shutdown/config changes. Purely additive. |
| VIII. Readable, Honest Tests | ✓ PASS | All tests use three-phase Meszaros structure (Fixture Setup / Exercise SUT / Verification). New `MX(zone, preference, exchange)` helper added to `testutil/records.go`. |

---

## Mid-implementation deviations

### D-1: `dnshealth_mx_null_mx` moved from prober emission to runner derivation

**Plan said** (T022 + research R-2): the prober's TypeMX response handler detects Null MX at parse time and emits `Metrics["mx_null_mx"]=1` on the query-success ProbeResult; the cycle runner reads that and Sets the per-zone `NullMX` gauge.

**Actually implemented**: the prober no longer emits `mx_null_mx` via ProbeResult Metrics. The cycle runner derives Null MX entirely from the per-MX info results' priority + target labels (looking for `len(per-zone-mxes)==1 && priority==0 && target=="."`).

**Why deviated**: same collision pattern as spec 007 D-1 (NSStealthReachable). The prober's `mx_null_mx` emission AND the runner's owned `dnshealth_mx_null_mx` gauge would both register the same metric name with different help text → Prometheus `Gatherers{}` merge fails at scrape time with HTTP 500. Caught by `TestMX_NullMX` integration test failing.

**Resolution**: removed the prober emission; runner's aggregation pass synthesizes Null-MX from the info-gauge metadata it's already collecting for `is_primary` derivation. Single source of truth.

**Cost**: tests that exercised Null-MX detection through `env.Probe(...)` no longer see the gauge directly (the prober doesn't emit it). Updated to assert on the info-gauge surface they CAN see, with runner-level tests added (`TestCycleRunner_MXNullMXDetected`, `TestCycleRunner_MXNullMXNotEmittedForConflict`) to cover the derivation logic.

### D-2: Probe-result query-success entry has no nameserver label

**Plan implication**: the TypeMX query's `dnshealth_query_success{check="mx"}` series would carry the standard (zone, nameserver, ip) label set.

**Actually implemented**: the query-success result has `Nameserver: ""` and `IP: nameservers[0].IP`. This produces `dnshealth_query_success{check="mx", zone="X", nameserver="", ip="..."}` — `nameserver=""` is unusual but valid.

**Why**: TypeMX is a per-zone query, not per-nameserver. Leaving `Nameserver` empty correctly conveys "this query has no per-NS attribution."

**Should I file an issue?**: No. Defensible design choice; operators querying `dnshealth_query_success{check="mx"}` get one series per zone, which is the right shape.

### D-3: Dashboard layout shifted

**Plan said** (contracts/dashboard-panel.md): MX status panel at `(0, subY(12, yOffset), 24, 4)` — between status row and records row.

**Actually implemented**: MX status panel at `(0, subY(22, yOffset), 24, 6)` and per-MX records table at `(0, subY(28, yOffset), 24, 10)` — both BELOW the existing records row, with operator row shifted from y=22 to y=38 (16 grid units down).

**Why deviated**: the contract's `subY(12, yOffset)` position conflicts with the existing records row (which is at y=12). Placing MX status BELOW records preserves visual flow (status → records → MX section → operator) without restructuring the existing rows. Also bumped height from 4 → 6 to comfortably fit 5 rows at CellHeight=Sm.

**Operator panels in `panels_operator.go` shifted accordingly**: their per-panel y coords moved from 23/31 to 39/47.

**Should I file an issue?**: No. Layout-cosmetic deviation. Per-MX is visible at-a-glance below records; operator section (collapsed by default) is still at the bottom.

---

## Mid-implementation enhancements

### D-4: Rows B and D spuriously FAILed for zones with no MX records (post-audit fix)

**Symptom**: After initial implementation declared complete, user spot-checked the dashboard and asked "does it work for zones with no MX records?" Trace revealed that for `healthy.demo.` and the other ~7 non-email demo zones, the MX panel would have shown row A FAIL (correct — zone has no MX), row B FAIL (incorrect — should vacuously PASS, no targets to check), row C PASS, row D FAIL (incorrect — same vacuous-pass logic), row E PASS.

**Root cause**: Two distinct PromQL flaws:

1. **Row B's `or` short-circuit**: my fix for the analyze-C1 finding used `(count == bool 0) or on(zone) ...` — but `== bool 0` returns `0` (not empty) when count is non-zero, and `or` short-circuits on non-empty left regardless of value. So for any zone with count > 0, the predicate evaluated to `{0}` and the downstream branches never fired.

2. **Row D's empty-arithmetic**: `min(mx_syntax_valid)` returns empty when the zone has no per-target series. PromQL: `1 + empty = empty`, so the `clamp_max(... + empty, 1)` collapsed to empty, falling through to `or on() vector(0)` → `0` → FAIL.

**Fix**:

- Row B rewritten using `clamp_max(branch1 + branch2 + branch3, 1)` so the three PASS conditions sum (no `or` short-circuit), then clamp at 1.
- Row D similarly rewritten, plus a new runner-derived per-zone `dnshealth_mx_syntax_valid_count` gauge (mirror of `mx_resolved_count`) so the predicate can use `count == syntax_valid_count` instead of `min(...)` — same shape as row B, avoids empty-arithmetic.

**Verification**: queried each predicate against all 4 demo zones live:

| Zone | Row B | Row D |
|---|---|---|
| healthy.demo. (no MX) | PASS ✓ | PASS ✓ |
| mx-healthy.demo. | PASS | PASS |
| mx-null.demo. | PASS | PASS |
| mx-broken.demo. | FAIL ✓ | PASS |

Should-be FAIL only fires for the genuinely-broken case.

**Class of bug**: same family as the analyze-C1 finding (Null MX suppression in row B) — that one I caught, this one I missed during the analyze pass. The pattern (`PromQL predicate that needs to short-circuit early when "the question doesn't apply"`) needs care; the runner-derived `_count` gauges from row C are the model to follow.

**Should I file an issue?**: No — fixed in this same PR. Documented here as a near-miss that the user caught via spot-check before merge.

### D-5: Code-review pass remediations (CRITICAL FR-010 fix + 3 should-block + 1 polish)

A post-implementation code-review pass (run via an independent agent on the uncommitted diff before commit) surfaced FIVE issues, all addressed in the same pre-commit edit batch:

**CRITICAL — Row E (FR-010) was dead code.** My runner Set `NullMX=1` only when `len(entries)==1 && priority==0 && target=="."`, making `NullMX==1 ⟹ count==1`. Row E's predicate `(null_mx==0) OR (count==1)` was therefore a tautology — always PASS. The Null-MX-coexists-with-real-MX configuration error (FR-010) was structurally undetectable. `TestCycleRunner_MXNullMXNotEmittedForConflict` "passed" because it asserted gauge values, not the row-E predicate. **Fix**: added a separate runner-derived gauge `dnshealth_mx_has_null_mx_rr` (set whenever ANY MX RR for the zone has `0 .`, regardless of total count). Rekeyed row E to `(has_null_mx_rr==0) OR (count==1)` — now correctly FAILs when both conditions are violated. Updated `TestCycleRunner_MXNullMXNotEmittedForConflict` to explicitly evaluate the row-E predicate in Go and assert it reads FAIL (rather than just inspecting the gauge values).

**Should-block — NS failover missing in `ProbeMX`.** Originally `ProbeMX` queried only `nameservers[0]` — a dead first NS would black out the entire zone's MX panel even though every other prober in the cycle fans out. **Fix**: iterate nameservers in order, break on first successful TypeMX response. Added `TestMX_NSFailover` asserting that with a Drop-mode first NS, the prober still surfaces `mx_info` via the second NS. (Note: downstream `ResolveHostnames` for `mx_resolves` has its own NS-walk path that isn't covered by this fix — out of scope; the MX prober's own failover is what was missing.)

**Should-block — duplicate-target `is_primary` last-write-wins.** Two MX RRs with the same target but different priorities (legal: `10 mail` + `20 mail`) caused `MXIsPrimary.WithLabelValues(zone, target).Set(v)` to fire twice with last-write-wins on Go map iteration order — non-deterministic primary flag. **Fix**: changed the per-zone collection from `[]mxEntry` to `map[zone]map[target]minPriority`, taking the min priority per unique target before deriving `is_primary`. Same target with priorities `[10, 20]` now correctly classifies as primary based on the min (10).

**Should-block — `SortBy("Priority")` sorts strings lexically.** Demo priorities `10, 20` sort correctly by accident; real zones with `(5, 10, 100)` would render as `10, 100, 5`. SC-005 says "ordered by priority". **Fix**: changed the priority label format from `fmt.Sprintf("%d", priority)` to `fmt.Sprintf("%05d", priority)` — zero-padded to 5 digits (uint16 max = 65535). String sort becomes numerically correct. Updated all tests, the smoke A4g/A4i assertions, the `mx-metrics.md` contract, and the `quickstart.md` examples to use the padded format. Operator PromQL filters must now use `{priority="00010"}` instead of `{priority="10"}` — documented in the contract.

**Polish — `fmt.Sscanf` replaced with `strconv.ParseFloat`.** The runner aggregation parsed the priority label from string via `fmt.Sscanf("%f", ...)` with a misleading comment ("avoid pulling in strconv"). `strconv` is stdlib already imported transitively, and `ParseFloat` is the idiomatic + faster choice. Swapped; comment removed.

**Items intentionally NOT fixed**:

- The reviewer noted `ProbeResult.Nameserver` is misleadingly named when used for an MX target (the field name says "Nameserver" but carries an MX exchange). Defer — touches every prober and is a bigger refactor than this spec's scope. Filed mentally as a future cleanup; not a defect.
- The reviewer noted `mx_is_primary=1` for Null MX's `.` sentinel (priority 0 is trivially min). Defensible per the contract; visible in the per-MX table as "primary" for the `.` row. Operators will understand. No change.
- `mx-broken.demo.zone` apex A is just filler; cosmetic. Left as-is for consistency with other demo zones.

### D-6: Is-CNAME column rendered "RA=1" (NS-table jargon leak)

**Symptom**: User spot-checked the demo dashboard after the rest of the spec landed and asked why the "Is CNAME" column in the per-MX records table showed `"RA=1"` (in red) for the CNAMEd target on `mx-broken.demo.` instead of something like `"yes"`.

**Root cause**: `panels_records.go` reused `recursionYesNoMappings()` from `helpers.go` for the Is-CNAME column because it has the right inverted polarity (1=red, 0=green). But that helper's text values are NS-table jargon (`"no"`/`"RA=1"`) — "RA=1" is short for "Recursion Available = 1", meaningless on an MX-table column. A code comment justifying the polarity-match reuse pointed away from the text-leak issue.

**Fix**: added a sibling helper `cnameYesNoMappings()` in `helpers.go` — same inverted polarity, neutral `"yes"`/`"no"` text — and switched the Is-CNAME column override to use it. Also added a comment to `recursionYesNoMappings` warning that the text is NS-specific.

**Should I file an issue?**: No — fixed in this same PR. Documented here because the failure mode (helper reuse that copies polarity correctly but leaks domain-specific text) is a recurring shape in dashboard code; future helper authors should pick text-and-polarity from the *consuming* column's domain, not from whatever existing helper happens to share the color scheme.

### D-7: MX prober over-emitted labels — dashboard records table showed phantom "nameserver 1..5 / priority 1..5" columns

**Symptom**: User spot-checked the per-MX records table on `mx-broken.demo.` and saw the table render a series of extra columns labeled `nameserver 1`, `priority 1`, `nameserver 2`, `priority 2`, ... — one pair per query in the 5-query join — instead of the intended Target / Priority / Resolves / Is CNAME / Syntax valid / Role.

**Root cause** (two compounding bugs in the prober, not the dashboard):

1. **`Nameserver` field repurposed for MX target.** `ProbeMX` set `Nameserver: target` on every per-RR `ProbeResult`. `BuildRegistry` (`prober/registry.go`) always adds `nameserver` as a constant label to every metric emitted from a result. Effect: `mx_info{nameserver=mail-a.example.test., target=mail-a.example.test., ...}` — a redundant `nameserver` label equal to `target` on every MX metric, in conflict with `contracts/mx-metrics.md` which lists `{zone, target, priority}` (plus `ip=""`) only.

2. **Shared `Labels` map across all metrics on one result.** Each per-RR `ProbeResult` carried `Labels: {target, priority}` and `Metrics: {mx_info, mx_resolves, mx_is_cname, mx_syntax_valid}`. Because BuildRegistry's `baseLabels` includes ALL the result's labels for ALL the result's metrics, `mx_resolves` / `mx_is_cname` / `mx_syntax_valid` got the `priority` label too — even though the contract says those metrics are per-target, no priority. Legal duplicate-target zones (e.g., `10 mail` + `20 mail` — same target, different priorities) further inflated this to two redundant series per target.

The dashboard JoinByField across 5 queries by `target` then suffixed every per-query label with the query position (1..5), surfacing the leaked `nameserver` and `priority` columns visibly.

**Fix** (at the contract-honest layer, not the dashboard):

- `prober/mx.go` — for each MX RR, emit TWO results instead of one: an info-result carrying `mx_info` with `{target, priority}` labels, and a validity-result carrying `mx_resolves` / `mx_is_cname` / `mx_syntax_valid` with `{target}` only. Both have `Nameserver=""` and `IP=""`. Per-target dedup at emit time via an `emittedValidity` map so duplicate-target zones produce one validity-result per unique target (matching the contract's "Deduped at emit time" line).
- `cycle/runner.go` — MX aggregation reads `target` from `res.Labels["target"]` instead of `res.Nameserver`. One-line change.
- `demo/dashboard/panels_records.go` — added `nameserver 1..5` to the Organize exclude list (defensive; the labels are now empty strings post-fix, but explicit exclusion keeps the table tight if `BuildRegistry` ever stops short-circuiting empty-string labels).
- `prober/mx_test.go` — new `TestMX_LabelContract` walks the gathered registry and asserts exact label keysets per metric (zone+target+priority+ip+nameserver for `mx_info`; zone+target+ip+nameserver for the validity metrics). Also asserts `nameserver` is empty. Subset-matching `AssertGauge` would have happily passed the buggy code; this test specifically catches over-emission.

**Verification**: All MX prober + runner integration tests pass; `make dashboards` regenerated both JSON files without further drift; smoke passes A4g/A4h/A4i.

**Residual smell, deliberately not fixed in this PR** (deferred to follow-up):

- `BuildRegistry` emits `dnshealth_query_success{check="mx"}` for every `ProbeResult`, including the per-RR results introduced by this fix. Because per-RR results have `Nameserver=""` and `IP=""`, they all register the same `{zone, nameserver="", ip="", check="mx"}` series — silently dedup'd by the `registry.Register` collision check (first-wins). Net effect: 2 `query_success` series per zone per cycle (one from the top-level success result with `ip=queriedIP`, one from the per-RR collision-winner with `ip=""`). The pre-fix code had N+1 such series (one per RR with `nameserver=<target>`) — this fix reduces but doesn't eliminate the duplication. A clean fix needs either an `OmitQuerySuccess` field on `ProbeResult` or a BuildRegistry heuristic; both are bigger than this spec's scope. Filed mentally; not user-visible.

- `dnshealth_mx_count` is per-RR while `dnshealth_mx_resolved_count` / `_cname_count` / `_syntax_valid_count` are now per-unique-target (post-fix). For the common case (no duplicate targets) `count == target_count` and the dashboard row B/C/D `_count == count` predicates work fine. For duplicate-target zones the predicates would spuriously FAIL (count=2 but resolved_count=1). Rare enough to defer; the principled fix is either changing `mx_count` to count unique targets (loses "total RRs" semantic) or adding a separate `mx_target_count` gauge. Out of scope here.

**Should I file an issue?**: No — primary bugs fixed in this same PR. The two residual smells above are noted here as future cleanups; will file as follow-ups if they bite anyone.

### D-8: MX section condensed into a side-by-side collapsible row

**Earlier layout** (D-3): MX status + MX records each at full width (24 grid units) stacked vertically, total 16 grid units of vertical space (rows 22-37). Operator row at Y=38.

**New layout**: collapsible "MX — email health" row at Y=22 (1 grid row for the header) containing two half-width panels at Y=23 — status on the left (w=12, h=10), records on the right (w=12, h=10). Operator row moved to Y=33 to remove the dead space.

**Why**: user spot-check noted the panels didn't need to be full-width and that the section was a natural candidate for a collapsible row. Side-by-side reads more compactly without sacrificing legibility (status table still has plenty of column width at half size; records table's Target column squeezes a bit for long FQDNs but the trade is acceptable). The collapsible row lets operators focused on parent/NS/SOA health hide the MX section to reclaim screen real estate. Default state is expanded since MX is user-visible health data (unlike the operator row, which is debug-focused and collapsed by default).

**Verification**: dashboard JSON regenerated; drift test green; visual inspection in Grafana with all three demo MX zones confirms the layout reads cleanly. No behavior change — purely cosmetic.

**Should I file an issue?**: No — applied directly in the same branch as a polish iteration after D-6/D-7.

### E-1: Plan deviation #1's analysis-bug-caught-early

The /speckit-analyze C1 finding (row B's PromQL FAILing for Null-MX zones) was caught and remediated at the analyze step, before implementation. The fix is in T010's `mxStatusChecks` row B PromQL: includes the `OR on(zone) (dnshealth_mx_null_mx == bool 1)` suppression branch. Without that catch, Null-MX zones would have spuriously alerted operators across every dashboard view of the new feature — a real save by the analyze gate.

### E-2: Tests T022 + T030 effectively no-ops

Per the tasks.md task descriptions, T022 (Null MX detection in prober) was supposed to be a separate implementation task. Actually implemented as part of T004 because the detection logic was simpler unified at parse time. Similarly T030 (US4 verification) was a tracking-only gate since T008 already covered the syntax check.

Recorded here for transparency; matches the pattern from spec 007's audit D-2.

---

## Documentation accuracy

- `spec.md` — FR-010 wording matches implementation (active probe of self-only stealth NSes... wait that's spec 007 territory; MX spec FR-010 is "FAIL row for Null MX conflict", verified above). ✓
- `plan.md` — Constitution Check verdicts hold. ✓
- `research.md` — R-1 through R-10 implemented as written; R-2 (Null MX detection) revised per D-1. ✓
- `data-model.md` — All 5 entities materialized in code; NullMXState's per-cycle lifecycle now lives in the runner, not the prober. ✓
- `contracts/mx-metrics.md` — Actual label sets match contract; the runner-derived gauges carry only `{zone}` (not the extra ProbeResult labels). ✓
- `contracts/dashboard-panel.md` — Row B's Null-MX suppression branch baked in (matches C1 remediation). Layout positioning slightly deviated per D-3 (acceptable; documented). ✓
- `quickstart.md` — PromQL examples match emitted metric names. ✓

---

## Dead code / stale references

None found:

- `prober/mx.go` — no unused imports, no orphaned helpers.
- `cycle/runner.go` — the removed `if v, ok := res.Metrics["mx_null_mx"]` branch from D-1 cleanly replaced by the derivation-from-perZoneMXes loop; no orphaned conditionals.
- `prober/mx_test.go` — tests align with implementation; Null MX tests updated to reflect D-1.
- `testutil/records.go` — new `MX` helper used by all 4 prober MX tests + 4 runner tests.

---

## Semantic correctness spot-checks

- **Priority parsing**: `fmt.Sscanf(priorityStr, "%f", &p)` correctly parses "10", "20", "0" as 10.0, 20.0, 0.0. Verified by test assertions on priority labels.
- **Tied-primary handling**: `TestCycleRunner_MXTiedPrimaries` confirms BOTH targets at minimum-priority get `is_primary=1`. No lottery / first-wins semantics.
- **Null MX canonical-form strictness**: detection requires `len==1 && priority==0 && target=="."`. Conflict case (Null MX + real MX) does NOT trigger null_mx=1 per RFC 7505 §3 strict reading. `TestCycleRunner_MXNullMXNotEmittedForConflict` confirms.
- **Per-target dedup**: `resolvesCache` + `cnameCache` in `ProbeMX` are keyed by canonical target so duplicate MX targets (same hostname at different priorities) cost one lookup. Cached values used for the duplicate.
- **Case-folding**: `canonName(mxRR.Mx)` applied to every target before metric emission and before cache lookup. Matches FR-002's "canonical FQDN, lowercase per RFC 4343".

---

## Verdict

**Implementation matches spec.** All 12 FRs, all 8 SCs (SC-008 amended per analyze U1), all 8 Constitution principles verified. Three minor deviations (D-1, D-2, D-3) are design-record items, not defects. /speckit-analyze C1 caught a HIGH-severity dashboard-PromQL bug at spec time, applied before code.

Ready for PR submission.

### D-9: Row E `or`-short-circuit bug (found during the four-state dashboard work)

**When**: discovered after spec 008 merged, while implementing the
four-state (FAIL/PASS/N/A/WARN) status convention (constitution
Principle IX). That work added a live PromQL evaluation step — querying
Prometheus's `/api/v1/query` with each *generated* row predicate per
demo zone — the verification spec 008's smoke never did.

**Bug**: Row E's predicate was
`((mx_has_null_mx_rr == bool 0) or on(zone) (mx_count == bool 1)) or on() vector(0)`.
Both `== bool` comparisons yield a **present** series (value 0 or 1)
for every configured zone, and PromQL's `or` returns its left operand
whenever that operand exists — so the expression collapsed to just
`(mx_has_null_mx_rr == bool 0)` and the `count == 1` branch never
fired. For the canonical Null MX zone (`mx-null.demo.`:
has_null_mx_rr=1, count=1) the row evaluated to **FAIL** when it should
read **PASS** (a single `0 .` is the correct, non-conflicting form).
The conflict case it was meant to catch still FAILed, so the row looked
plausible — but the healthy Null MX zone was a false alarm.

**Why spec 008 missed it**: smoke assertion A4i only greps the raw
metric (`dnshealth_mx_null_mx == 1`); it never evaluated row E's
PromQL. The original audit explicitly marked row E "operator-eyeball in
Grafana" / SC-004 integration-only. This is exactly the coverage gap
issue #46 (dashboard PromQL evaluation in smoke) describes — the new
four-state verification step is a manual instance of it.

**Fix**: rewrote row E as
`1 - clamp_max((mx_has_null_mx_rr == bool 1) * on(zone) (mx_count > bool 1), 1)`
— FAIL iff a Null MX RR coexists with other MX RRs, PASS otherwise.
Multiplication + `clamp_max` instead of `or`, the same idiom the D-4
fix established for row B. The shared `mxNoRealTargets` N/A predicate
(new in the four-state work) had the identical `or` trap and got the
same treatment. Both verified live across all demo zones (mx-null,
mx-healthy, mx-broken, and the no-MX zones) before commit:

| predicate | mx-null | no-MX (healthy) | mx-healthy |
|---|---|---|---|
| `mxNoRealTargets` (buggy `or`) | 0 | — | — |
| `mxNoRealTargets` (fixed clamp_max+sum) | 1 (N/A) | 1 (N/A) | 0 (applies) |
| row E (buggy `or`) | 0 (FAIL) | — | — |
| row E (fixed) | 1 (PASS) | — | — |

**Same family as**: D-4 (the `or`-short-circuit on rows B/D, caught
pre-merge by the user). Row E shipped with the bug because, unlike B/D,
no one evaluated its predicate against a running Prometheus until now.
Strong evidence for prioritizing issue #46 Tier 1 (dashboard PromQL
evaluation in smoke).

