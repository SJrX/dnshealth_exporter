# Specification Quality Checklist: Stealth Nameserver Detection

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-05-23
**Feature**: [Link to spec.md](../spec.md)

## Content Quality

- [X] No implementation details (languages, frameworks, APIs)
- [X] Focused on user value and business needs
- [X] Written for non-technical stakeholders
- [X] All mandatory sections completed

## Requirement Completeness

- [X] No [NEEDS CLARIFICATION] markers remain
- [X] Requirements are testable and unambiguous
- [X] Success criteria are measurable
- [X] Success criteria are technology-agnostic (no implementation details)
- [X] All acceptance scenarios are defined
- [X] Edge cases are identified
- [X] Scope is clearly bounded
- [X] Dependencies and assumptions identified

## Feature Readiness

- [X] All functional requirements have clear acceptance criteria
- [X] User scenarios cover primary flows
- [X] Feature meets measurable outcomes defined in Success Criteria
- [X] No implementation details leak into specification

## Notes

- Items marked incomplete require spec updates before `/speckit.clarify` or `/speckit.plan`
- Two minor fixes applied during the first validation pass:
  - SC-001 changed "PromQL query" → "metric query" to stay technology-agnostic
  - SC-005 reframed the squishy "~30 seconds" target as a concrete list of fields the dashboard must surface
- Scope clarification accepted (post-review): RFC 8499 defines "stealth server" as one absent from every public NS RR set — undetectable from a single-vantage-point exporter. Added a prominent "What 'stealth' means in this feature" section near the top of the spec stating that we surface the achievable divergence-based approximation (NS hostnames appearing in one source but not another) and that the dashboard detail text must disclose this limitation to operators. The Assumptions section's redundant bullet was slimmed to reference the new definition section.
- **Post-`/speckit-analyze` remediation** (HIGH finding C1 — FR-010 coverage gap): Option A applied. FR-010 reworded to explicitly require an active SOA probe of self-only stealth NSes (resolve hostname out-of-band, query SOA against resolved IPs). Added new metric `dnshealth_ns_stealth_reachable{zone, nameserver}` (boolean) to `contracts/classification-metric.md`. Added research decision R-9 detailing the probe mechanism. Added StealthReachability entity to `data-model.md`. Updated `quickstart.md` to reference the new metric instead of the unreachable `dnshealth_query_success{check="soa"}` workaround. Tasks renumbered: Phase 4 (US2) expanded from 3 to 7 tasks (T016-T022) covering the probe implementation, runner gauge plumbing, two integration tests, detail-text refresh, multi-auth test, and quickstart drift check; Phase 5 (US3) becomes T023; Phase 6 becomes T024-T027. New total: 27 tasks (after de-duplicating an accidental orphan T016 created during the first remediation pass).
