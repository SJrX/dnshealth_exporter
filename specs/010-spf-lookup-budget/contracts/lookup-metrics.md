# Contract: SPF Lookup-Budget Metrics

These three gauges were **reserved** by spec 009 (`contracts/email-auth-metrics.md`) and **ship** in this feature. Per-zone, additive, gauges, `dnshealth_spf_` prefix (constitution Principle II). Emitted via the `email_auth` prober's ProbeResult pipeline (empty `nameserver`/`ip` labels, like the other SPF gauges; dashboard aggregates `by (zone)`).

| Metric | Type | Labels | Values | Semantics |
|--------|------|--------|--------|-----------|
| `dnshealth_spf_lookup_count` | gauge | `zone` | integer 0–11 | DNS-lookup-incurring mechanisms required to evaluate the record. Exact for 0–10; **11 means "≥11"** (the walk stops the instant it exceeds 10). |
| `dnshealth_spf_lookup_budget_exceeded` | gauge | `zone` | 0 / 1 | 1 iff the resolved count exceeds the RFC 7208 §4.6.4 limit of 10. Asserted only on lookups that actually resolved — an unreachable include cannot make this 1 by itself. |
| `dnshealth_spf_lookup_eval_complete` | gauge | `zone` | 0 / 1 | 0 iff any `include`/`redirect` branch was unreachable, macro-bearing, or cycle-truncated this cycle (so `count`/`exceeded` are a lower bound). 1 iff the whole record resolved. |

## Emission rule

All three are emitted **only** when the zone has exactly one valid SPF record (spec 009 `spf_present=1 ∧ spf_record_count=1 ∧ spf_valid=1`). For no-SPF / multiple-record / malformed zones they are **absent** → the dashboard row reads N/A via `absent()`. (Unlike the spec-009 boolean SPF gauges, these are not zero-emitted for every zone, because "0 lookups" is a meaningful in-budget value distinct from "no SPF record at all.")

## Interaction (the trustworthiness guarantee)

- `budget_exceeded=1` ⇒ genuinely over budget (FAIL), regardless of `eval_complete`.
- `budget_exceeded=0 ∧ eval_complete=0` ⇒ under budget *so far*, but an include was unreachable — **PASS, not FAIL** (no transient-include false alarm).
- `budget_exceeded=0 ∧ eval_complete=1` ⇒ confidently within budget (PASS).

## Stability

Metric names + the `zone` label key are the contract. Exporter emits raw signals only — no threshold/verdict (Principle V). The §4.6.4 void-lookup limit is **not** part of this contract (deferred).
