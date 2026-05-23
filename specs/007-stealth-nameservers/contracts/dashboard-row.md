# Contract: NS-Status "No Stealth NSes" Row

External interface contract for the new row added to the "NS — status" panel of the demo Grafana dashboard. Consumed by: anyone viewing the dashboard, anyone exporting the dashboard JSON to grafana.com or to a separate Grafana instance.

---

## Row identity

- **Panel**: `NS — status` (the middle table in the status row at `demo/dashboard/panels_status.go`)
- **Position**: appended to `nsStatusChecks` slice as row `G` (after the existing `F` for "No NS hostname is a CNAME"). Selected to keep the existing alphabetical sequence intact.
- **Label** (shown in the `Test` column): `No stealth NSes (parent and self agree on NS set)`
- **Result column**: standard PASS/FAIL color background, identical to other rows in the same panel.

---

## PromQL predicate

```promql
(
  max by (zone) (dnshealth_ns_classification_count{classification="self-only",zone="$zone"}) == bool 0
)
and on(zone) (
  max by (zone) (dnshealth_ns_classification_count{classification="parent-only",zone="$zone"}) == bool 0
)
or on() vector(0)
```

**Semantics**:

- **PASS (= 1)** when both self-only count AND parent-only count are zero for the selected zone — i.e., the parent's NS set and the union of self-side views are identical.
- **FAIL (= 0)** when either count is > 0, OR when no series exist at all for the selected zone (the `or on() vector(0)` fallback).
- The `max by (zone)` aggregation is structurally a no-op (there's only one series per (zone, classification) tuple per cycle from the count gauge), but follows the existing predicate idiom used by other rows in the panel.

---

## Detail text (mandatory per `feedback-metric-needs-dashboard` rule)

Multi-line markdown rendered via Grafana's panel `Description()` tooltip ("i" icon). Must pass the `TestStatusChecksHaveDetail` guard test (non-empty `detail` field on the `statusCheck`).

Required content:

```markdown
**Metric**: `dnshealth_ns_classification_count{classification="self-only"|"parent-only"}`
**Why FAIL matters**: At least one NS hostname is asymmetric — the parent advertises an NS the zone's own authoritative servers don't list (parent-only), or an auth lists an NS the parent doesn't (self-only, the "stealth" case). Self-only divergence can be a legitimate hidden-master setup (NOTIFY-driven primary not in the public NS set, by design) — verify intent before alerting.
**Scope limitation**: This row detects asymmetry between sources we can query. RFC 8499 "stealth" servers — those absent from EVERY public source — are not detectable by any single-vantage-point exporter and remain invisible to this check. See spec 007 for the full definition.
**Investigate**: NS records (from parent) and NS records (from the zone) tables side-by-side — the hostname appearing in only one table is the asymmetric NS. For the self-only case, check whether the missing-from-parent NS responds to direct SOA queries to distinguish a working hidden master from a leaked listing.
```

---

## Position rationale

The row lives in the "NS — status" panel because it operates on the NS RR set, matching the panel's semantic grouping. Same panel as the existing row D ("Parent and self report same NS records") — the two rows are complementary:

- **Row D** (from #36): checks set-equality on NS names (every NS appears in both sources, exactly).
- **Row G** (this spec): same underlying data, expressed as count-of-asymmetric-NSes ≥ 1 — equivalent boolean outcome for the strict-equality case, but the new metric series allow operators to drill into WHICH NSes are asymmetric and HOW (parent-only vs self-only), not just whether ANY asymmetry exists.

The two rows agreeing (both PASS or both FAIL) is a sanity check; row G provides the per-NS detail that row D's predicate doesn't expose at the metric layer.

---

## Compatibility

- Adding the row bumps the NS — status table from 6 rows to 7. Panel height stays at `H=8` grid units — still comfortably fits at `CellHeight=Sm`.
- No changes to other rows in the panel or to other panels.
- The new metric labels (`classification`) are completely new and do not collide with any existing label set.
- The drift test (`TestDashboardJSONMatchesGenerator`) will require the regenerated JSON to be committed alongside the source change, per the established pattern (`make dashboards` regenerates).
