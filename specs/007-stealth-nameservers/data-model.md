# Data Model: Stealth Nameserver Detection (Spec 007)

Phase 1 output of `/speckit-plan`. Describes the ephemeral data structures the new classifier prober materializes per probe cycle, the gauge series it emits, and the relationships between them. Everything here lives inside a single probe cycle — no persistence.

---

## Entities

### Classification (per NS hostname, per zone, per cycle)

The unit of detection. Each NS hostname observed in either source for a given zone gets exactly one Classification per probe cycle.

| Field | Type | Notes |
|---|---|---|
| `zone` | string (canonical FQDN) | The zone being classified. |
| `nameserver` | string (canonical FQDN, lowercase) | The NS hostname. Case-folded per RFC 4343 / FR-006. |
| `classification` | enum | One of `parent-only`, `self-only`, `both`. Exactly one per (zone, nameserver). |

**Lifecycle**: created during `ProbeNSClassification` execution by walking the union of parent and self NS-hostname sets and applying the membership-derived classification. Survives only until the end of the cycle (no persistence). Emitted as `dnshealth_ns_classification{zone, nameserver, classification} = 1`.

**Validation rules**:

- Both `zone` and `nameserver` MUST be in canonical FQDN form (trailing dot present).
- `nameserver` MUST be case-folded to lowercase before comparison (per FR-006; matches existing `prober/ns_hostname.go` / `prober/soa.go` patterns for case-insensitive NS comparisons).
- `classification` MUST be exactly one value; ambiguity is a bug.

### ZoneClassificationCount (per zone, per classification, per cycle)

A derived per-zone aggregation suitable for status-table predicates and alert thresholds.

| Field | Type | Notes |
|---|---|---|
| `zone` | string (canonical FQDN) | The zone. |
| `classification` | enum | One of `parent-only`, `self-only`, `both`. |
| `count` | non-negative integer | Number of NS hostnames with this classification in this zone this cycle. |

**Lifecycle**: emitted per cycle as `dnshealth_ns_classification_count{zone, classification}`. The gauge is `Reset()` at the start of each cycle, then `Set(count)` per (zone, classification) tuple — including `Set(0)` when a classification has zero entries. This explicit-zero pattern (mirroring `dnshealth_parent_delegation` from PR #29) is what distinguishes "no divergence" (count=0) from "no data this cycle" (no series at all).

**Validation rules**:

- For every probed zone, ALL THREE classification values (`parent-only`, `self-only`, `both`) MUST receive an explicit `Set` call per cycle — even if count is 0 — so the series exists.
- `count` equals the cardinality of the corresponding Classification series with the same `(zone, classification)` filter.
- When `delegation.NSRecords` is empty for a zone (delegation walk failed), the classifier emits no Classification entries but DOES emit ZoneClassificationCount entries with count=0 for all three values (per R-5).

### StealthReachability (per self-only NS, per cycle)

Added per FR-010 / research R-9. Surfaces whether a detected self-only stealth NS actually answers authoritatively when probed directly — the data backing the dashboard's hidden-master-vs-leaked-listing disambiguation.

| Field | Type | Notes |
|---|---|---|
| `zone` | string (canonical FQDN) | The zone whose stealth NS is being probed. |
| `nameserver` | string (canonical FQDN, lowercase) | The stealth NS hostname. ONLY populated for classification = `self-only`. |
| `reachable` | boolean | 1 if any IP resolved for the hostname returned an authoritative SOA response for `zone`; 0 if hostname doesn't resolve OR no resolved IP responded authoritatively. |

**Lifecycle**: created during `ProbeNSClassification` execution as a follow-up pass after the per-NS classification is computed. For each self-only NS, call `ResolveHostnames(hostname)`; for each resolved IP, issue an SOA query; short-circuit on first authoritative success. Survives only until end of cycle. Emitted as `dnshealth_ns_stealth_reachable{zone, nameserver}`.

**Validation rules**:

- Emitted ONLY for NSes classified as `self-only` — the other classifications have other diagnostic surfaces.
- `nameserver` MUST match the same canonical form as the Classification entity's `nameserver`.
- `reachable` is strictly boolean — no third "unknown" state. Resolution failure is `0`, matching the "leaked listing" semantics in the spec.

### Sets used during classification

Internal-only data structures used during a single cycle's computation. Not emitted.

| Set | Source | Population |
|---|---|---|
| `parentSet` | `delegation.NSRecords` | Unique NS hostnames from the parent's referral, case-folded canonical FQDN. |
| `selfSet` | per-NS NS-RR-set query responses | Union across all reachable parent-listed NSes of the NS hostnames each one reports for the zone. Same canonicalization. |
| `union` | parentSet ∪ selfSet | The Classification iteration target. |

---

## Relationships

```
                  ┌────────────────────────────┐
                  │ delegation.NSRecords       │
                  │ (parent-side, from         │
                  │ existing WalkDelegation)   │
                  └───────────┬────────────────┘
                              │
                              ▼
                       canonicalize
                              │
                              ▼
                          parentSet ──────────────────┐
                                                      │
  per parent-listed NS                                │
   │                                                  │
   ▼                                                  │
  fresh DNS query: <zone> NS                          │
   (one query per nameserver, this prober's only      │
    DNS traffic — see research R-3)                   │
   │                                                  │
   ▼                                                  │
  selfNS list per response                            │
   │                                                  │
   union (cross-NS)                                   │
   │                                                  │
   ▼                                                  ▼
  selfSet ──────────────┬──────────────► parentSet ∪ selfSet (union)
                        │                         │
                        │                         │ for each hostname H:
                        ▼                         │
                   set membership                 ▼
                   tests                    classification =
                                              both      if H ∈ parent ∧ H ∈ self
                                              parent-only if H ∈ parent ∧ H ∉ self
                                              self-only if H ∈ self   ∧ H ∉ parent
                                                  │
                                                  ▼
                                          emit Classification series
                                                  │
                                                  ▼
                                          aggregate counts
                                                  │
                                                  ▼
                                          emit ZoneClassificationCount series
```

---

## State transitions

The Classification entity has no state transitions within a cycle — it is computed once and emitted. Across cycles, a given (zone, nameserver) tuple may move between classifications (e.g., when the parent registrar finally pushes a config change). Operators observe these transitions via Prometheus time series; the exporter itself does not track them.

The ZoneClassificationCount entity transitions between values across cycles as NSes appear / disappear / move classifications. Time-series treatment of the per-zone count is the operator's primary alerting surface (`changes(dnshealth_ns_classification_count{classification="self-only"}[1h]) > 0` to detect new stealth NSes appearing).

---

## Edge case mapping (from spec)

| Edge case (spec) | Data model handling |
|---|---|
| Hidden master pattern (legitimate) | Surfaced as a `self-only` Classification entry. Detail text in the dashboard row warns operators it may be intentional. |
| All authoritative servers unreachable | `selfSet` is empty. `parentSet` may be non-empty. Every parent NS becomes `parent-only`; the count for that classification will be ≥ 1, status row reads FAIL. Distinguished from real asymmetry via correlation with `dnshealth_query_success{check="ns_classification"}` series being 0 for that zone's NSes. |
| NS-set drift between auth servers | Handled by union semantics (R-4). `selfSet` is the union; classification operates on it. Drift itself is the row D check from issue #36; not this feature's concern. |
| Case-insensitive comparison | Enforced at set-construction time (case-fold both sets before iteration). |
| Trailing-dot normalization | Enforced via `dns.Fqdn()` on every name before insertion. |
| Zero auth-side data (delegation walk failed) | `delegation.NSRecords` empty → no Classification entries; ZoneClassificationCount emitted with count=0 for all three classifications (R-5). |
