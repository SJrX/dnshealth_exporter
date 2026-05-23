# Data Model: MX Prober Family (Spec 008)

Phase 1 output of `/speckit-plan`. Describes the ephemeral data structures the MX prober materializes per probe cycle, the gauge series it emits, and the relationships between them. Everything is per-cycle; no persistence.

---

## Entities

### MXRecord (per zone, per cycle)

A single MX record returned by the zone's authoritative server in the TypeMX response.

| Field | Type | Notes |
|---|---|---|
| `zone` | string (canonical FQDN) | The zone being probed. |
| `target` | string (canonical FQDN, lowercase) | The MX exchange field (case-folded per RFC 4343). |
| `priority` | uint16 | The MX preference value (lower = preferred per RFC 5321 §5.1). |

**Lifecycle**: created during `ProbeMX` execution by parsing each MX RR in the authoritative response. Survives only until end of cycle. Emitted as `dnshealth_mx_info{zone, target, priority="N"} = 1`.

**Validation rules**:

- `zone` MUST be canonical FQDN form (trailing dot).
- `target` MUST be canonicalized via `dns.Fqdn(...) + strings.ToLower(...)` before being used as a label or as a set-membership key.
- `priority` is uint16 per the DNS wire format; emitted as a decimal-string label.

### MXTargetValidity (per (zone, target), per cycle)

The boolean-flag bundle attached to each MXRecord's target hostname. Derived by running three sub-checks against each unique MX target.

| Field | Type | Source | Notes |
|---|---|---|---|
| `resolves` | bool | `ResolveHostnames(target)` returns non-empty | Either A or AAAA satisfies. |
| `is_cname` | bool | `lookupCNAME(target)` returns true | RFC 2181 §10.3 violation when true. |
| `syntax_valid` | bool | `isValidNSHostname(target)` returns true | Reused from spec N6; same LDH rules. |

**Lifecycle**: computed per unique MX target per cycle. Cache by canonical target name so duplicate targets in multi-MX zones cost only one round of checks. Survives only until end of cycle.

**Emitted as**:

- `dnshealth_mx_resolves{zone, target}` = 0|1
- `dnshealth_mx_is_cname{zone, target}` = 0|1
- `dnshealth_mx_syntax_valid{zone, target}` = 0|1

### NullMXState (per zone, per cycle)

A boolean per-zone flag derived from parsing the TypeMX response.

| Field | Type | Notes |
|---|---|---|
| `zone` | string (canonical FQDN) | The zone. |
| `is_null_mx` | bool | True iff exactly one MX RR is in the response AND `priority == 0` AND `target == "."`. |

**Lifecycle**: derived at parse time of the MX response. Emitted as `dnshealth_mx_null_mx{zone}`. Per-zone gauge on the permanent registry, Reset+Set(0|1) per cycle.

**Validation rules**:

- RFC 7505 §3 specifies the exact form. A response with multiple MX RRs is not Null MX even if one of them has `target == "."` (that's the conflict case, handled separately).
- Detection MUST be tolerant of case ("." is the wire-form root label; miekg/dns parses it as `"."` already).

### MXClassification (per (zone, target), per cycle)

Derived per-target primary/backup classification.

| Field | Type | Source | Notes |
|---|---|---|---|
| `is_primary` | bool | True iff `target`'s priority equals the minimum priority across all MX records for the zone. | Multi-MX-tied-for-primary cases all get `is_primary=1`. |

**Lifecycle**: computed by the cycle runner's aggregation pass after the prober emits its results. For each zone, find `min(priorities)`, then iterate per-MX results and Set the gauge accordingly.

**Emitted as**: `dnshealth_mx_is_primary{zone, target}` = 0|1. Per-(zone, target) gauge on the permanent registry, Reset per cycle.

### ZoneMXCount (per zone, per cycle)

Per-zone count gauges emitted via the cycle runner's aggregation pass.

| Field | Type | Notes |
|---|---|---|
| `mx_count` | non-negative integer | Total count of MX records for the zone (includes Null MX if present). |
| `resolved_count` | non-negative integer | Count of MX targets where `resolves == true`. |
| `cname_count` | non-negative integer | Count of MX targets where `is_cname == true`. |

**Lifecycle**: emitted as three separate gauges per cycle:

- `dnshealth_mx_count{zone}`
- `dnshealth_mx_resolved_count{zone}`
- `dnshealth_mx_cname_count{zone}`

GaugeVecs `Reset()` at start of each cycle; explicit `Set(N)` per zone — including `Set(0)` when a zone has no MX records — so PromQL can distinguish "no divergence" from "no data this cycle". Mirrors `dnshealth_ns_classification_count` semantics from spec 007 R-2.

---

## Relationships

```
                  ┌────────────────────────────────┐
                  │ TypeMX query → one auth server │
                  └───────────────┬────────────────┘
                                  │
                                  ▼
                          parse MX RRs
                                  │
                ┌─────────────────┼─────────────────┐
                ▼                 ▼                 ▼
            MXRecord[]      NullMXState        zone min(priority)
            (one per RR)    (per zone)         (per zone, derived)
                │                                   │
                ▼                                   ▼
        per-target dedup                    MXClassification
        ┌────────────────┐                  is_primary per target
        ▼                ▼
   ResolveHostnames   lookupCNAME           ← (existing helpers,
   per target         per target              spec N6/#27)
        │                │
        ▼                ▼
   MXTargetValidity (resolves, is_cname, syntax_valid)
                │
                ▼
   per-MX ProbeResult emission
                │
                ▼
   cycle.Runner aggregation pass
   ┌────────────────────────────────────────┐
   │ • count MX records → ZoneMXCount.mx_count
   │ • count resolves==1 → ZoneMXCount.resolved_count
   │ • count is_cname==1 → ZoneMXCount.cname_count
   │ • derive min priority → MXClassification.is_primary
   │ • Set NullMXState directly from prober flag
   └────────────────────────────────────────┘
                │
                ▼
   per-zone count + boolean gauges on permanent registry
```

---

## State transitions

The MXRecord entity has no in-cycle state transitions. Across cycles, the set of (zone, target) tuples shifts as zones update their MX records — operators observe this via Prometheus time series.

The NullMXState boolean transitions between 0 and 1 across cycles when a zone publishes / removes Null MX. Operators alert on `changes(dnshealth_mx_null_mx[1h]) > 0` to detect such transitions.

The MXClassification booleans transition when MX priorities are reordered (e.g., promoting a backup to primary). Stable within a cycle.

---

## Edge case mapping (from spec)

| Edge case (spec) | Data model handling |
|---|---|
| Zone with no MX records and no Null MX | `mx_count = 0`, `null_mx = 0`. The MX-presence dashboard row (`null_mx == 1 OR mx_count > 0`) reads FAIL. |
| Zone with MX target pointing to apex of same zone | Treated as a normal target; the resolution check walks DNS like any other hostname. |
| Zone with many MX records (10+) | Per-MX series scale linearly. Cardinality bound = configured zones × max-MX-per-zone, typically small. |
| MX target as wildcard | Treated as literal hostname; wildcard expansion is a resolver concern, not surfaced here. |
| IDN MX targets | Must be punycode-encoded at the DNS level; syntax check naturally validates the encoded form. |
| MX target resolves only to AAAA | `resolves = 1` (either family satisfies). Out of scope to distinguish further per R-6. |
| MX query fails (timeout, SERVFAIL) | Distinct from "no MX records"; surfaces as `dnshealth_query_success{check="mx"} = 0` via the standard ProbeResult pipeline. The prober emits no MXRecord entities; aggregation Set(0) for all count gauges. |
| MX target with no records (no A/AAAA, not CNAME) | `resolves = 0`, `is_cname = 0`. The status row distinguishes "all resolve" from "no CNAMEs". |
| Null MX with priority != 0 | Malformed per RFC 7505. The prober's parse logic detects only the canonical form (`priority == 0 && target == "."`); a malformed `15 .` does NOT set `null_mx = 1`. The per-MX info gauge still surfaces it (priority="15", target=".") for visibility. |
| Null MX coexists with real MX records | Both `null_mx = 1` AND `mx_count > 1`. The status row "No conflict between Null MX and real MX records" reads FAIL when this condition is met. |
