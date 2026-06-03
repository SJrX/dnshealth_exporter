# Phase 1 Data Model: SPF DNS-Lookup Budget Check

Stateless within a probe cycle; these are in-memory parse/evaluation results the extended `email_auth` prober produces. No persistence.

## Entity: SPFMechanism (parser output, new in spf.go)

One term of an SPF record, classified for counting.

| Field | Type | Notes |
|-------|------|-------|
| `Kind` | enum | `include` / `redirect` / `a` / `mx` / `ptr` / `exists` / `all` / `other` |
| `Target` | string | the name after `include:` / `redirect=` (empty for non-recursing kinds) |
| `HasMacro` | bool | true if `Target` contains `%{` — unresolvable without sender context |

The parser also exposes, per record, `HasAll bool` (any `all` mechanism present) so the counter can apply the redirect-precedence rule (R-6).

**Derivation**: `a`/`mx`/`ptr`/`exists`/`include`/`redirect` are the six lookup-incurring kinds; only `include`/`redirect` carry a `Target` and recurse. `ip4`/`ip6`/`all`/unknown → `Kind=other`/`all`, cost 0 lookups.

## Entity: SPFLookupResult (per zone)

The outcome of `countSPFLookups` for a zone's apex SPF record.

| Field | Type | Notes |
|-------|------|-------|
| `Count` | int | 0–10 exact; 11 means "≥11" (stop-at-11, Q3) |
| `Exceeded` | bool | true iff `Count > 10` among **resolved** lookups (R-3) |
| `Complete` | bool | false iff any include/redirect branch was unreachable, macro-bearing, or cycle-truncated |

**Validation / derivation rules**
- Produced **only** when the zone has exactly one valid SPF record (spec 009: `spf_present=1 ∧ spf_record_count=1 ∧ spf_valid=1`). Otherwise no result and no `spf_lookup_*` series are emitted → dashboard row N/A.
- `Exceeded` is asserted on resolved lookups only: an `include` that didn't resolve contributes its own `+1` but not a sub-count, and cannot by itself push `Exceeded` true (R-3).
- A detected cycle (target already visited) or depth-cap hit short-circuits to `Count=11, Exceeded=true` (R-5) and sets `Complete=false`.

## Metric mapping (see contracts/lookup-metrics.md)

| Field | Metric |
|-------|--------|
| `Count` | `dnshealth_spf_lookup_count{zone}` |
| `Exceeded` | `dnshealth_spf_lookup_budget_exceeded{zone}` |
| `Complete` | `dnshealth_spf_lookup_eval_complete{zone}` |

## Lifecycle (per probe cycle)

1. `email_auth` prober already fetched the apex TXT and built the spec-009 `SPFRecord` (present / count / valid / qualifier).
2. **New**: if `present ∧ recordCount==1 ∧ valid`, parse the record's mechanisms (`SPFMechanism` list + `HasAll`) and run `countSPFLookups(record, resolveSPFRecord, …)`.
3. `resolveSPFRecord` fetches the SPF TXT of each `include`/`redirect` target via the iterative-from-root walk; `a`/`mx`/`ptr`/`exists` add `+1` with no query.
4. Emit `SPFLookupResult` → the three `dnshealth_spf_lookup_*` gauges (empty nameserver/ip labels; dashboard aggregates `by (zone)`).
5. Dashboard PromQL applies FAIL/PASS/N-A (no threshold in the exporter).

## Out of model (this feature)

- The §4.6.4 **void-lookup** limit (≤2 NXDOMAIN/NODATA) — deferred (clarification Q2).
- Exact over-budget totals beyond 11 — not computed (clarification Q3).
- Macro expansion / real SPF evaluation against a sender IP — out of scope.
