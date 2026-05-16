# Research: Port demo dashboard to Grafana Foundation SDK (Go)

**Date**: 2026-05-15
**Feature**: 005-dashboard-go-sdk
**Status**: Complete — verified against local SDK clone at `/home/sjr/git/grafana-foundation-sdk` (commit `a8c311b58`).

This document closes every NEEDS CLARIFICATION the plan would otherwise
have raised about the SDK, and records the design choices that flow
from those facts.

---

## R-1: SDK module path and version pin

**Decision**: Import as `github.com/grafana/grafana-foundation-sdk/go/...`
(per-package: `dashboard`, `cog`, `common`, `prometheus`, `text`,
`table`, `timeseries`, `resource`). Pin to the `v11.6.x+cog-v0.0.x`
branch — the highest stable Grafana-version branch the SDK currently
publishes.

**Rationale**: Confirmed via local clone:
- Branch list goes up to `v11.6.x+cog-v0.0.x`. No `v12.x` branch
  exists yet despite the README saying "best suited for Grafana >= 12";
  pinning to `v11.6` is the highest currently-shipping option.
- Latest Go module tag in the local clone is `go/v0.0.9` (2026-03-04
  era). The branch tip will be ahead of the tag.
- Our demo runs `grafana/grafana-oss:latest` (currently 11.x or 12.x).
  v11.6-targeted SDK output is forward-compatible with newer Grafana
  per the SDK's own compat note.

**How to pin**: `go get github.com/grafana/grafana-foundation-sdk/go@v11.6.x+cog-v0.0.x`
will resolve to a pseudo-version locked in `go.sum`. The pseudo-version,
not the branch name, is what's actually pinned.

**Alternatives considered**:
- Pin to a `go/v0.0.X` tag — opaque (don't know which Grafana branch
  it was cut from). Rejected for clarity.
- Vendor the SDK — adds ~thousands of generated files to the repo.
  Rejected; standard module pinning is enough.

---

## R-2: Panel-type coverage

**Decision**: Every panel type the v1 dashboard uses has a typed
builder in the SDK. Confirmed packages:

| v1 dashboard panel       | SDK package                  | Builder                    |
|--------------------------|------------------------------|----------------------------|
| Markdown info text       | `go/text`                    | `text.NewPanelBuilder()`   |
| Status / records tables  | `go/table`                   | `table.NewPanelBuilder()`  |
| Operator timeseries      | `go/timeseries`              | `timeseries.NewPanelBuilder()` |
| Row separators           | `go/dashboard`               | `dashboard.NewRowBuilder()`|
| `$zone` template var     | `go/dashboard`               | `dashboard.NewQueryVariableBuilder("zone")` |
| Prometheus query targets | `go/prometheus`              | `prometheus.NewDataqueryBuilder().Expr(...).LegendFormat(...).RefId(...)` |

**Rationale**: Direct grep against local clone — every package and
every method called out exists. `text.NewPanelBuilder()` confirmed
in `go/text/panel_builder_gen.go`; `table.PanelBuilder.WithTransformation`
confirmed in `go/table/panel_builder_gen.go`.

---

## R-3: Transformations — typed at the slot, untyped at the payload

**Decision**: Hand-build each transformation as a
`dashboard.DataTransformerConfig{Id: "...", Options: <map or struct>}`
and attach via `panel.WithTransformation(...)`. Use
`map[string]any` for the `Options` payload because the SDK does not
provide per-transformation typed builders.

**Rationale**: The SDK type is intentionally open:

```go
type DataTransformerConfig struct {
    Id       string `json:"id"`
    Disabled *bool  `json:"disabled,omitempty"`
    Filter   *MatcherConfig `json:"filter,omitempty"`
    Topic    *DataTransformerConfigTopic `json:"topic,omitempty"`
    Options  any    `json:"options"`   // free-form
}
```

`grep DataTransformerConfigBuilder` returns no results in the clone:
the SDK has no `JoinByFieldBuilder`, `OrganizeFieldsBuilder`, etc.

**Implication**: We capture the four transformation IDs we need
(`joinByField`, `organize`, `filterByValue`, `reduce`) in small
typed Go structs at our end (in the generator), with field tags
matching Grafana's expected JSON names. This gives us compile-time
safety on the *shape* of the options without waiting for the SDK to
add typed builders.

**Alternatives considered**:
- Use `map[string]any` directly with no typed wrappers — fast to
  write, no compile-time safety. Rejected because typo-catching is
  one of the core goals (SC-002).
- Submit upstream typed builders — out of scope for this feature.

---

## R-4: Output determinism

**Decision**: Marshal with `encoding/json.MarshalIndent(dashboard, "", "  ")`
and trust Go's stdlib determinism — no extra canonicalisation step
needed.

**Rationale**:
- Go's `encoding/json` emits struct fields in declaration order
  (deterministic for code-generated SDK structs).
- For `map` values (the `Options` field on transformations,
  `fieldConfig.overrides`, panel `options`), Go has marshalled maps
  in sorted key order since Go 1.12.

**Risk & mitigation**: If a future SDK upgrade introduces
non-deterministic generation (e.g., random panel IDs), the drift
test in R-7 catches it on the next run. We accept that risk over
pre-emptively introducing a canonical-JSON dependency.

---

## R-5: Two dashboard variants from one source

**Decision**: One `func buildOverview(includeInfoText bool) *dashboard.DashboardBuilder`
that conditionally calls `.WithPanel(infoTextPanel())` near the top
of its build chain. `main.go` calls it twice with different flags
and writes to two paths.

**Rationale**: Idiomatic SDK pattern — the `grafana-agent-overview`
example factors each panel into a `func ...() *<pkg>.PanelBuilder`
and composes them in a single dashboard builder. Adding a boolean
flag to the composition step is the smallest possible diff.

The two emitted files MUST have distinct `uid`s
(`dnshealth-overview` and `dnshealth-overview-clean`) so Grafana
treats them as separate dashboards. They MUST share their `$zone`
variable definition (one helper) so the user-facing experience is
identical.

**Alternatives considered**:
- Generate JSON, then post-process to strip the info panel — fragile
  (depends on JSON ordering), defeats the point of having a typed
  source.
- Two separate dashboard builders — duplicates ~95% of code, drifts
  over time. Rejected.

---

## R-6: Generation pattern — file write, no resource.Manifest wrap

**Decision**: Generator main function calls
`json.MarshalIndent(dashboard, "", "  ")` and `os.WriteFile(path, b, 0644)`.
**Do not** wrap in a `resource.Manifest{ApiVersion, Kind, Metadata, Spec}`
envelope.

**Rationale**: The `grafana-agent-overview` example wraps in
`resource.Manifest` because it pushes via `gcx`/`grafanactl`. Our
existing dashboard JSON file (`demo/grafana/dashboards/dnshealth-overview.json`)
has *bare* dashboard root keys (`annotations`, `editable`, `panels`,
`templating`, `title`, `uid`, ...) — verified by reading the file.
That's what Grafana's *file provisioner* expects (it wraps the JSON
in metadata internally). Using `resource.Manifest` here would break
provisioning.

**Output paths** (written by the generator):
- `demo/grafana/dashboards/dnshealth-overview.json`        (full)
- `demo/grafana/dashboards/dnshealth-overview-clean.json`  (no info)

Provisioning config (`demo/grafana/provisioning/dashboards/dashboards.yml`)
already points at the `/var/lib/grafana/dashboards` directory and
loads every JSON file in it — no provisioning change needed for the
new file.

---

## R-7: Drift detection

**Decision**: A `go test` golden test in the generator package:
runs the builder, marshals to JSON, and `bytes.Equal`s against the
committed file. On mismatch, writes a unified diff to test output
and prints a "run `make dashboards` to regenerate" hint.

**Rationale**: No SDK-blessed pattern exists. Golden tests are the
standard Go idiom for this and play nicely with `go test ./...` in
CI. The test lives next to the generator (in `demo/dashboard/`)
under the `integration` build tag so it doesn't run in the default
unit-test path that constitution-sensitive code paths use.

**CI hookup**: The existing CI workflow already runs
`go test -tags=integration ./...`. The drift test runs
automatically; no new CI step.

**Alternatives considered**:
- `go generate` with a check-in-CI step that asserts no diff —
  works, but introduces an extra "did you run go generate?" footgun.
  Rejected; a failing test is louder.

---

## R-8: Generator code location and module boundary

**Decision**: Generator lives at
`demo/dashboard/` as a `main` package
(`demo/dashboard/main.go`, `demo/dashboard/dashboard.go`,
`demo/dashboard/panels_*.go`, `demo/dashboard/dashboard_test.go`).

**Why a single Go module**: The exporter and the generator share
`go.mod`. Adding the SDK dependency adds it to the *exporter's*
`go.mod`, but:

1. The exporter binary never imports the `demo/dashboard/` package,
   so `go build ./...` for the exporter does not pull SDK code into
   the binary (Go strips unused dependencies at link time).
2. Adding a second `go.mod` under `demo/` would force contributors
   into multi-module workspace gymnastics for a one-binary project.

**Constitution check (Principle III: Modern Go Ecosystem)**: The
SDK is added as a `go.mod` dep. We are NOT adding it as a runtime
dependency of the exporter. To make this explicit, the
`go.mod` will carry a comment:
`// grafana-foundation-sdk used by ./demo/dashboard only`.

---

## R-9: Regeneration command surface

**Decision**: Two front doors, one underneath:

- **Make target** (primary): `make dashboards` from repo root, runs
  `go run ./demo/dashboard`.
- **Direct invocation** (fallback): `go run ./demo/dashboard` from
  repo root.

The generator writes to fixed paths (no flags). Behaviour is
deterministic; running it on a clean checkout produces no diff.

**README updates required**:
- `demo/README.md` — document the regeneration command and the new
  port defaults.
- `README.md` (repo root) — link to the generator command.

---

## R-10: Port-mapping change ergonomics

**Decision**: Change a single line in `demo/docker-compose.yml`
(`${EXPORTER_PORT:-9266}` → `${EXPORTER_PORT:-9053}`) and update
README references. Grafana and Prometheus stay on 3000 / 9090.

**Smoke test impact**: `demo/smoke.sh` reads `EXPORTER_PORT` from
the environment with a default. Update the default from 9266 to
9053 and the test continues to pass on default invocation.

**Why not also bump Grafana/Prometheus**: User direction (clarified
2026-05-15): change only the exporter default. Grafana 3000 and
Prometheus 9090 are well-known to operators; collisions are
self-evident and the env-var override pattern is already documented.

---

## Honest unknowns / risks remaining

- **SDK release cadence**: If the upstream SDK ships a v12.x branch
  before we cut this work, prefer that branch; the lock file will
  catch any drift. Low risk.
- **Grafana 11 → 12 schema diff**: If `grafana-oss:latest` rolls to
  12 mid-implementation and our v11.6 SDK output starts producing
  warnings in Grafana, the visible symptom is a warning banner in
  the dashboard, not a crash. The drift test will catch the
  underlying schema change. Low-medium risk.
- **Transformation typo at the JSON layer**: Even with our typed
  options structs, mistyping `"joinByField"` as `"joinByFiled"`
  inside the `Id` string would only fail at Grafana load. Mitigation:
  define the four IDs as untyped string constants in one file
  (`transformIDs.go`) and reference the constants. SC-002 covers
  metric-name typos; this is a small extension.
