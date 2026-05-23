# Research: MX Prober Family (Spec 008)

Phase 0 output of `/speckit-plan`. The spec had no `[NEEDS CLARIFICATION]` markers — all open questions were resolved upfront via the "Scope of this feature" section and the Assumptions block. Research below records the technical-design decisions that drive the Phase 1 contracts and Phase 2 task breakdown.

---

## R-1: TypeMX query semantics — single auth or fan-out?

**Decision**: Issue ONE TypeMX query per cycle per zone, against the FIRST reachable parent-listed nameserver. Use the first authoritative response; do not aggregate across auths.

**Rationale**:

- MX records are zone data, served identically by every authoritative server (RFC 1035). Querying every auth would produce N redundant copies of the same data.
- The existing `glue` prober already cross-checks auth-side disagreement on NS records (and spec 007 surfaces NS-set asymmetry). MX-record disagreement between auths IS theoretically possible (mid-replication, BIND zone-transfer drift) but is rare and a different concern — could be a follow-up issue, not in this spec.
- Single-query keeps the per-zone DNS cost low (~5-10 queries per cycle including downstream resolution / CNAME / syntax checks per target).

**Alternatives considered**:

- *Fan-out across all auths, classify by union* — rejected for the redundancy reason above plus the cost reason. The spec's Assumptions note acknowledges this is theoretically possible but defers it.
- *Use the parent-side referral's Authority/Additional sections* — rejected because parent referrals don't carry MX records (only NS + glue). Must query an authoritative server directly.

---

## R-2: Null MX detection — at parse time or with a separate query?

**Decision**: Null MX detection is a parse-time property of the TypeMX response. A response is a Null MX iff it contains exactly one MX RR with `Preference == 0` and `Mx == "."`. No separate query.

**Rationale**:

- RFC 7505 §3 specifies the canonical form: "if any of the MX RRs have an Mx hostname of '.', then there MUST be only one MX RR in the set, and it MUST have a Preference of 0." So the detection is purely about parsing the single TypeMX response.
- A separate gauge (`dnshealth_mx_null_mx`) exposed per zone gives operators an explicit signal — distinct from "no MX records at all" (which is itself a parseable property, just `count == 0`).
- The "Null MX coexists with real MX records" configuration error from US2 acceptance scenario #3 is detected by counting MX records and checking the Null-MX flag separately: if `null_mx == 1 && total_mx_count > 1`, the configuration is malformed.

**Alternatives considered**:

- *Treat Null MX as just `total_mx_count == 1 && exchange == "."` without a dedicated gauge* — rejected because operators want to filter / alert on Null MX explicitly without writing the predicate. A dedicated gauge is cleaner.
- *Suppress all per-MX series for Null-MX zones* — rejected because the per-MX info gauge for the `0 .` record itself IS informationally useful (operator can see WHICH zones have explicit Null MX without writing a join query).

---

## R-3: Primary/backup classification — label on info gauge or separate boolean gauge?

**Decision**: Both. Per-MX info gauge carries the priority as a label value (informational, allows PromQL filtering / sorting); a dedicated `dnshealth_mx_is_primary` boolean gauge per (zone, target) makes the classification trivially queryable without needing a `min by (zone)` PromQL chain in every alert rule.

**Rationale**:

- The priority label is already on the info gauge for free — no extra emission cost, and operators querying `dnshealth_mx_info{zone="X"}` want to see priorities natively.
- The is-primary boolean is derived per cycle: for each zone, find the minimum priority across all MX records; any MX whose priority equals that minimum gets `is_primary=1`, others get 0. This is computed by the cycle runner's aggregation pass, similar to how `dnshealth_ns_classification_count` is derived in spec 007.
- Multi-MX-tied-for-primary case (US3 acceptance scenario #2) is naturally handled: every MX at the minimum priority gets `is_primary=1`. No "lottery" or "first wins" semantics.

**Alternatives considered**:

- *Only the priority label, let operators derive primary via PromQL `min by (zone)`* — rejected because the predicate `(dnshealth_mx_priority == on(zone) group_left min by (zone) (dnshealth_mx_priority))` is awkward and would need to live in every alert rule. A dedicated boolean reduces operator cognitive load.
- *A separate enum label "role={primary,backup}"* — rejected because changing the label set per cycle (as priorities change) would create transient series. A boolean stays put (always 1 or 0 per existing (zone, target) tuple).

---

## R-4: CNAME check per MX target — reuse spec N6's `lookupCNAME`?

**Decision**: Yes — reuse `lookupCNAME` from `prober/ns_hostname.go` unchanged. Per-MX cache the result by hostname within a single cycle so duplicate MX targets (rare but possible) only cost one lookup.

**Rationale**:

- The CNAME check semantics are identical to spec N2's NS-hostname check: "does this hostname have a CNAME RR owning it?" — a yes/no question answered by walking the delegation chain and asking for TypeCNAME at the authoritative server for the target's parent zone.
- Reuse over duplication keeps the code surface small and tied to a single canonical implementation. If `lookupCNAME` ever gains optimizations (caching across cycles, etc.), the MX prober benefits automatically.
- Per-cycle per-hostname caching mirrors the `mnameResolves` cache in `prober/soa.go` (spec 006 / S1) — same pattern, well-understood.

**Alternatives considered**:

- *Promote `lookupCNAME` to a public package-level helper* — currently it's lowercase / package-private to `prober`. The MX prober is in the same package so no visibility change needed. Refactor to public-export only if future external probers need it.

---

## R-5: Per-MX syntax check — reuse spec N6's `isValidNSHostname`?

**Decision**: Yes — reuse unchanged. The LDH validation rules from RFC 952 / 1123 apply identically to MX exchange names per RFC 5321 §2.3.5.

**Rationale**:

- Same predicate, different label name in the metric. The helper takes a string and returns bool — no semantic difference between "is this a valid NS hostname" and "is this a valid MX target hostname" at the syntax layer.
- Future RFC-compliance changes (e.g., if the project ever decides to allow underscores per actual zone-file practice) would be made in one place.

**Alternatives considered**:

- *Duplicate the helper as `isValidMXTargetHostname`* — rejected for the obvious reason. No semantic divergence justifies duplication.

---

## R-6: Resolution check — A vs AAAA semantics, and what counts as "resolved"?

**Decision**: An MX target is "resolved" if `ResolveHostnames(target)` returns a non-empty slice. The existing helper already attempts both A and AAAA per family. Either family non-empty → resolved.

**Rationale**:

- Some MX targets are AAAA-only (modern email infra moving to v6). Some are A-only. Either is acceptable; the SMTP MTA picks the family it can speak.
- The spec's Edge Case for "MX target resolves only to AAAA (no A)" already declared this out of scope as a special concern (legacy-MTA compat). Treating either family as "resolved" matches the spec's intent.
- Reusing `ResolveHostnames` keeps the resolution semantics consistent with NS-hostname resolution from existing probers — same dual-walk pattern, same out-of-band glueless-referral fallback from spec #27.

**Alternatives considered**:

- *Emit separate A-resolves and AAAA-resolves gauges* — rejected as scope creep. The spec's User Story 1 talks about "resolves to deliverable IPs" without per-family distinction. If operators need per-family granularity later, it's a clean follow-up.
- *Require BOTH A and AAAA (modern best practice)* — rejected because it's stricter than the spec called for and would generate FAIL on perfectly-functional A-only or AAAA-only setups.

---

## R-7: Dashboard placement — extend NS panel, new MX panel, or sub-panel under an "Email" header?

**Decision**: New dedicated "MX — status" panel placed next to (or below) the existing NS / SOA / Parent panels in the status row. Plus a new per-MX records table in the records row.

**Rationale**:

- MX is a distinct concern from NS / SOA / Parent. Folding rows into the NS panel would conflate "is the zone reachable via DNS" with "can the zone receive email" — operationally different alarms.
- The dashboard's existing layout has three status panels side-by-side (Parent / NS / SOA, each 8 grid units wide). Adding MX makes 4 side-by-side at 6 units each is too narrow for the existing row content. Better: stack MX status panel below the existing status row, possibly alongside a future "Email-auth status" panel from issue #44.
- The per-MX records table (sort by priority, show resolution status / CNAME status / primary-or-backup) lives in the records-row alongside the NS / SOA tables. Width: 8 grid units (matches existing tables).

**Alternatives considered**:

- *One panel "Email — status" combining MX, SPF, DMARC, DKIM* — rejected because SPF / DMARC / DKIM are deferred to issue #44. Designing the panel for a feature that doesn't exist yet would either leave empty rows or require restructuring when #44 lands. Defer; design later for #44.
- *Drop the per-MX records table; only ship status rows* — rejected because the User Story 1 acceptance scenarios explicitly call for "operator can identify the offending hostname from the metric labels" and "the operator opens the dashboard ... per-MX table presents them ordered by priority". A status-only dashboard wouldn't deliver US3 at all.

---

## R-8: Demo zones — how many, and which states?

**Decision**: Three new demo containers, one zone each:

- `mx-healthy.demo.` at `172.31.0.22` — multi-MX zone with two healthy MXes at priorities 10 + 20. Demonstrates US1 happy path + US3 primary/backup classification.
- `mx-null.demo.` at `172.31.0.23` — Null MX zone (`0 .`). Demonstrates US2 Null MX detection.
- `mx-broken.demo.` at `172.31.0.24` — multi-MX zone where one target is a CNAME (RFC 2181 §10.3 violation) and another target is unresolvable. Demonstrates US1 failure paths.

**Rationale**:

- Three zones cover all four user stories' happy and failure paths. Coverage matches SC-007's "at least three new zones exercising distinct MX states."
- The "Null MX coexists with real MX records" configuration-error case (US2 acceptance #3) is left to integration tests, not a demo zone — CoreDNS would either reject the conflicting record at zone-parse time or serve both with potentially-unpredictable semantics. Reproducing the error in DNS fixture code is straightforward; reproducing it in a CoreDNS-served demo zone is uncertain. Defer to integration test coverage.
- The "no MXes AND no Null MX" case is also integration-test-only — the demo's existing zones (e.g., `healthy.demo.`) already have no MX records by default, so the failure mode is implicitly covered just by enabling MX probing for every demo zone.

**Alternatives considered**:

- *Four demo zones (add a "no MX at all" zone)* — rejected because the existing zones already exercise this case as a side effect of being added to the MX prober's iteration.
- *One demo zone with all the states* — rejected because zone parsing forbids contradictory states (Null MX + real MX, etc.) and because the dashboard's per-zone view works best when each interesting case is isolatable via the zone selector variable.

---

## R-9: Probing scope — every configured zone, or only zones that opt into MX probing?

**Decision**: Every configured zone, unconditionally. No per-zone opt-in flag.

**Rationale**:

- Adding an opt-in flag complicates the config schema (a single boolean is fine, but it's the start of a slippery slope to per-check-type flags everywhere).
- The cost of probing MX for a zone with no MX records is one TypeMX query per cycle → fast, harmless. The "no MXes and no Null MX" case correctly fails the MX-presence row, which is the desired behavior for monitored zones the operator hasn't yet explicitly opted out of email via Null MX.
- Operators who genuinely want a zone monitored for DNS but NOT for MX can either (a) publish Null MX on the zone (correct DNS practice), or (b) accept the FAIL on the MX-presence row and silence it in their alerting rules.

**Alternatives considered**:

- *Per-zone `check_mx: false` config flag* — rejected as scope creep. If demand surfaces, can be added later cheaply (the prober already exists; just gate it in `cycle.Runner.probeZone`).

---

## R-10: Metric naming family

**Decision**:

- `dnshealth_mx_info{zone, target, priority}` — info gauge per MX record, value always 1; `priority` carried as a label (string-formatted integer).
- `dnshealth_mx_resolves{zone, target}` — boolean per MX target (1 if at least one A or AAAA resolves).
- `dnshealth_mx_is_cname{zone, target}` — boolean per MX target (1 if target is a CNAME — RFC 2181 §10.3 violation).
- `dnshealth_mx_syntax_valid{zone, target}` — boolean per MX target (1 if LDH-valid).
- `dnshealth_mx_is_primary{zone, target}` — boolean per MX target (1 if priority equals zone's minimum).
- `dnshealth_mx_null_mx{zone}` — per-zone boolean (1 if zone publishes exactly Null MX per RFC 7505).
- `dnshealth_mx_count{zone}` — per-zone count of MX records (including Null MX if present).
- `dnshealth_mx_resolved_count{zone}` — per-zone count of MX targets that resolve.
- `dnshealth_mx_cname_count{zone}` — per-zone count of MX targets that are CNAMEs.

**Rationale**:

- All names start with `dnshealth_mx_` for ecosystem-consistent prefixing. Booleans use `_is_*` or single-word names for the predicate. Counts use `_count` suffix.
- The per-zone count gauges are emitted via the cycle runner's aggregation loop (mirrors `dnshealth_ns_classification_count` from spec 007) — Reset+Set(0) per cycle per zone so PromQL can distinguish no-data from no-MX.

**Alternatives considered**:

- *Single `dnshealth_mx_validity{...,classification="resolves|cname|syntax"}` metric* — rejected because it muddles independent boolean predicates into one series, making PromQL filter expressions harder to read and breaking the established per-check-gauge pattern from `ns_hostname` (which has separate `_syntax_valid` and `_is_cname` gauges, not a unified one).
