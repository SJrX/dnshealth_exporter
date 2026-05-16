# Data Model: 005-dashboard-go-sdk

**Date**: 2026-05-15
**Status**: Complete

This feature has no persisted data, no API entities, and no
configuration schema. The "data model" is the in-memory Go
representation of the dashboard, expressed via the Grafana Foundation
SDK plus a small layer of project-local helpers.

---

## Entity 1: `Dashboard` (SDK type)

The SDK's `dashboard.Dashboard` is the top-level entity. Built via
`dashboard.NewDashboardBuilder("Title")` and a chain of `.With*`
calls; emitted via `.Build()` then `json.MarshalIndent`.

**Key fields used**:
- `uid` — must be unique per emitted file (`dnshealth-overview` for
  the full variant, `dnshealth-overview-clean` for the no-info
  variant).
- `title` — `"DNS Health Overview"` (full),
  `"DNS Health Overview (clean)"` (clean variant).
- `tags`, `timezone`, `refresh`, `time`, `templating`, `panels`,
  `editable` — set via builder chain to match v1 dashboard values.

---

## Entity 2: `TemplateVariable` — `$zone`

The single template variable, query-driven from Prometheus.

```go
dashboard.NewQueryVariableBuilder("zone").
    Label("Zone").
    Datasource(prometheusDS).
    Query(dashboard.StringOrMap{String: cog.ToPtr(
        "label_values(dnshealth_query_success, zone)")}).
    Refresh(dashboard.VariableRefreshOnDashboardLoad).
    Sort(dashboard.VariableSortAlphabeticalAsc).
    Current(dashboard.VariableOption{
        Selected: cog.ToPtr(true),
        Text:  dashboard.StringOrArrayOfString{String: cog.ToPtr("healthy.demo.")},
        Value: dashboard.StringOrArrayOfString{String: cog.ToPtr("healthy.demo.")},
    })
```

Default value: `healthy.demo.` (preserved from v1, FR-009).

---

## Entity 3: `Panel` (SDK type, polymorphic)

One Go function per logical panel returns the appropriate
`*<pkg>.PanelBuilder`:

| Function                       | Returns                    | Source file                   |
|--------------------------------|----------------------------|-------------------------------|
| `infoTextPanel()`              | `*text.PanelBuilder`       | `panels_info.go`              |
| `parentStatusTable()`          | `*table.PanelBuilder`      | `panels_status.go`            |
| `nsStatusTable()`              | `*table.PanelBuilder`      | `panels_status.go`            |
| `soaStatusTable()`             | `*table.PanelBuilder`      | `panels_status.go`            |
| `parentNSRecordsTable()`       | `*table.PanelBuilder`      | `panels_records.go`           |
| `selfNSRecordsTable()`         | `*table.PanelBuilder`      | `panels_records.go`           |
| `soaSerialsTable()`            | `*table.PanelBuilder`      | `panels_records.go`           |
| `probeCycleDurationTimeseries()` | `*timeseries.PanelBuilder` | `panels_operator.go`        |
| `dnsQueryRateTimeseries()`     | `*timeseries.PanelBuilder` | `panels_operator.go`          |
| `cacheHitRatioTimeseries()`    | `*timeseries.PanelBuilder` | `panels_operator.go`          |
| `delegationCacheTimeseries()`  | `*timeseries.PanelBuilder` | `panels_operator.go`          |

Each panel function reads the same `prometheusDS` constant and the
same `$zone` variable reference, so they compose identically into
both dashboard variants.

---

## Entity 4: `DataTransformerConfig` (SDK type, with project helpers)

The SDK's transformation type is open (`Options any`). We wrap each
of the four transformation IDs we use in a tiny typed helper to
catch mis-spellings at compile time:

```go
// demo/dashboard/transforms.go

const (
    TransformJoinByField   = "joinByField"
    TransformOrganize      = "organize"
    TransformFilterByValue = "filterByValue"
    TransformReduce        = "reduce"
)

type JoinByFieldOptions struct {
    ByField string `json:"byField"`
    Mode    string `json:"mode"` // "outer" or "inner"
}

func JoinByField(opts JoinByFieldOptions) dashboard.DataTransformerConfig {
    return dashboard.DataTransformerConfig{
        Id:      TransformJoinByField,
        Options: opts,
    }
}
// (same shape for Organize, FilterByValue, Reduce)
```

**Validation**: Each helper is invoked with a typed struct literal,
so a renamed or missing field is a compile error. The transformation
`Id` is referenced from the constants block, so a typo there is
local to one line.

---

## Entity 5: Generator output paths

Hard-coded inside `demo/dashboard/main.go`:

```go
const (
    fullVariantPath  = "demo/grafana/dashboards/dnshealth-overview.json"
    cleanVariantPath = "demo/grafana/dashboards/dnshealth-overview-clean.json"
)
```

The generator MUST be invoked from the repo root (so the relative
paths resolve). `make dashboards` enforces this from the Makefile.

---

## Relationships

```
            +---------------------+
            | DashboardBuilder    |  ← built from typed source
            +----------+----------+
                       |
        +------------------+-----------------+
        |                  |                 |
        v                  v                 v
 TemplateVariable    Panel(s) (multi)   Layout (rows)
   ($zone, query     │
    label_values)    │
                     v
              prometheus.DataqueryBuilder
                     │
                     v
              [PromQL strings — SC-002 typo gate]
```

The two dashboard variants share every entity above except for the
top-level `uid`/`title` and whether `infoTextPanel()` is included.

---

## State transitions

There are none. Generation is one-shot and idempotent: same source +
same SDK version → byte-identical JSON (R-4, FR-004).
