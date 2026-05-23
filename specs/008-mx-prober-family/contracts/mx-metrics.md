# Contract: MX Prober Metrics

External interface contract for the new metrics exposed by the `mx` prober. Consumed by: any Prometheus scraper, the bundled Grafana dashboard, and any operator-written alert rules.

---

## `dnshealth_mx_info`

**Type**: gauge (always 1 when present; info-style)
**Labels**:

| Label | Source | Values |
|---|---|---|
| `zone` | configured zones | Canonical FQDN. |
| `target` | per MX RR | Canonical FQDN, lowercase. For Null MX records, this is `"."` (root label). |
| `priority` | per MX RR | Decimal string of the uint16 priority field (e.g., `"10"`, `"20"`, `"0"`). |
| `check`, `ip`, etc. | ProbeResult plumbing | Standard prober-pipeline labels — `check="mx"`, `ip=""` (MX prober doesn't have a per-NS-IP fan-out shape). |

**Cardinality bound**: At most `|zones| × (MX records per zone)` series per cycle. Typical zones: 1-4 MX records, so a deployment monitoring 100 zones expects ~200-400 series. Bounded.

**Value**: Always 1 when the series exists. Absence = no such MX record in this cycle.

**Example series**:

```text
dnshealth_mx_info{zone="example.com.",target="mail-a.example.com.",priority="10",check="mx",ip=""} 1
dnshealth_mx_info{zone="example.com.",target="mail-b.example.com.",priority="20",check="mx",ip=""} 1
dnshealth_mx_info{zone="no-email.example.com.",target=".",priority="0",check="mx",ip=""} 1
```

---

## `dnshealth_mx_resolves`

**Type**: gauge (boolean: 0 or 1)
**Labels**:

| Label | Source | Values |
|---|---|---|
| `zone` | configured zones | Canonical FQDN. |
| `target` | per unique MX target | Canonical FQDN, lowercase. |
| `check`, `ip` | ProbeResult plumbing | `check="mx"`, `ip=""`. |

**Value**: 1 if `ResolveHostnames(target)` returned at least one A or AAAA address; 0 otherwise (including DNS-resolution failure or no records at all).

**Cardinality**: One series per unique MX target per zone. Deduped at emit time so a zone with two MX records pointing at the same target produces ONE series (not two).

---

## `dnshealth_mx_is_cname`

**Type**: gauge (boolean: 0 or 1)
**Labels**: same as `dnshealth_mx_resolves`.

**Value**: 1 if `lookupCNAME(target)` returned true (target is a CNAME — RFC 2181 §10.3 violation); 0 if not a CNAME (the healthy case) OR if the CNAME lookup itself failed (treated as non-CNAME — see notes below).

**Notes**: A CNAME-lookup failure (network error mid-walk) is conservatively treated as "not a CNAME" rather than "unknown" to avoid spuriously alerting on transient DNS noise. Operators concerned about this can correlate with `dnshealth_query_success{check="mx"}`.

---

## `dnshealth_mx_syntax_valid`

**Type**: gauge (boolean: 0 or 1)
**Labels**: same as `dnshealth_mx_resolves`.

**Value**: 1 if the target hostname passes the LDH validity check from `isValidNSHostname` (RFC 952 / 1123 hostname syntax); 0 otherwise. Special case: Null MX's `"."` target is considered syntactically valid (it's an RFC-defined sentinel, not a hostname).

---

## `dnshealth_mx_is_primary`

**Type**: gauge (boolean: 0 or 1)
**Labels**:

| Label | Source | Values |
|---|---|---|
| `zone` | configured zones | Canonical FQDN. |
| `target` | per MX target | Canonical FQDN, lowercase. |

**Value**: 1 if this target's priority equals the minimum priority across all MX records for the zone; 0 if there's a lower-priority MX in the same zone. For zones with a single MX, that MX is `is_primary=1`. For zones with multiple MXes tied at the minimum priority, all tied MXes get `is_primary=1`.

**Set by**: cycle runner's aggregation pass (not the prober directly), because the per-zone minimum is needed before any per-MX classification can be assigned. Lives on the permanent registry with Reset per cycle.

---

## `dnshealth_mx_null_mx`

**Type**: gauge (boolean: 0 or 1)
**Labels**:

| Label | Source | Values |
|---|---|---|
| `zone` | configured zones | Canonical FQDN. |

**Value**: 1 if the zone publishes exactly one MX record with priority 0 and target `"."` (canonical RFC 7505 Null MX form); 0 otherwise — including the "no MX records at all" case and the "multiple MXes including a `.` target" malformed-config case.

**Cardinality**: One series per configured zone (Reset+Set(0|1) per cycle, ensures the series exists for every zone every cycle so PromQL can distinguish "explicitly opt-out" from "no data").

---

## `dnshealth_mx_count` / `_resolved_count` / `_cname_count`

**Type**: gauge (non-negative integer)
**Labels**: `{zone}` only.

**Values**:

- `dnshealth_mx_count{zone}` — total MX records for the zone this cycle. Includes Null MX (`1` for a Null-MX zone; `0` for no-MX-at-all).
- `dnshealth_mx_resolved_count{zone}` — count of MX targets where `dnshealth_mx_resolves == 1`.
- `dnshealth_mx_cname_count{zone}` — count of MX targets where `dnshealth_mx_is_cname == 1`.

**Reset semantics**: GaugeVecs `Reset()` at start of each cycle; explicit `Set(N)` per configured zone — including `Set(0)` when N is zero — so PromQL can distinguish "no divergence" from "no data this cycle". Mirrors `dnshealth_ns_classification_count` from spec 007 R-2.

---

## `dnshealth_query_success{check="mx"}` and `dnshealth_query_duration_seconds{check="mx"}`

Standard prober-result-pipeline emissions. One series per zone per cycle for the TypeMX query attempt. Operators can use `dnshealth_query_success{check="mx"} == 0` to detect zones where the MX prober couldn't reach the auth at all — distinguishing "real MX-records-absent" from "couldn't ask the question this cycle."

---

## Compatibility & invariants

- **No changes** to existing `dnshealth_ns_*`, `dnshealth_soa_*`, `dnshealth_query_*`, or any other established series.
- All new series are purely additive; existing dashboards and alert rules continue to work unchanged.
- The label-value conventions match the codebase: case-folded canonical FQDNs for hostnames, decimal-string priority.
- The per-zone count gauges follow the exact Reset+Set(0) pattern from spec 007 — operators familiar with `dnshealth_ns_classification_count` will find these immediately legible.
