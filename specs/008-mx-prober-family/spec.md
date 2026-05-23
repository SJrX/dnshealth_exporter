# Feature Specification: MX Prober Family

**Feature Branch**: `008-mx-prober-family`
**Created**: 2026-05-23
**Status**: Draft
**Input**: User description: "E2 — MX prober family per proposals doc: probe MX records for each zone, check resolution + reachability of MX hosts, surface Null MX (RFC 7505), distinguish primary vs backup MXes"

## Scope of this feature

Adds **DNS-level** health checks for the MX records of each configured zone. Specifically: presence of MX records, resolution of each MX target hostname, RFC-correctness checks (no CNAMEs at MX targets per RFC 2181 §10.3, syntactic validity per RFC 5321), Null MX detection per RFC 7505, and a primary-vs-backup classification per MX priority.

**Explicitly out of scope** for this feature (clearly enumerated so the spec scope stays tight):

- **SMTP-protocol probing** (connect to port 25, banner check, STARTTLS / TLS cert validation). The exporter is DNS-focused; SMTP-level health belongs in a separate exporter such as `blackbox_exporter` configured with SMTP probes.
- **Email-authentication TXT records** (SPF / DMARC / DKIM / BIMI / MTA-STS / TLSA / DANE). These are TXT-record checks at well-known names, structurally and parsing-wise different from MX-record checks. Tracked as a follow-up in [#44](https://github.com/SJrX/dnshealth_exporter/issues/44).
- **Open-relay detection**, **mail-flow load testing**, **reverse-DNS PTR checks for MX hostnames** (PTR is a separate concern, tracked under issue #37's broader umbrella).
- **Private-IP / RFC 1918 detection** on MX targets — flag-worthy but scope-creeps the predicate set; defer until operator demand surfaces.

The feature stays inside the DNS data plane the exporter already operates on; no new transport, no new query types beyond TypeMX + the existing TypeA / TypeAAAA / TypeCNAME helpers used elsewhere.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See every MX record per zone with resolution + RFC-validity flags (Priority: P1) 🎯 MVP

A zone operator wants to know, for each zone they monitor, what MX records the zone publishes, which MX targets actually resolve to deliverable IPs, and whether any MX target violates RFC 2181 by being a CNAME. This is the core email-deliverability sanity check — every other email-failure mode this feature covers is a refinement of this one.

**Why this priority**: This is the single most operationally-valuable signal. A zone whose MX records point at unresolvable hostnames or at CNAMEs silently breaks inbound email until an end-user complains. Catching it at probe time gives the operator hours-to-days lead time instead of waiting for support tickets. Every other story in this spec is a refinement; this one is the MVP that delivers the bulk of the operator value.

**Independent Test**: Deploy a demo zone with three MX records — one healthy, one with an unresolvable target, one whose target is a CNAME. Verify the exporter surfaces per-MX series with the right per-target validity flags and that the dashboard rows summarize the zone as FAIL with detail text pointing at the specific offending targets.

**Acceptance Scenarios**:

1. **Given** a zone with MX records `[10 mail-a.example.com., 20 mail-b.example.com.]` where both targets resolve to A records, **When** a probe cycle completes, **Then** the exporter exposes per-MX series carrying the priority (10 / 20) and the exchange hostname, plus a per-MX resolution gauge reading 1 for both targets.
2. **Given** a zone with MX records where one target is a CNAME to another host, **When** a probe cycle completes, **Then** the exporter exposes a per-MX is-CNAME gauge reading 1 for the offending target and the status-table row reads FAIL.
3. **Given** a zone with MX records where one target has no A or AAAA record anywhere reachable, **When** a probe cycle completes, **Then** the per-MX resolution gauge reads 0 for that target and the operator can identify the offending hostname from the metric labels.
4. **Given** a healthy zone with all MX targets resolved, valid, and non-CNAME, **When** the operator opens the dashboard, **Then** the MX status-table rows all read PASS for that zone.

---

### User Story 2 - Recognize Null MX (RFC 7505) as intentional opt-out (Priority: P2)

Some zones explicitly declare "this domain does not accept email" via a Null MX record: a single MX with priority 0 and exchange `.` (the root label). The exporter must recognize this and treat absence-of-other-MX-records as the intended state, not as a failure. Without this, every zone the operator intentionally configures for "no email" would generate a permanent FAIL on the MX-presence check — alert noise the operator would have to suppress per-zone.

**Why this priority**: Lower than US1 because most operator-monitored zones DO accept email — but a single FAIL-noise-per-no-email-zone scales painfully if the operator monitors many such zones. Adding Null MX detection costs little and prevents a class of false positives.

**Independent Test**: Deploy a demo zone with a single Null MX record (`0 .`). Verify the exporter detects it and the dashboard surfaces "Email: explicitly disabled via Null MX (RFC 7505)" rather than reading FAIL on the "MX records present" check.

**Acceptance Scenarios**:

1. **Given** a zone publishing exactly one MX record with preference 0 and exchange `.`, **When** a probe cycle completes, **Then** the exporter exposes a per-zone Null-MX boolean gauge reading 1 for that zone.
2. **Given** the same zone, **When** the operator opens the dashboard, **Then** the MX-presence check reads PASS (with detail text explaining the Null MX state), and per-MX resolution / CNAME / syntax checks are suppressed or pass-by-default for that zone.
3. **Given** a zone publishing MX records AND a Null MX record simultaneously (a configuration error), **When** a probe cycle completes, **Then** the Null-MX gauge reads 1 AND the operator can see the conflicting state surfaced as a FAIL on a dedicated "Null MX coexists with real MXes" row.

---

### User Story 3 - Distinguish primary from backup MXes by priority (Priority: P3)

MX records carry a priority (RFC 5321 §5.1 calls it the "preference value"); lower numbers are tried first. Operators want to see at a glance which MX is the primary (lowest preference) and which are backups, both to verify the configured failover order matches intent and to triage incidents where the primary fails over to backups.

**Why this priority**: Operationally useful but additive — the underlying data (per-MX series with priority label from US1) already makes this queryable in PromQL. The story is about the dashboard surfacing it cleanly rather than requiring the operator to write the predicate themselves.

**Independent Test**: A demo zone with three MX records at preferences 10 / 20 / 30 produces dashboard panel rows ordered by priority, with the priority-10 row marked "primary" and the others "backup".

**Acceptance Scenarios**:

1. **Given** a zone with MX records at preferences 10, 20, 30, **When** the operator opens the dashboard, **Then** the per-MX table presents them ordered by priority and visually distinguishes the lowest-priority (primary) row from the higher-priority (backup) rows.
2. **Given** a zone with multiple MX records sharing the same lowest priority (legitimate load-balancing pattern), **When** the operator opens the dashboard, **Then** all tied-lowest-priority MXes are labeled "primary" (not just one of them).

---

### User Story 4 - Syntactic validity of MX target hostnames (Priority: P4)

Same LDH (letter / digit / hyphen) rules that apply to NS hostnames (RFC 952 / 1123) apply to MX exchange names (RFC 5321 §2.3.5). An MX target with underscores, leading / trailing hyphens, or other invalid syntax may be rejected by strict resolvers and SMTP MTAs.

**Why this priority**: Operationally rare (most modern DNS providers reject invalid syntax at config-input time), and the helper from spec N6 (`isValidNSHostname` in `prober/ns_hostname.go`) is directly reusable. Low cost to include, low frequency of catch, low priority.

**Independent Test**: A demo zone (or integration test fixture) with an MX target containing an underscore in a label produces a syntax-valid=0 gauge.

**Acceptance Scenarios**:

1. **Given** a zone with an MX target like `bad_mail.example.com.` (underscore), **When** a probe cycle completes, **Then** the per-MX syntax-valid gauge reads 0 for that target.
2. **Given** a zone with MX targets all using valid LDH-only syntax, **When** a probe cycle completes, **Then** the per-MX syntax-valid gauge reads 1 for every target.

---

### Edge Cases

- **Zone with no MX records and no Null MX**: most-common email-failure mode. Status row "Zone has MX records OR Null MX" reads FAIL. Detail text explains both options.
- **Zone with MX target pointing to apex of the same zone**: legitimate (`example.com. MX 10 example.com.`). Tested through the resolution path like any other target.
- **Zone with very many MX records (e.g. 10+)**: per-MX series scale linearly. Acceptable cardinality (typical zones have 1-4 MXes; even extreme cases stay bounded).
- **MX target that's a wildcard (`*.example.com.`)**: not RFC-prohibited but unusual. Wildcard expansion happens at resolver time; the MX record's exchange field is the literal hostname. Treat as a normal target.
- **Internationalized Domain Names (IDN) in MX target**: must be punycode-encoded at the DNS level. Syntax-validity check covers this automatically (punycode labels are LDH-compliant).
- **MX target resolves only to AAAA (no A)**: technically valid; some legacy MTAs may not handle it. Out of this spec's scope — treat as "resolved" if either family resolves.
- **MX query fails (timeout, SERVFAIL)**: distinct from "zone has no MX records". Surface as a probe-failure (`dnshealth_query_success{check="mx"} = 0`) rather than implying the zone has no MXes.
- **MX target with no A/AAAA and not a CNAME (genuinely no records)**: resolution gauge reads 0. Distinct from "is a CNAME" (which reads 1 on the is-cname gauge).
- **Null MX with priority other than 0**: RFC 7505 requires preference 0. A record like `15 .` is malformed. Detect via the exchange == "." check; the dashboard can surface the unusual priority via the per-MX info gauge's labels.
- **Zone configured to be monitored without email**: operator sets up the zone but doesn't intend to receive email AND doesn't publish Null MX (forgot, or legacy config). The "no MXes AND no Null MX" row reads FAIL — that's correct per spec; operator either publishes Null MX or accepts the FAIL.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST query each configured zone for MX records via the established probe-cycle pipeline, against each authoritative nameserver.
- **FR-002**: System MUST expose a per-MX info gauge carrying labels for the zone, the MX target hostname (canonical FQDN, lowercase per RFC 4343), and the priority/preference value.
- **FR-003**: System MUST, for each MX target hostname, attempt A and AAAA resolution and expose a per-MX resolution gauge (1 if at least one of A or AAAA returns at least one address, 0 otherwise).
- **FR-004**: System MUST, for each MX target hostname, query CNAME directly and expose a per-MX is-CNAME gauge (1 if the target is a CNAME — RFC 2181 §10.3 violation — 0 otherwise).
- **FR-005**: System MUST, for each MX target hostname, validate LDH syntax (RFC 952 / 1123) and expose a per-MX syntax-valid gauge.
- **FR-006**: System MUST detect Null MX records (RFC 7505: a single MX with preference 0 and exchange `.`) and expose a per-zone Null-MX boolean gauge.
- **FR-007**: System MUST expose per-zone count gauges (count of MX records, count of resolved targets, count of CNAMEd targets) following the Reset+Set(0) per-cycle pattern from spec 007 R-2 so PromQL can distinguish "no divergence" from "no data this cycle".
- **FR-008**: System MUST distinguish primary (lowest-preference) MXes from backups via either label encoding on the per-MX gauge or via a dedicated "is-primary" boolean gauge per MX.
- **FR-009**: System MUST suppress or pass-by-default the "MX records present" check for zones with Null MX detected, so the operator does not get a permanent FAIL on intentionally-no-email zones.
- **FR-010**: System MUST surface a dedicated FAIL row for the "Null MX coexists with real MX records" configuration error.
- **FR-011**: System MUST add dashboard status-table rows for MX health (presence, resolution, no-CNAME, syntax-valid, Null MX state) following the existing PASS/FAIL color-background pattern and the `TestStatusChecksHaveDetail` guard test.
- **FR-012**: System MUST add a dedicated per-MX dashboard table presenting each zone's MX records ordered by priority, with columns for hostname, priority, resolution status, CNAME status, and primary/backup classification.

### Key Entities

- **MX record**: A single (zone, preference, exchange) tuple returned by an authoritative server for the zone's TypeMX query. Multiple per zone; zero if the zone declares no email; one with `0 .` exchange if Null MX.
- **MX target hostname**: The exchange field of an MX record. Subject to all the per-hostname validity checks (resolution, CNAME, syntax).
- **Null MX state**: A boolean per-zone flag derived from the presence of exactly one MX record with preference 0 and exchange `.` per RFC 7505.
- **Primary/backup classification**: Per-MX classification derived from per-zone minimum preference: any MX whose preference equals the zone's minimum is "primary"; others are "backup".
- **Per-zone count gauges**: Derived aggregations of the per-MX series, emitted with the same Reset+Set(0) pattern as `dnshealth_ns_classification_count` from spec 007.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Operators can identify every MX target with an RFC-2181 violation (CNAMEd target) across all monitored zones via a single metric query, without cross-referencing tables.
- **SC-002**: A demo zone with one resolvable MX, one unresolvable MX, and one CNAMEd MX surfaces the correct per-MX flags within one probe cycle of stack bring-up.
- **SC-003**: A demo zone with a Null MX record surfaces the Null-MX state and the MX-presence row reads PASS (not FAIL) for that zone.
- **SC-004**: An integration-test fixture with both Null MX AND a regular MX record surfaces the configuration-error row as FAIL via the dashboard row E predicate. (Deferred to integration-test coverage rather than a demo zone per research R-8: CoreDNS may reject the contradictory records at zone-parse time, making a reliable demo zone impractical. The metric surfaces are still verifiable via integration test with full PromQL-predicate evaluation.)
- **SC-005**: For zones with multiple MX records, the dashboard's per-MX table presents rows ordered by priority and visually distinguishes primary from backup MXes.
- **SC-006**: The integration test suite includes at least one fixture for each of: (a) healthy multi-MX zone, (b) unresolvable MX target, (c) CNAMEd MX target, (d) Null MX zone, (e) Null-MX-coexists-with-MX configuration error, (f) zone with no MX records and no Null MX.
- **SC-007**: The demo deployment includes at least three new zones exercising distinct MX states: clean (multi-MX), Null-MX, and a broken-MX case (CNAMEd or unresolvable target). Each has a smoke assertion verifying the metric surface.
- **SC-008**: The per-MX dashboard table renders correctly for zones with 1 MX record (covered by `mx-null.demo.`) and 2 MX records (covered by `mx-healthy.demo.`). The 5+-MX-record case is operator-eyeball post-deploy — no demo zone is added to exercise it explicitly because the table rendering is structurally identical regardless of row count, and adding a synthetic 5+-MX demo zone would have no DNS-correctness signal (just visual-padding rows). Operators monitoring zones with many MXes should spot-check the rendering on their own deployment.

## Assumptions

- All new metrics use the `dnshealth_mx_*` prefix, are snake_case, and follow the bounded-cardinality / type-suffix conventions from Constitution Principle II.
- The MX prober follows the same `RegisterProber()` + `ProbeFn` pattern as the existing `glue`, `soa`, `recursion`, `ns_hostname`, and `ns_classification` probers — no new prober-pipeline abstractions introduced.
- Per-MX info gauges carry priority as a label value, not as a metric value. This matches the established info-gauge pattern (`dnshealth_soa_mname` from spec 006 / S1) where the value is always 1 and the interesting data is in labels.
- Multi-auth disagreement on MX records (one auth reports `[X, Y]`, another reports `[X, Y, Z]`) is handled via UNION semantics analogous to spec 007 R-4 — any MX reported by at least one auth is included in the zone's MX set. Disagreement itself is a separate quality concern (already addressable via the existing parent-vs-self comparison row D, though MX records aren't currently in scope of that check).
- The Null MX check uses the canonical RFC 7505 definition: exactly one MX record with preference 0 and exchange `.`. A zone publishing multiple MX records OR a Null MX with a non-zero preference does NOT satisfy this check.
- Reachability is defined as "DNS resolution succeeds" (the MX target hostname resolves to at least one A or AAAA record). No SMTP-level connectivity check is performed — that belongs in `blackbox_exporter` with an SMTP-prober config. The dashboard detail text must say so to avoid operator confusion.
- The per-MX dashboard table follows the existing per-NS-record table pattern (`selfNSRecordsTable` in `demo/dashboard/panels_records.go`) — joined queries by hostname, columns with overrides for narrow width and color-background where appropriate.
- Demo zones added for this feature follow the existing pattern: dedicated CoreDNS container at a fresh static IP, zone file under `demo/coredns/<name>/`, delegation entry in the root zone, smoke assertion in `demo/smoke.sh`. Estimated 3-4 new containers depending on how many demo states the operator wants visualized vs. integration-test-only.
- The dashboard rows ship with `detail` text passing the `TestStatusChecksHaveDetail` guard test (per the `feedback-metric-needs-dashboard` memory rule applied since spec 007 / PR #35).
- Existing per-NS metric series and dashboard panels are unchanged. The MX feature adds purely additive series and adds either a new panel or extends an existing per-zone records row — TBD in the planning phase.
- Probe-cycle latency budget impact: one TypeMX query per zone per cycle, plus one CNAME query per MX target per cycle, plus the resolution queries (which the existing `ResolveHostnames` already optimizes per-hostname). For typical zones with 1-4 MX records, this adds ~5-10 DNS queries per cycle per zone — bounded and well within budget.
