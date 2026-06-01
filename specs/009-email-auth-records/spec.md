# Feature Specification: Email-Authentication DNS Records (Tier 1: SPF + DMARC)

**Feature Branch**: `009-email-auth-records`
**Created**: 2026-05-31
**Status**: Draft
**Input**: User description: "Email-authentication DNS record health checks (Tier 1: SPF and DMARC). Add a new prober that, per monitored zone, queries the well-known email-authentication TXT records and exposes granular Prometheus gauges so operators can detect missing or malformed email-auth configuration that breaks deliverability even when MX records are healthy. GitHub issue #44, the deliberate follow-on to spec 008 (MX prober family)."

## Scope of this feature

Adds **DNS-level** health checks for the **email-authentication TXT records** of each configured zone — the records that receiving mail servers consult to decide whether mail claiming to be from a domain is legitimate. A zone whose MX records are perfect (spec 008) but whose SPF / DMARC are missing or malformed still has broken email: messages get rejected for failing authentication, or land in spam because the sender is unauthenticated. This feature surfaces those failure modes as metrics.

Tier 1 covers the two records that publish at **fixed, well-known names** and therefore validate cleanly from an external vantage point:

- **SPF** (RFC 7208) — a TXT record at the **zone apex** beginning `v=spf1`, declaring which hosts may send mail for the domain.
- **DMARC** (RFC 7489) — a TXT record at **`_dmarc.<zone>`** beginning `v=DMARC1`, declaring the policy a receiver should apply when SPF/DKIM alignment fails.

**Explicitly out of scope** for this feature (enumerated so the spec scope stays tight, mirroring spec 008):

- **DKIM** (RFC 6376) — DKIM public keys publish at `<selector>._domainkey.<zone>`, where the **selector is an operator-chosen string that travels only inside the `DKIM-Signature` header of a sent message**. Selectors cannot be enumerated from DNS alone, so DKIM cannot be probed by the same clean external lookup SPF and DMARC allow. Tracked as a follow-up in [#57](https://github.com/SJrX/dnshealth_exporter/issues/57).
- **Tier 2 records** — MTA-STS (RFC 8461), TLS-RPT (RFC 8460), BIMI (draft). Real operational value but lower deployment; deferred to keep this cycle spec-008-sized.
- **Tier 3 / DANE** — TLSA records for SMTP (RFC 7672) are only meaningful with DNSSEC validation, which the exporter does not perform. Deferred until DNSSEC support exists.
- **SPF DNS-lookup budget (RFC 7208 §4.6.4 ten-lookup limit)** — the only SPF check that requires recursively resolving `include`/`redirect` targets (and therefore a recursive resolver). It is User Story 3 / Priority P3, the optional refinement, and is **deferred to [#58](https://github.com/SJrX/dnshealth_exporter/issues/58)** so this cycle stays pure-string parsing with no new dependency. The clarified severity model still designates an over-budget record FAIL *when that check lands*.
- **Actually sending or receiving mail** — no SMTP connection, no message signing/verification, no live deliverability test. The exporter inspects the published DNS records only.
- **Policy enforcement / alerting thresholds** — consistent with the project's "detection, not policy" principle, the exporter exposes raw signals (presence, qualifier, policy value); operators decide what severity and alerting to attach in Grafana / Alertmanager.

The feature stays inside the DNS data plane the exporter already operates on: two TXT queries per zone (apex for SPF, `_dmarc.<zone>` for DMARC). No recursive resolution, no new transport, no SMTP, no DNSSEC.

## Clarifications

### Session 2026-05-31

- Q: How should SPF adverse states map to the four-state severity model? → A: **Structural/PermError errors → FAIL; weak-but-valid policy → WARN; safe → PASS.** Specifically: a *broken* record (two `v=spf1` records, or malformed syntax — PermError-class, where the operator tried to publish SPF and the DNS is objectively wrong) reads FAIL; an *absent or weak* policy (no SPF at all, `?all` neutral, no terminal `all`, or `+all` which is syntactically valid but authorizes any sender) reads WARN ("verify intent"); `-all` / `~all` reads PASS. Principle: **FAIL = the record is broken; WARN = the record is absent or its policy is weak/reckless; PASS = present and safe.** (The over-10-lookup-budget case is also broken/FAIL under this principle, but that check is deferred to [#58](https://github.com/SJrX/dnshealth_exporter/issues/58).) No RFC mandates absent-SPF be a hard failure — RFC 7208 governs receiver evaluation, not monitoring severity — and this mapping aligns with the project's "detection, not policy" principle and its existing use of WARN for verify-intent states (e.g. hidden-master MNAME).
- Q: How should email-auth rows behave for a zone with no MX records or a Null MX (RFC 7505)? → A: **Always evaluate, MX-independent.** SPF and DMARC protect a domain from being *forged as a sender*, which is independent of whether the domain *receives* mail (MX). A no-mail or Null-MX domain is still a spoofing target and should publish `v=spf1 -all` + DMARC `p=reject`, so the email-auth rows apply to every configured zone regardless of MX state: such a zone reads PASS when it publishes the anti-spoofing records and WARN when it does not. The status-row detail text MUST explain this anti-spoofing rationale so the dashboard does not look contradictory ("Null MX says no email, yet email-auth nags about SPF"). Email-auth N/A is reserved for within-email-auth inapplicability (e.g. the terminal-`all`-qualifier row when the zone has no SPF record), never derived from MX state.
- Q: How much SPF scope belongs in v1 vs. how complex must it be? → A: **Defer the DNS-lookup-budget check (US3).** v1 SPF is pure string parsing — presence, single-record, terminal `all` qualifier — needing no recursion, no recursive resolver, and **no new dependency**. The 10-lookup budget check is the only part that needs recursive resolution; it is P3 (optional refinement) and is split to [#58](https://github.com/SJrX/dnshealth_exporter/issues/58). Full DMARC ships in v1.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See SPF presence and safety per zone (Priority: P1) 🎯 MVP

A zone operator wants to know, for each zone they monitor, whether the zone publishes a valid SPF record and whether that record ends in a safe "all" qualifier. SPF is the foundational, most broadly deployed email-authentication record; a missing or permissive SPF record means anyone can send mail claiming to be from the domain and pass the receiver's first authentication check.

**Why this priority**: This is the single most operationally-valuable email-auth signal and the cheapest to probe (one TXT query at the apex). A domain with no SPF — or with `+all`, which explicitly authorizes the entire internet — is trivially spoofable, and operators rarely notice until their mail is rejected or their domain is used in a phishing campaign. Every other story refines this one; this is the MVP.

**Independent Test**: Deploy a demo zone with a valid SPF record ending `-all` and another with no SPF record at all. Verify the exporter surfaces an SPF-present signal for the first and not the second, exposes the terminal qualifier, and that the dashboard status row reads PASS for the healthy zone and the configured adverse state for the missing one.

**Acceptance Scenarios**:

1. **Given** a zone publishing exactly one TXT record `v=spf1 include:_spf.example.net -all`, **When** a probe cycle completes, **Then** the exporter exposes an SPF-present signal reading 1 for the zone and a terminal-qualifier signal identifying the qualifier as `-all` (fail), and the SPF status row reads PASS.
2. **Given** a zone publishing no `v=spf1` TXT record, **When** a probe cycle completes, **Then** the SPF-present signal reads 0 and the SPF status row reads **WARN** ("verify intent") with detail text explaining the spoofing exposure and that a non-sending domain should still publish `v=spf1 -all`.
3. **Given** a zone publishing `v=spf1 +all`, **When** a probe cycle completes, **Then** the terminal-qualifier signal identifies `+all` (pass) and the SPF status row reads **WARN** with detail text explaining that `+all` is a syntactically valid but reckless policy that authorizes any sender.
4. **Given** a zone publishing **two** separate `v=spf1` TXT records, **When** a probe cycle completes, **Then** the exporter exposes a multiple-SPF-records signal reading 1 and the SPF status row reads **FAIL** (RFC 7208 §3.2: more than one SPF record is a permanent error — a broken record, not a weak policy).
5. **Given** a zone publishing `v=spf1 include:_spf.example.net` with **no** terminal `all` mechanism and no `redirect`, **When** a probe cycle completes, **Then** the terminal-qualifier signal reports "none" and the SPF status row reads WARN.

---

### User Story 2 - See DMARC presence and enforcement policy per zone (Priority: P2)

A zone operator wants to know whether the zone publishes a DMARC record and, if so, what enforcement policy it declares (`none`, `quarantine`, or `reject`). DMARC ties SPF and DKIM together with alignment and tells receivers what to do on failure; a domain with SPF but no DMARC still permits look-alike spoofing that SPF alone doesn't stop, and a domain stuck at `p=none` is only monitoring, not enforcing.

**Why this priority**: Lower than SPF because DMARC builds on SPF and is less universally deployed, and because `p=none` is a legitimate rollout stage rather than an outright failure — so the signal is more "where on the maturity curve is this domain" than "is mail broken right now." Still high value: surfacing the policy lets an operator confirm enforcement matches intent and spot domains that silently regressed to `none`.

**Independent Test**: Deploy a demo zone publishing `v=DMARC1; p=reject; rua=mailto:dmarc@example.com` at `_dmarc.<zone>` and a zone with no DMARC record. Verify the exporter surfaces DMARC presence and the policy value as a label for the first, nothing for the second, and that the dashboard surfaces the policy and the configured "no DMARC" state.

**Acceptance Scenarios**:

1. **Given** a zone publishing `v=DMARC1; p=reject` at `_dmarc.<zone>`, **When** a probe cycle completes, **Then** the exporter exposes a DMARC-present signal reading 1 and a policy signal carrying `reject`, and the DMARC status row reads PASS.
2. **Given** a zone publishing `v=DMARC1; p=none` at `_dmarc.<zone>`, **When** a probe cycle completes, **Then** the policy signal carries `none` and the DMARC status row reads WARN with detail text explaining that `p=none` only monitors and does not enforce.
3. **Given** a zone with no `v=DMARC1` record at `_dmarc.<zone>`, **When** a probe cycle completes, **Then** the DMARC-present signal reads 0 and the DMARC status row reflects the configured "no DMARC" state.
4. **Given** a zone publishing a `_dmarc` TXT record that begins `v=DMARC1` but omits the mandatory `p=` tag, **When** a probe cycle completes, **Then** the exporter exposes a DMARC-malformed signal reading 1 and the DMARC status row reads FAIL (RFC 7489 requires `p=` as the first tag after the version).

---

### User Story 3 - Catch SPF records that exceed the DNS-lookup budget (Priority: P3) — DEFERRED to [#58](https://github.com/SJrX/dnshealth_exporter/issues/58)

Warning operators when an SPF record exceeds the RFC 7208 §4.6.4 ten-lookup limit is the most subtle SPF failure, but it is the only SPF check that requires recursively resolving `include`/`redirect` targets — and thus a recursive resolver. As the optional P3 refinement, it is **out of scope for this cycle** and tracked in [#58](https://github.com/SJrX/dnshealth_exporter/issues/58); v1 keeps SPF to pure-string parsing (US1) and full DMARC (US2). The clarified severity model already designates an over-budget record FAIL for when #58 lands.

---

### Edge Cases

- **Zone that intentionally sends no mail**: such a domain should still publish `v=spf1 -all` (and optionally DMARC `p=reject`) to prevent spoofing. The exporter cannot infer intent, so "no SPF" is reported uniformly per the configured severity (see Assumptions); operators suppress per-zone if a bare domain is deliberately unconfigured.
- **TXT record split into multiple character-strings**: a single long SPF/DMARC record is often published as several quoted strings that must be concatenated (RFC 7208 §3.3). The exporter MUST concatenate them before parsing, not treat each string as a separate record.
- **Other TXT records at the same name**: the apex and `_dmarc` names commonly hold unrelated TXT records (verification tokens, etc.). The exporter MUST select records by their `v=spf1` / `v=DMARC1` prefix and ignore the rest.
- **Case-insensitivity**: the `v=spf1` / `v=DMARC1` version tokens and mechanism/tag names are case-insensitive per their RFCs; matching MUST not be case-sensitive.
- **Querying `_dmarc` returns NXDOMAIN vs NODATA**: both mean "no DMARC policy at this name" and MUST be treated identically as "DMARC not present".
- **`redirect=` modifier present with no terminal `all` mechanism**: the record has no `all`, so the terminal-qualifier check reports "none" (WARN). v1 does not follow the `redirect` to find an effective qualifier (that needs resolution, like the deferred budget check); it reports the syntactic state. When both `all` and `redirect` appear, `all` wins (RFC 7208) and the qualifier check reads the `all`.
- **Zone with no MX records or a Null MX (`0 .`, RFC 7505)**: email-auth checks still apply (SPF/DMARC are anti-spoofing, independent of whether the zone receives mail — see Clarifications). The zone reads PASS if it publishes `v=spf1 -all` + DMARC `p=reject`, WARN if it lacks them. The row MUST NOT read N/A or PASS *because of* the MX state, and the detail text MUST explain why a no-mail domain still benefits from these records.

## Requirements *(mandatory)*

### Functional Requirements

**SPF (User Story 1)**

- **FR-001**: The system MUST, for each configured zone, query the TXT records at the zone apex and detect whether exactly one record beginning `v=spf1` is present.
- **FR-002**: The system MUST detect and distinctly flag the presence of **more than one** `v=spf1` record at the apex (RFC 7208 §3.2 permanent error).
- **FR-003**: The system MUST identify the terminal `all` mechanism's qualifier and expose which of `-all` (fail), `~all` (softfail), `?all` (neutral), `+all` (pass), or "none present" applies. This is a syntactic read of the record (no resolution); following a `redirect=` to an effective qualifier is part of the deferred budget work (#58).
- **FR-004**: The system MUST concatenate multi-string TXT records before parsing and MUST select the SPF record by its `v=spf1` prefix, ignoring unrelated TXT records at the same name.
- *FR-005, FR-006 (SPF DNS-lookup budget counting + bounded recursive evaluation) — **deferred to [#58](https://github.com/SJrX/dnshealth_exporter/issues/58)**; the only SPF checks needing recursive resolution. Numbers retained as placeholders to keep later FR references stable.*

**DMARC (User Story 2)**

- **FR-007**: The system MUST, for each configured zone, query the TXT records at `_dmarc.<zone>` and detect whether a record beginning `v=DMARC1` is present, treating NXDOMAIN and NODATA identically as "not present".
- **FR-008**: The system MUST parse the DMARC policy directive `p=` and expose its value (`none`, `quarantine`, `reject`) in a way the dashboard can surface as a label.
- **FR-009**: The system MUST detect and distinctly flag a DMARC record that is present but malformed (begins `v=DMARC1` but lacks a valid `p=` tag).
- **FR-010**: The system SHOULD additionally surface the presence of a subdomain policy (`sp=`) and of aggregate/forensic reporting addresses (`rua=` / `ruf=`) as secondary signals.

**Cross-cutting (all stories)**

- **FR-011**: The system MUST expose all email-auth checks as per-zone Prometheus gauges following the existing `dnshealth_*` naming and the data-only ProbeResult → registry pattern used by every other prober (no thresholds or alerting baked into the exporter).
- **FR-012**: Every new metric MUST ship with Grafana dashboard wiring: a new **"Email auth — status"** panel whose rows use the established four-state FAIL / PASS / N/A / WARN status-row convention (constitution Principle IX), each row carrying detail text describing the metric, what an adverse state means operationally, and where to investigate.
- **FR-013**: Status rows MUST read **N/A** (not a misleading PASS or FAIL) for checks that do not apply to a zone — e.g. the SPF terminal-qualifier row when the zone has no SPF record at all. N/A MUST be derived only from within-email-auth applicability, never from the zone's MX state.
- **FR-017**: Email-auth checks MUST apply to every configured zone **independent of its MX records or Null MX status** (SPF/DMARC are anti-spoofing controls, orthogonal to mail reception). A zone with no MX or a Null MX reads PASS when it publishes anti-spoofing records (`v=spf1 -all` + DMARC `p=reject`) and WARN when it does not; the row detail text MUST explain the anti-spoofing rationale so a no-mail zone's WARN is not mistaken for a contradiction.
- **FR-014**: The feature MUST ship demo zones covering: a fully healthy zone (valid SPF `-all` + DMARC `p=reject`); an SPF-only zone (no DMARC); a zone missing SPF entirely; a permissive zone (`+all` and/or DMARC `p=none`); a broken zone exercising a FAIL state (multiple SPF records and/or malformed DMARC); and a **no-mail / Null-MX zone that still publishes `v=spf1 -all` + DMARC `p=reject`**, proving the email-auth rows read PASS independent of MX state (FR-017). (The over-budget SPF demo zone belongs to the deferred #58.)
- **FR-015**: The feature MUST ship smoke assertions verifying both the happy path and a representative broken case for SPF and for DMARC against the demo zones.
- **FR-016**: All examples, demo zones, and documentation MUST use RFC 2606 reserved names (`example.com`, `example.net`, `*.demo.`) and MUST NOT reference any real or personal domain.

### Key Entities *(include if feature involves data)*

- **SPF record**: the apex `v=spf1` TXT string for a zone. Attributes the feature cares about (v1): presence, count (one vs many), and the terminal `all` qualifier. (The recursively-evaluated DNS-lookup total is deferred to #58.)
- **DMARC record**: the `_dmarc.<zone>` `v=DMARC1` TXT string. Attributes: presence, the `p=` policy value, well-formedness, and optionally `sp=` and `rua`/`ruf` presence.
- **Email-auth probe result (per zone)**: the bundle of signals the prober emits per zone — the SPF attributes, the DMARC attributes, and per-check applicability — that the registry turns into `dnshealth_*` gauges and the dashboard turns into status rows.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For any monitored zone, an operator can determine from the dashboard within one probe cycle whether the zone publishes SPF and whether the SPF terminal qualifier is safe — without writing a PromQL query by hand.
- **SC-002**: For any monitored zone, an operator can determine from the dashboard whether the zone publishes DMARC and which enforcement policy (`none` / `quarantine` / `reject`) it declares.
- **SC-003**: A zone with no SPF record, a zone with `+all`, and a zone with multiple SPF records each surface a distinct, correct adverse state — none is silently reported as healthy.
- **SC-004**: A zone that does not apply to a given check (e.g. the terminal-qualifier row on a zone with no SPF) reads **N/A**, never a misleading PASS or FAIL.
- **SC-005**: The demo stack demonstrates every covered failure mode live, and the smoke suite fails if any happy-path zone regresses to an adverse state or any broken-path zone regresses to healthy.
- **SC-006**: The feature adds no new query transport, protocol, dependency, or recursive resolution beyond the two TXT queries per zone, and a zone's email-auth probe never blocks or aborts the overall probe cycle.

## Assumptions

- **Severity mapping** (resolved in clarification — see Clarifications §2026-05-31): the governing principle is **FAIL = the record is broken (structural / PermError-class); WARN = the record is absent or its policy is weak/reckless; PASS = present and safe.** Concretely — SPF multiple records / malformed → **FAIL**; SPF absent / `?all` / no-terminal-`all` / `+all` → **WARN**; SPF `-all` / `~all` → **PASS** (the over-10-lookup-budget → FAIL case is part of the deferred #58). DMARC present-but-malformed (missing `p=`) → **FAIL**; DMARC absent / `p=none` → **WARN**; DMARC `p=quarantine` / `p=reject` → **PASS**. SPF-absent and DMARC-absent are now symmetric (both WARN), consistent with the broken-vs-weak principle.
- **SPF DNS-lookup budget is deferred** (resolved in clarification — see Clarifications §2026-05-31, tracked in [#58](https://github.com/SJrX/dnshealth_exporter/issues/58)): it is the only SPF check needing recursive resolution of `include`/`redirect` targets, so v1 omits it entirely — no recursive resolver, no new dependency. v1 SPF is pure-string parsing of the apex record.
- **One SPF record per zone is the norm**; the multiple-record case is treated as an error to surface, not a configuration to support.
- **The exporter cannot know whether a zone intends to send mail**, so email-auth checks apply uniformly to every configured zone; operators suppress individual zones in Grafana/Alertmanager if a bare domain is deliberately unconfigured.
- **Reuses the existing prober/registry/dashboard machinery**: a new `email_auth` prober registered in the existing registry, emitting the data-only ProbeResult shape, surfaced through the existing four-state status-row builder — no new framework.
- **DMARC `rua`/`ruf` and `sp=` are surfaced as presence signals only** (FR-010 is SHOULD), not validated for address syntax or external-domain authorization (RFC 7489 §7.1), which is deferred.
