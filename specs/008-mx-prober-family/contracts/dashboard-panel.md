# Contract: MX Dashboard Surface

External interface contract for the dashboard additions consumed by anyone viewing the bundled Grafana dashboard or exporting the JSON to grafana.com.

---

## New panel: "MX — status"

A new status-row-style panel positioned BELOW the existing Parent / NS / SOA status row. Same shape as the existing status tables: instant PromQL targets reduced `seriesToRows`, two columns (Test / Result), PASS/FAIL color-background on Result.

**Placement**: in `buildOverview` in `demo/dashboard/dashboard.go`, after the existing status-row trio. GridPos `(0, subY(12, yOffset), 24, 4)` — full width, height 4 grid units (smaller than the existing status panels because MX has fewer rows; can grow to 6 if needed).

**Rows** (each is one `statusCheck` literal in a new `mxStatusChecks` slice in `panels_status.go`):

| Row | Label | PromQL | Detail text |
|---|---|---|---|
| A | "Zone has MX records (or Null MX intentionally set)" | `((dnshealth_mx_count{zone="$zone"} > bool 0) or on(zone) (dnshealth_mx_null_mx{zone="$zone"} == bool 1)) or on() vector(0)` | Metric: `dnshealth_mx_count` + `dnshealth_mx_null_mx`. Why FAIL: zone publishes no MX records AND no Null MX declaration → all incoming email fails. Either publish MX records or declare Null MX (`0 .`) per RFC 7505 if no email is intended. |
| B | "All MX targets resolve" | `((dnshealth_mx_count{zone="$zone"} == bool dnshealth_mx_resolved_count{zone="$zone"}) and on(zone) (dnshealth_mx_count{zone="$zone"} > bool 0)) or on(zone) (dnshealth_mx_null_mx{zone="$zone"} == bool 1) or on() vector(0)` | Metric: count comparison `_count` vs `_resolved_count`, with explicit pass-by-default for Null-MX zones. Why FAIL: at least one MX target's hostname does not resolve to any A or AAAA. Inbound mail attempts for that target will fail. Investigate via the per-MX table below — `resolves=0` column. Suppressed (always PASS) for Null-MX zones via the `null_mx == 1` branch — Null MX has no resolvable targets and a literal `count == resolved_count` predicate would otherwise spuriously FAIL because Null MX's `.` target intentionally has no `mx_resolves` emission. |
| C | "No MX target is a CNAME (RFC 2181 §10.3)" | `(dnshealth_mx_cname_count{zone="$zone"} == bool 0) or on() vector(0)` | Metric: `dnshealth_mx_cname_count == 0`. Why FAIL: at least one MX target is an alias (CNAME), violating RFC 2181 §10.3. Many MTAs handle this inconsistently; some refuse delivery outright. Investigate via the per-MX table — `is_cname=1` column. |
| D | "All MX target hostnames syntactically valid (LDH)" | `(min by (zone) (dnshealth_mx_syntax_valid{zone="$zone"}) == bool 1) or on() vector(0)` | Metric: `min(dnshealth_mx_syntax_valid) == 1`. Why FAIL: at least one MX target hostname has invalid syntax (underscore, leading/trailing hyphen, etc.) per RFC 952/1123. Some strict resolvers may reject. Reuses the same LDH check applied to NS hostnames in spec N6. |
| E | "No conflict between Null MX and real MX records" | `((dnshealth_mx_null_mx{zone="$zone"} == bool 0) or on(zone) (dnshealth_mx_count{zone="$zone"} == bool 1)) or on() vector(0)` | Metric: derived from `_null_mx` + `_count`. Why FAIL: zone publishes Null MX (`0 .`) AND additional MX records. RFC 7505 requires Null MX to be the SOLE MX record. Configuration error; behavior is undefined and likely interpreted differently by different MTAs. |

All rows ship with `detail` text that passes the `TestStatusChecksHaveDetail` guard test (verified before the new statusCheck is added per the `feedback-metric-needs-dashboard` memory rule).

---

## New table: per-MX records

A new joined-by-target table in the records row alongside the existing parent-NS / self-NS / SOA-serials tables. Same `selfNSRecordsTable` shape from `panels_records.go`.

**Placement**: `(0, subY(22, yOffset), 24, 8)` — full width, height 8, positioned below the existing records-row tables. Could fold into 8-grid-unit width if dashboard layout becomes crowded; full width chosen because the per-MX table has 6+ columns and benefits from horizontal space.

**Columns** (post-Organize transformation):

| Column | Source query / RefId | Notes |
|---|---|---|
| Target | `dnshealth_mx_info` label `target` | Sorted alphabetically ascending; primary indicator goes on Priority column. |
| Priority | `dnshealth_mx_info` label `priority` | Numeric sort would be better but Grafana's Organize transformation orders rows by the SortBy column. Use SortBy(Priority asc) so primary appears first. |
| Resolves | `dnshealth_mx_resolves` value | Yes/no color-background mapping (green/red). |
| Is CNAME | `dnshealth_mx_is_cname` value | Yes/no color-background mapping (red/green — CNAME is bad). |
| Syntax valid | `dnshealth_mx_syntax_valid` value | Yes/no mapping (green/red). |
| Role | `dnshealth_mx_is_primary` value | Mapped to "primary"/"backup" via value-mappings. |

**Query shape**: `WithTarget(...).WithTarget(...)` for each of the 5 metrics, joined by `target` field. Mirrors how `selfNSRecordsTable` joins NS / Responded / Recursion by `nameserver`.

**Width overrides**:

- Target: no width (auto-expands)
- Priority: 80px (fits "999" + margin)
- Resolves / Is CNAME / Syntax valid: 90-100px each (Yes/No)
- Role: 100px ("primary"/"backup")

**SortBy**: Priority ascending (so primary first, backups in priority order). Falls back to alphabetical Target for ties.

---

## Compatibility

- New panel + table; no existing panels modified beyond the row coordinates shifted to accommodate the new MX panel placement.
- Drift test (`TestDashboardJSONMatchesGenerator`) will require regenerated JSON to be committed alongside the source change — `make dashboards` produces both `dnshealth-overview.json` and `dnshealth-overview-demo.json`.
- New `mxStatusChecks` slice in `panels_status.go` follows the same `var nameStatusChecks = []statusCheck{...}` pattern as the existing slices, enabling the `TestStatusChecksHaveDetail` guard to iterate it automatically.
