# Specification Quality Checklist: MX Prober Family

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
- Prominent "Scope of this feature" section at the top explicitly bounds the work — SMTP probing, email-auth records (SPF/DMARC/DKIM), PTR checks, and RFC-1918 detection are listed as out-of-scope so reviewers can't accidentally expand the spec at plan time.
- Assumptions section references established codebase patterns (`dnshealth_mx_*` prefix, `RegisterProber()`, the spec-007 Reset+Set(0) idiom, the `TestStatusChecksHaveDetail` guard test) — these are interface contracts and naming conventions established by prior specs, not implementation directives. Treated as "house style" the spec is allowed to invoke, similar to how spec 007 referenced the same conventions.
- One scope decision worth flagging to a reviewer (documented in the "Explicitly out of scope" block): SPF/DMARC/DKIM TXT-record checks could reasonably be folded into this spec since they're operationally tied to email DNS health. Chose to defer to keep this spec tight to MX-specific concerns. If the reviewer wants them in scope, this is the place to redirect before `/speckit-plan`.
- Four user stories (P1-P4) ordered by operator value. P1 is the MVP; P2 prevents alert noise on intentional no-email zones; P3 is dashboard ergonomics on data the underlying gauges already expose; P4 is a cheap addition that reuses spec-N6's existing helper.
- **Post-`/speckit-analyze` remediation** applied across all 5 findings:
  - **C1 (HIGH)** — Row B "All MX targets resolve" PromQL now includes the explicit Null-MX suppression branch (`OR on(zone) (mx_null_mx == bool 1)`); without this the predicate would have spuriously FAILed on Null-MX zones, defeating US2's no-alert-noise promise. Updated in both `contracts/dashboard-panel.md` and the T010 task description in `tasks.md`.
  - **C2 (MEDIUM)** — SC-004 wording softened to acknowledge the conflict case is integration-test-only (per research R-8's decision to defer the demo zone due to CoreDNS parse-time fragility). The metric/predicate behavior is still verified via T025 integration test.
  - **U1 (LOW)** — SC-008 wording softened to explicitly acknowledge that the 5+-MX-record case is operator-eyeball post-deploy (no synthetic demo zone — table rendering is structurally identical regardless of row count).
  - **O1 (LOW)** — plan.md "Performance Goals" corrected to reflect actual query math: 1 + N + 2N = ~3N+1 queries per zone (typical 4-MX zone: 13 queries; range ~5-15).
  - **I1 (LOW)** — T027 marked [P] for consistency with T026; both edit different files and are independent.
  - **U2 (LOW)** intentionally NOT remediated: T030's "verify T008 already covers US4" no-op gate matches the precedent from spec 007 (similar US-presence checkpoints without functional code changes). Kept for user-story tracking visibility.
