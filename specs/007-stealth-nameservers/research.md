# Research: Stealth Nameserver Detection (Spec 007)

Phase 0 output of `/speckit-plan`. The spec had no `[NEEDS CLARIFICATION]` markers — all open questions in the spec were resolved upfront via the "What 'stealth' means in this feature" definition section. Research below records the technical-design decisions that drive the Phase 1 contracts and the Phase 2 task breakdown.

---

## R-1: Metric shape — info gauge with classification label, or one boolean per classification?

**Decision**: Single info gauge per (zone, nameserver) carrying a `classification` label with one of three values: `parent-only`, `self-only`, `both`. Plus separate per-zone count gauges aggregated from this.

**Rationale**:

- One classification label per NS is the cleanest model — every observed NS hostname gets exactly one classification per cycle. Operators can filter by classification value in PromQL (`{classification="self-only"}`) which is idiomatic.
- Splitting into three boolean metrics (`is_parent_only`, `is_self_only`, `is_both`) inflates cardinality without adding query expressiveness — `{classification="X"}` is equivalent to `{is_X="1"}` in alert rules.
- The per-zone count gauge derives trivially via `count by (zone) (dnshealth_ns_classification{classification="self-only"})` from PromQL, but exposing it as a directly emitted gauge makes status-table rows simpler (single-series boolean predicate instead of an aggregation) and avoids the "no series = 0 or no data?" ambiguity.

**Alternatives considered**:

- *Three boolean gauges per NS* — rejected for cardinality and PromQL ergonomics reasons above.
- *Single per-zone-only metric with NS names in label* — rejected because high-cardinality label values per a single series make PromQL filtering awkward. The current model (one series per NS) matches how `dnshealth_ns_record` already works.

---

## R-2: How to handle "no data this cycle" vs "no divergence"

**Decision**: The per-zone count gauges (`dnshealth_ns_classification_count{classification="parent-only"|"self-only"}`) ALWAYS get one series per probed zone per cycle via `GaugeVec.Reset()` + `WithLabelValues(...).Set(...)` (mirrors the `dnshealth_parent_delegation` pattern from PR #29). When a zone's delegation walk fails, the count gauges still get explicit `Set(0)` — distinguishable from "missing series" via direct query.

For the status-table row predicate: PASS only when (parent set non-empty AND self set non-empty AND no asymmetry). FAIL on any of: parent-only count > 0, self-only count > 0, parent or self set empty (covers the "no data" case). Detail text on the row points the operator at the parent-delegation row when both sets are empty.

**Rationale**: Without this distinction, an exporter outage or a totally-broken zone would silently read PASS (no series = no divergence detected = nothing to see). The Reset+Set pattern is already proven in #29 (parent_delegation) and #32 (dashboard wiring).

**Alternatives considered**:

- *Use only per-NS series and let PromQL aggregate per zone* — rejected because the aggregation must run inside the status-table PromQL predicate, and predicates that mix `count by (zone)` over potentially-empty sets are brittle (need `or on() vector(0)` chains that obscure intent).

---

## R-3: Where does the classifier fit in the prober pipeline?

**Decision**: New `prober.ProbeNSClassification` function, registered via `RegisterProber("ns_classification", ProbeNSClassification)`. Like all other probers, it receives `(ctx, zone, nameservers, delegation, client, logger)`. Unlike most probers, it issues ZERO DNS queries — it reads `delegation.NSRecords` (parent side) and re-queries each nameserver for the zone's own NS RR set to compute the self side.

Wait — the glue prober already does that self-side query and emits `dnshealth_ns_record{source="self"}`. Can the classifier just consume the parent / self data from the same delegation walk + a fresh per-NS NS RR set query?

**Final decision after re-reading `prober/glue.go`**: The classifier MUST issue its own per-nameserver NS-RR-set query rather than depending on the glue prober's metric output. Two reasons:

1. Probers are isolated by design (`cycle.runner.go` runs them in arbitrary order via map iteration); the classifier cannot assume the glue prober has run first this cycle.
2. The glue prober's internal data structures (`selfNS` list per NS) aren't exposed across prober boundaries — only the resulting `ProbeResult`s are.

So the classifier issues its own NS RR set query per parent-listed nameserver, builds the self-set as the union of all responses, and compares against `delegation.NSRecords`. Cost: 1 extra DNS query per nameserver per cycle. Acceptable — matches the cost the glue prober already pays.

**Rationale**: Isolation between probers is a Constitution-VI consistency property (each prober is a self-contained unit). Duplicating the self-side query is a small price for that isolation.

**Alternatives considered**:

- *Share self-side data between glue and classification probers via a side-channel struct* — rejected for breaking the prober isolation property and adding coupling that would obstruct future reorganization.
- *Move the classifier inside the glue prober* — rejected because glue already does too much; a separate prober keeps each one focused and dashboards-friendly (each has its own `query_success` / `query_duration_seconds` per-check series).

---

## R-4: Multi-auth union semantics for the self set

**Decision**: The self set per zone is the UNION of all NS hostnames reported by any auth-side response, with all hostnames canonicalized (`dns.Fqdn` + `strings.ToLower`). A nameserver appears in the self set if ANY reachable auth lists it; unanimity is NOT required.

**Rationale**: Per FR-007 and the Edge Cases section of the spec, requiring all auths to agree would mask the most operationally-relevant case (hidden master listed by one auth's NS RR set view but not another's). The "every auth agrees" property is a separate quality check already covered by row D from issue #36.

**Alternatives considered**:

- *Intersection* — rejected; would miss exactly the case we want to detect.
- *Majority* — rejected; arbitrary threshold for no real benefit, doesn't match how DNS resolvers actually behave (they use any working NS, not majority-voted).

---

## R-5: Classification when the parent set is empty

**Decision**: When the parent set is empty (e.g., delegation walk failed and `delegation.NSRecords` is empty), the classifier emits no per-NS classification series for that zone but DOES emit `dnshealth_ns_classification_count{classification=...}` gauges with `Set(0)`. The status-table row reads FAIL with detail text pointing at the parent-delegation row.

**Rationale**: Without a parent reference set, "asymmetry" is undefined — every NS in the self set could be classified `self-only` but that conflates "no parent data" with "real divergence". Better to emit zero count and let the status row's failure semantics signal the deeper problem.

---

## R-6: Demo-zone topology for the self-only case

**Decision**: New zone `hidden-master.demo.` on a new container at `172.31.0.21`. Parent (root) advertises `[ns1.hidden-master.demo., ns2.hidden-master.demo.]` with glue at `172.31.0.21`. The auth container at `172.31.0.21` serves a zone file whose NS RR set is `[ns1.hidden-master.demo., ns2.hidden-master.demo., hidden-primary.hidden-master.demo.]` — the third one being the stealth NS (the hidden master). No A record for `hidden-primary.hidden-master.demo.` is shipped from anywhere reachable — it's a phantom NS for detection-purposes only; this is exactly the misconfigured "leaked hidden master" pattern.

The classifier's self-side query to either ns1 or ns2 returns three NS records. Set diff against parent ([ns1, ns2]) yields `[hidden-primary]` as self-only. Smoke asserts:

- `dnshealth_ns_classification{zone="hidden-master.demo.",nameserver="hidden-primary.hidden-master.demo.",classification="self-only"} = 1`
- `dnshealth_ns_classification_count{zone="hidden-master.demo.",classification="self-only"} = 1`

**Rationale**: Mirrors the existing `ns-mismatch.demo.` (count divergence) and `ns-names-mismatch.demo.` (names divergence, equal counts) pattern. Adding a third zone for the self-only / hidden-master case completes the operationally-meaningful divergence taxonomy. Self-only is the highest-value case per the spec's P1 user story.

Also the spec's User Story 2 asks for context distinguishing hidden master vs forgotten — represented here by the lack of an A record for `hidden-primary.hidden-master.demo.` (it's a "leaked listing" case, not a working hidden master). The label data is sufficient for the dashboard's detail-text guidance to apply; the operator can correlate against whether the NS hostname appears in other check metrics (SOA query_success for the same name would be missing, indicating leaked rather than working).

**Alternatives considered**:

- *Two demo zones (one working hidden master, one leaked)* — rejected for this PR's scope; the integration test covers both cases. Defer if visual demonstration becomes desirable.
- *Reuse `ns-mismatch.demo.` since it already has self/parent NS divergence* — rejected because that zone exercises count divergence (parent=1, self=2 with completely different names); the spec's P1 case is "same plus extra" which is distinct enough operationally to warrant its own demo.

---

## R-7: Status-table row design

**Decision**: New status row `E` (in `nsStatusChecks` slice in `demo/dashboard/panels_status.go`):

- Label: `"No stealth NSes (parent and self agree on NS set)"`
- PromQL: `(max by (zone) (dnshealth_ns_classification_count{classification="self-only",zone="$zone"}) == bool 0) and on(zone) (max by (zone) (dnshealth_ns_classification_count{classification="parent-only",zone="$zone"}) == bool 0) or on() vector(0)`
- Detail text discloses:
  - The metric (`dnshealth_ns_classification` / `_count`)
  - That FAIL can be a legitimate hidden-master config (per spec User Story 2 / Edge Cases)
  - That RFC-strict stealth (servers unknown to every public source) is NOT covered by this check — see spec definition section
  - Where to look next (NS records side-by-side tables to identify the asymmetric NS)

**Rationale**: Folds into the existing NS-status table grouping (matches the established pattern of all NS-side checks landing in the NS panel). The detail-text honesty about RFC-strict stealth being out of scope is explicitly required by the spec's definition section.

---

## R-8: Metric naming

**Decision**:

- `dnshealth_ns_classification{zone, nameserver, classification}` — info gauge, value = 1, one series per (zone, NS hostname) per cycle. `classification` label is one of `parent-only`, `self-only`, `both`.
- `dnshealth_ns_classification_count{zone, classification}` — gauge, count of NS hostnames in that classification for the zone. `Reset()` + explicit `Set(0)` per zone per cycle.

**Rationale**: `_classification` describes what the metric carries. The `_count` suffix follows the existing convention used by per-zone aggregation gauges in the codebase. Both names start with `dnshealth_ns_` matching the existing `dnshealth_ns_record`, `dnshealth_ns_hostname_*`, `dnshealth_ns_recursion_available` family.

**Alternatives considered**:

- `dnshealth_ns_asymmetry` — more accurate but breaks the "stealth" naming continuity called out in the spec definition section. Operators searching for "stealth" in their metric explorer would miss it.
- `dnshealth_stealth_ns` — too narrow; the classifier surfaces both `parent-only` and `self-only`, not just stealth.

---

## R-9: Active reachability probe of self-only stealth NSes (post-analyze remediation for FR-010)

**Decision**: After classification, for each NS classified as `self-only`, the classifier resolves the hostname via the existing `ResolveHostnames` helper and issues an SOA query against the resolved IPs (one query per resolved IP, short-circuit on first authoritative response). Emit a boolean gauge `dnshealth_ns_stealth_reachable{zone, nameserver}` — 1 if any resolved IP returned an authoritative SOA response for the zone, 0 if resolution failed or no IP returned a valid SOA.

The probe runs ONLY for self-only NSes (the `parent-only` and `both` cases have other diagnostic surfaces already — parent-only is "registrar config drift" not "is the server real", and `both` NSes already get hit by the SOA prober via the normal pipeline).

**Rationale**: FR-010 promises the operator can distinguish a working hidden master from a leaked / forgotten listing from the dashboard alone. Without this active probe, the existing `dnshealth_query_success{check="soa"}` series would NEVER contain a row for the stealth NS (the SOA prober only iterates parent-listed NSes), so the disambiguation guidance in the row G detail text would point at data that doesn't exist. Speckit-analyze flagged this as finding C1 (HIGH); option A was chosen.

**Cost**: Per cycle, per self-only stealth NS: one `ResolveHostnames` call (typically 1-2 DNS queries) plus 1-2 SOA queries against the resolved IPs. Bounded by the number of stealth NSes the operator actually has — typically zero, occasionally one or two for hidden-master setups. Negligible additional load.

**Failure semantics**:

- Hostname doesn't resolve → reachable = 0 (matches the "leaked listing" pattern)
- Resolves but SOA query times out / errors → reachable = 0
- Resolves and SOA returns NXDOMAIN / NOTAUTH / SERVFAIL → reachable = 0
- Resolves and SOA returns valid authoritative response for the zone → reachable = 1

**Alternatives considered**:

- *Probe every classification value (parent-only, self-only, both) for reachability* — rejected as scope creep. `both` NSes are already covered by the SOA prober; `parent-only` reachability is a different operator question (registrar drift, addressed by row D / #36 + parent_delegation). Self-only is the only case where reachability is the discriminating signal.
- *Emit reachability as a label on `dnshealth_ns_classification` instead of a separate metric* — rejected because the value is only meaningful for `self-only` and would be empty / 0 for the other classifications, polluting the metric. Cleaner to have a focused boolean.
- *Defer to a follow-up issue* — rejected because the row G detail text PROMISES this disambiguation; shipping the feature without it would mean the documented workflow doesn't actually work. Better to land them together.
