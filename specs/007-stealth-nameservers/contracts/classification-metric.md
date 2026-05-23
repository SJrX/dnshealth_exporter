# Contract: NS Classification Metrics

External interface contract for the two new metric families exposed by the `ns_classification` prober. Consumed by: any Prometheus scraper, the bundled Grafana dashboard, and any operator-written alert rules.

---

## `dnshealth_ns_classification`

**Type**: gauge (always 1 when present; an info-style series)
**Labels**:

| Label | Source | Values |
|---|---|---|
| `zone` | configured zones | Canonical FQDN of the zone being probed (trailing dot present). |
| `nameserver` | observed NS hostnames | Canonical FQDN, lowercase per RFC 4343. Comes from the union of parent-side and self-side NS RR sets. |
| `ip` | inherited from prober pipeline | The IP the classifier queried for this NS's self-view; empty string for entries that came purely from the parent set without a self-side answer. |
| `classification` | computed | Exactly one of `parent-only`, `self-only`, `both`. |
| `check` | check label (on `query_success`/`query_duration_seconds` only — see below) | Value: `ns_classification`. |

**Cardinality bound**: At most `|zones| × (NS hostnames per zone)` series per cycle. For the demo deployment that's roughly 8 zones × 3 NSes = 24 series. For production-scale use (a few hundred zones, ≤ 10 NSes each), expected upper bound ~3000 series. No per-IP fan-out — classification is per-(zone, nameserver), and the `ip` label carries the address that was queried for the self-view (informational), not a new fan-out axis.

**Value**: Always 1 when the series exists. Absence of a series for a given (zone, nameserver) means that NS hostname is not currently classified — either because the zone wasn't probed this cycle, or because the union of parent ∪ self for that zone is empty.

**Reset semantics**: The underlying GaugeVec is `Reset()` at the start of each cycle. NS hostnames that disappear from the union between cycles drop out of `/metrics` immediately.

**Example series**:

```text
dnshealth_ns_classification{zone="example.test.",nameserver="ns1.example.test.",ip="192.0.2.1",classification="both"} 1
dnshealth_ns_classification{zone="example.test.",nameserver="hidden-primary.example.test.",ip="",classification="self-only"} 1
```

---

## `dnshealth_ns_classification_count`

**Type**: gauge (non-negative integer-valued, typically small)
**Labels**:

| Label | Source | Values |
|---|---|---|
| `zone` | configured zones | Canonical FQDN. |
| `classification` | derived | One of `parent-only`, `self-only`, `both`. |

**Cardinality bound**: `|zones| × 3` series. For the demo deployment that's 8 × 3 = 24 series. Tight, bounded.

**Value semantics**:

- `count > 0`: that many NS hostnames are currently in this classification for this zone.
- `count == 0`: this classification has no NS hostnames for this zone — either no divergence (when classification is `parent-only` or `self-only`) or no overlap (when classification is `both`).
- **Series absent**: distinguishable from `count == 0`. Series absent means the classifier did NOT run for this zone this cycle (e.g., the zone is not in `cfg.Zones`, or `cycle.Runner` short-circuited before the classifier executed).

**Reset semantics**: The underlying GaugeVec is `Reset()` at the start of each cycle. Then for every probed zone, all three classification values are explicitly `Set()` — including `Set(0)` for classifications with no entries. This is the mechanism that ensures `count == 0` is distinguishable from "no data".

**Example series**:

```text
dnshealth_ns_classification_count{zone="example.test.",classification="parent-only"} 0
dnshealth_ns_classification_count{zone="example.test.",classification="self-only"} 1
dnshealth_ns_classification_count{zone="example.test.",classification="both"} 2
```

---

## `dnshealth_ns_stealth_reachable`

**Type**: gauge (boolean: 0 or 1)
**Labels**:

| Label | Source | Values |
|---|---|---|
| `zone` | configured zones | Canonical FQDN. |
| `nameserver` | detected self-only stealth NS | Canonical FQDN, lowercase. |
| `ip` | ProbeResult plumbing | Always empty string for stealth NSes (they have no parent-side glue by definition). |
| `check` | ProbeResult plumbing | Always `ns_classification`. |
| `classification` | ProbeResult plumbing | Always `self-only` (the metric is emitted only for that classification). |

> **Note on extra labels**: the implementation emits this metric through the standard `ProbeResult` → `BuildRegistry` pipeline (see audit.md D-1), which adds the same `ip` / `check` / `classification` labels every other ProbeResult-emitted metric carries. Operators querying `{zone, nameserver}` get correct behavior; the extras are informational and unused in any committed alert rule or dashboard query.

**Cardinality bound**: At most one series per self-only NS per zone — typically zero, occasionally one or two for zones with hidden-master setups. Bounded by the operator's actual stealth-NS count.

**Value semantics**:

- `1`: The classifier resolved the stealth NS hostname out-of-band (via `ResolveHostnames`) and at least one resolved IP returned a valid authoritative SOA response for the zone. Indicates a working hidden-master-style server.
- `0`: Either the hostname did not resolve, OR none of the resolved IPs returned an authoritative SOA. Indicates a leaked / forgotten listing.
- **Series absent**: No self-only stealth NS was detected for this zone this cycle (clean state, no disambiguation needed).

**Probe cost**: One additional `ResolveHostnames` call plus one SOA query per resolved IP, per self-only stealth NS, per cycle. In practice this fires zero times for healthy zones and ≤ 2 times for zones with intentional hidden masters — negligible.

**Reset semantics**: GaugeVec `Reset()` per cycle so detection rolls cleanly across cycles. A stealth NS that disappears between cycles drops out of `/metrics` immediately rather than carrying its last-seen reachability.

**Example series**:

```text
dnshealth_ns_stealth_reachable{zone="hidden-master.demo.",nameserver="hidden-primary.hidden-master.demo."} 0
```

This is the data the row G detail text references when it tells the operator to distinguish "working hidden master" from "leaked listing" without leaving the dashboard. The metric is emitted ONLY for self-only stealth NSes (the classification value `self-only`); the disambiguation question doesn't apply to `parent-only` or `both`.

---

## `dnshealth_query_success{check="ns_classification"}` and `dnshealth_query_duration_seconds{check="ns_classification"}`

These are emitted by the standard prober-result pipeline (one series per (zone, nameserver, ip) tuple that the classifier queried). Operators can use `dnshealth_query_success{check="ns_classification"} == 0` to detect zones where the classifier's self-side query failed — distinguishing "classifier ran cleanly and reported 0 divergence" from "classifier couldn't reach the auth at all this cycle".

---

## Compatibility & invariants

- **No changes** to existing `dnshealth_ns_record`, `dnshealth_ns_glue`, `dnshealth_ns_recursion_available`, `dnshealth_ns_hostname_*`, or `dnshealth_soa_*` series. The new `dnshealth_ns_stealth_reachable` series is the only addition beyond the classification metric family.
- The new series are purely additive; existing dashboards and alerts continue to work unchanged.
- The `classification` label values are stable (`parent-only`, `self-only`, `both`) — operator alert rules can rely on exact string match.
- The `nameserver` label values match the same canonicalization rules used everywhere else in the exporter (FQDN + case-fold).
