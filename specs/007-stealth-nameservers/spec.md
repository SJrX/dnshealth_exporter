# Feature Specification: Stealth Nameserver Detection

**Feature Branch**: `007-stealth-nameservers`
**Created**: 2026-05-23
**Status**: Draft
**Input**: User description: "N7 — detect stealth nameservers per proposals doc"

## What "stealth" means in this feature

DNS terminology (RFC 8499) defines a **stealth server** as a server holding zone data but not present in the public NS RR set — classic examples are hidden masters that NOTIFY-listen for zone changes without being publicly listed, and forgotten secondaries that still answer for a zone after being removed from the public delegation. By that strict definition, detecting a stealth server requires out-of-band signals (passive DNS feeds, certificate-transparency log mining, internal DNS-server inventories) that an exporter walking the public DNS tree cannot obtain.

This feature surfaces the **detectable approximation**: NS hostnames that appear in one source the exporter can query (the parent's referral) but not the other (an auth's self-reported NS RR set), or vice versa. That catches the subset of RFC-stealth setups where at least one publicly-queryable source happens to list the otherwise-hidden server — typically a hidden master that one of the secondaries is configured to track, or a forgotten secondary that one auth still knows about and reports. It does **not** catch genuinely zero-knowledge stealth servers; those are unreachable to any single-vantage-point exporter.

Where the term "stealth" appears in this spec (in success criteria, requirements, dashboard labels, metric names), treat it as shorthand for this asymmetry-based approximation. The dashboard's row detail-text must make the same disclosure to the operator so they aren't misled into thinking a PASS guarantees no hidden infrastructure exists.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Surface NS-set divergence between parent and zone (Priority: P1)

A zone operator monitors their zones with this exporter and wants to be told when the parent's published NS RR set doesn't match what the authoritative servers themselves report — specifically the case where an authoritative server reports an NS hostname that the parent doesn't know about. This often indicates: a hidden master that escaped a config rotation, a leftover server from a previous DNS provider, or a misconfigured split-DNS leak.

**Why this priority**: This is the case that is genuinely detectable from probe data and represents real operational risk. Without it, an operator can compare the existing per-NS-record tables side-by-side but has to do the set-difference mentally for every zone, every cycle. A direct gauge makes the discrepancy alertable.

**Independent Test**: Deploy a demo zone where the parent advertises `[ns-public-a, ns-public-b]` but the auth reports `[ns-public-a, ns-public-b, ns-hidden-c]` in its own NS RR set. Verify the exporter surfaces `ns-hidden-c` as a stealth-classified NS in metrics and that the corresponding dashboard row reads FAIL.

**Acceptance Scenarios**:

1. **Given** a zone whose parent advertises NSes `[A, B]` and whose authoritative servers all report `[A, B, C]` for the zone's NS RR set, **When** a probe cycle completes, **Then** the exporter exposes a metric series indicating `C` is "self-only" (a stealth NS from the parent's perspective).
2. **Given** the same zone, **When** an operator opens the dashboard, **Then** a status-table row reads FAIL and the row's investigation pointer leads them to the per-NS table where `C` is visible only on the self side.
3. **Given** a zone where parent and self report identical NS sets, **When** a probe cycle completes, **Then** the stealth-detection row reads PASS and no stealth NSes are listed.

---

### User Story 2 - Distinguish hidden masters from forgotten servers (Priority: P2)

The operator wants context on each stealth NS so they can quickly decide whether it is a legitimate hidden-master configuration (NOTIFY-driven primary not in public NS set — by design) or a forgotten / leaked server (operational hazard). The exporter surfaces enough metadata per stealth NS for the operator to make this judgement at a glance, rather than treating every divergence as an alert.

**Why this priority**: Hidden-master setups are common and legitimate. Without this distinction, P1's row would generate noise for operators who use a hidden-master pattern. P2 makes the detection useful in practice without overloading the alerting surface.

**Independent Test**: Add a demo zone whose self-set includes an NS hostname designated `hidden-master.*` and verify the exporter exposes its detected role (or at least surfaces enough labels for an operator to identify it). A separate demo zone with a "forgotten" stealth NS should appear with different labels / context.

**Acceptance Scenarios**:

1. **Given** a stealth NS detected on a zone, **When** the operator queries metrics by NS hostname, **Then** the exporter exposes the NS's authoritative-response characteristics (e.g., whether the server actually responds authoritatively when queried directly) so the operator can correlate against known hidden-master patterns.
2. **Given** a stealth NS that is in fact unreachable / non-responsive, **When** a probe cycle completes, **Then** the exporter surfaces that the NS hostname appears in the self-set NS list but is not itself reachable — a "leaked listing" pattern distinct from a working hidden master.

---

### User Story 3 - Detect parent-only NSes (the symmetric divergence) (Priority: P3)

The mirror case of P1: the parent advertises an NS hostname that none of the authoritative servers report as their own. This typically means a misdelegation — the zone owner removed an NS from their own configuration but didn't update the registrar. Resolvers seeded with the parent's view will keep trying to query a server that doesn't know about the zone.

**Why this priority**: Less common than the self-only case (registrars push parent-side changes; the auth-side is harder to update silently), but still operationally meaningful when it happens. Symmetric design with P1.

**Independent Test**: Demo zone where parent advertises `[A, B, C]` and the auth's self-report is `[A, B]`. Verify `C` is surfaced as "parent-only".

**Acceptance Scenarios**:

1. **Given** a zone whose parent advertises an NS hostname `X` and whose authoritative servers do not include `X` in their own NS RR set, **When** a probe cycle completes, **Then** the exporter exposes `X` as a "parent-only" NS with appropriate per-NS labels.

---

### Edge Cases

- **Hidden master pattern (legitimate)**: An NS appears in self-set but never in parent-set; this is by-design for hidden-master setups. The detection MUST surface the fact, but the alert framing (in the dashboard's row detail-text) MUST acknowledge that a 0/FAIL value here is sometimes intentional — same posture as the SOA-MNAME-in-NS-set row from spec 006.
- **All authoritative servers unreachable**: If the exporter cannot reach any auth-side server, no self-set data exists this cycle. The stealth-detection row MUST treat "no data" distinctly from "no divergence" — likely reading FAIL or absent rather than PASS, to avoid masking a probe outage as a healthy state.
- **NS set drift between auth servers**: Two auth servers may each report a different self-set. The exporter MUST classify against the UNION of self-set views (any NS reported by at least one auth is considered "in self set"), not require unanimity. Disagreement between auths is a separate quality concern (already partially covered by row D from issue #36).
- **Case-insensitive comparison**: DNS names are case-insensitive (RFC 4343). NS comparisons MUST be case-insensitive; `NS1.example.com.` and `ns1.example.com.` are the same NS.
- **Trailing-dot normalization**: All NS comparisons operate on the canonical FQDN form (trailing dot present).
- **Zones with zero auth-side data**: Zones whose delegation walk failed or whose auths all timed out have no self-set data. The classification MUST handle this without producing false stealth alerts.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST compute, for each probed zone, the set of distinct NS hostnames reported by self-side queries (`dnshealth_ns_record{source="self"}`) and the set of distinct NS hostnames advertised by the parent (`dnshealth_ns_record{source="parent"}`).
- **FR-002**: System MUST identify NS hostnames that appear in the self set but not in the parent set ("stealth" or "self-only" candidates).
- **FR-003**: System MUST identify NS hostnames that appear in the parent set but not in the self set ("parent-only" candidates) for symmetric visibility.
- **FR-004**: System MUST expose per-NS classification via metric labels so operators can list, alert on, and dashboard-filter individual stealth NSes — not just an aggregate count.
- **FR-005**: System MUST expose per-zone count gauges (e.g., number of stealth NSes detected) suitable for status-table aggregation and alert thresholds.
- **FR-006**: System MUST perform NS name comparison case-insensitively and on canonical FQDN form (trailing dot).
- **FR-007**: System MUST classify against the UNION of self-set views when multiple authoritative servers disagree (any NS reported by at least one auth is in the self set).
- **FR-008**: System MUST distinguish "no divergence detected" from "no data available this cycle" so that a probe outage does not mask as a healthy state.
- **FR-009**: System MUST add a dashboard status-table row surfacing the detection (per the `feedback-metric-needs-dashboard` rule and the `TestStatusChecksHaveDetail` guard test), with detail text that acknowledges hidden-master setups as a legitimate 0/FAIL case.
- **FR-010**: System MUST actively probe each detected self-only stealth NS for SOA authoritativeness (by resolving its hostname out-of-band and issuing a SOA query against the resolved IPs) and expose the result as a per-(zone, nameserver) reachability gauge. This is the data backing the dashboard's "working hidden master vs leaked listing" disambiguation in the row G detail text — without an active probe, the disambiguation guidance cannot be followed because the existing SOA prober only queries parent-listed NSes.

### Key Entities

- **Stealth NS classification**: A per-(zone, nameserver) classification with values like `parent-only`, `self-only`, `both` (i.e., not stealth). Each NS hostname observed in either source for a zone receives exactly one classification per probe cycle.
- **Zone-level stealth count**: A per-zone count of NS hostnames currently classified as `self-only` (the operationally interesting one) and a separate count for `parent-only`.
- **Self-set view source**: For multi-auth zones, each auth produces its own view of the NS RR set. The detection collapses these via union before comparing against the parent.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Operators can identify every stealth NS for a zone via a single metric query against the new classification series, without needing to cross-reference parent and self tables manually.
- **SC-002**: A demo zone deliberately configured as "parent says [A, B], self says [A, B, C]" surfaces `C` as a stealth NS within one probe cycle of the exporter's first scrape of that zone.
- **SC-003**: A demo zone configured normally (parent and self agree) surfaces zero stealth NSes; the new dashboard row reads PASS for that zone.
- **SC-004**: The new dashboard status-table row distinguishes "no divergence" (PASS) from "stealth NS present" (FAIL) and from "no data this cycle" (also FAIL, but visually distinct from a working zone — covered by row's detail text pointing to the parent-delegation row when applicable).
- **SC-005**: For zones where a stealth NS exists and is genuinely a hidden master (responds authoritatively), the dashboard surfaces — without leaving the panel and without external docs — enough per-NS information for the operator to classify it as such: at minimum, the NS hostname, an indicator of whether it answered an authoritative probe this cycle, and a note in the row's detail text that hidden-master patterns are a legitimate cause of this row's FAIL state.
- **SC-006**: The integration test suite includes at least one fixture exercising each of: (a) clean state (no stealth), (b) self-only stealth NS, (c) parent-only NS, (d) multi-auth disagreement converging via union. Each case asserts the expected classification metric value.
- **SC-007**: The demo deployment includes at least one zone exercising the self-only stealth case end-to-end, with a smoke assertion that the classification metric is present and reads the expected value.

## Assumptions

- The detection uses ONLY data the exporter already gathers (parent NS records from delegation walk; self NS records from the glue prober's self-side queries). No new query types or active scanning are introduced — the existing data is sufficient for the achievable approximation described in the "What 'stealth' means in this feature" section above.
- RFC-strict stealth detection (servers absent from every public source) is out of scope — see definition section. The dashboard row detail text must make that limitation visible to the operator.
- The classification is per-NS-hostname, not per-(NS, IP). IP-level disagreement between parent glue and auth-side IPs is a separate concern tracked under issue #37.
- Hidden-master setups are legitimate and common. The feature MUST surface stealth NSes but the dashboard framing MUST treat a FAIL on this row as "worth a look" rather than "definite incident", consistent with the SOA-MNAME-in-NS-set row from spec 006.
- Existing per-NS metric series (`dnshealth_ns_record`, `dnshealth_query_success`, etc.) remain unchanged. New series are additive; no schema change to existing series.
- The dashboard status-table row follows the existing PASS/FAIL color-background pattern and inherits the `feedback-metric-needs-dashboard` requirement: every new row ships with `detail` text passing the `TestStatusChecksHaveDetail` guard test.
- The demo zone(s) added for this feature follow the existing pattern (dedicated CoreDNS container at a fresh IP, zone file under `demo/coredns/<name>/`, delegation entry in the root zone, smoke assertion).
- Probe-cycle latency budget is not materially affected — the new computation operates on data already in memory at the end of each cycle; no new DNS queries are issued.
