# Contract: Generated Dashboard JSON Files

**Date**: 2026-05-15
**Type**: File-on-disk artifact consumed by Grafana provisioning.

The generator emits two Grafana-importable dashboard JSON files. This
contract describes the invariants both files MUST satisfy so that
Grafana provisioning, the demo smoke test, and operators relying on
`$zone` continue to work.

## File 1: `dnshealth-overview.json` (full)

Located at `demo/grafana/dashboards/dnshealth-overview.json`.

**Top-level keys** (bare dashboard JSON; no `resource.Manifest` wrap):
- `uid`: `"dnshealth-overview"`
- `title`: `"DNS Health Overview"`
- `tags`: includes `"dnshealth"` and `"demo"`
- `templating.list[0].name`: `"zone"` (the `$zone` variable)
- `panels`: includes every panel in the v1 dashboard (markdown info,
  three status tables, three records tables, four operator
  timeseries collapsed in a row).

## File 2: `dnshealth-overview-clean.json` (no info text)

Located at `demo/grafana/dashboards/dnshealth-overview-clean.json`.

Identical to File 1 except:
- `uid`: `"dnshealth-overview-clean"` (must differ for Grafana to
  treat as a separate dashboard).
- `title`: `"DNS Health Overview (clean)"`
- `panels` does NOT include the markdown info-text panel; the
  layout MUST reflow upward to fill the gap (no empty space at top).

## Cross-file invariants

Both files MUST:

- Have a `templating` block defining the `$zone` variable, driven
  by Prometheus `label_values(dnshealth_query_success, zone)`,
  defaulting to `healthy.demo.`.
- Reference the Prometheus datasource by `uid: "dnshealth-prometheus"`
  (matches `demo/grafana/provisioning/datasources/datasources.yml`).
- Reference only metric names actually emitted by the exporter
  (cross-checked against `prober/metrics.go`). FR-010.
- Use the same panel queries (PromQL strings) for equivalent panels
  — only the `infoTextPanel` differs.
- Be valid JSON parsable by `encoding/json` and Grafana's dashboard
  loader.

## Schema version

Both files MUST set `"schemaVersion"` to whatever value the SDK
emits. We do NOT pin a schema version in our source; the SDK is the
authority. If `grafana/grafana-oss:latest` rolls forward and emits
warnings, the fix is to bump the SDK pin (R-1).

## Compatibility with Grafana provisioning

Both files MUST sit in `demo/grafana/dashboards/` so they are auto-
loaded by the file provisioner declared in
`demo/grafana/provisioning/dashboards/dashboards.yml`. No
provisioning config edits required.
