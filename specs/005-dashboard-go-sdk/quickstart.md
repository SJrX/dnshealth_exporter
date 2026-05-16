# Quickstart: 005-dashboard-go-sdk

**Date**: 2026-05-15

This document is the maintainer-facing operational summary of the
feature: how to regenerate the dashboards, how to verify your
changes, and how to override demo ports.

## Regenerate the dashboard JSON

```bash
make dashboards
```

(Or, equivalently: `go run ./demo/dashboard` from repo root.)

This rewrites:

- `demo/grafana/dashboards/dnshealth-overview.json`
- `demo/grafana/dashboards/dnshealth-overview-clean.json`

Both files are pretty-printed and terminated by a single trailing
newline. Re-running with no source changes produces byte-identical
output (drift test enforces this).

## Verify your dashboard change end-to-end

1. Edit the typed source under `demo/dashboard/`.
2. `make dashboards` to regenerate.
3. `cd demo && docker compose up -d --build` — Grafana provisioning
   picks up the new JSON within `updateIntervalSeconds: 10`.
4. Open Grafana, switch the `$zone` variable through
   `healthy.demo.`, `soa-serial-mismatch.demo.`,
   `lame-nameserver.demo.`, `ns-mismatch.demo.` to verify panels
   render correctly for each.
5. `cd demo && ./smoke.sh` — confirms no regression in the underlying
   exporter contract.
6. `cd demo && docker compose down -v` to tear down.

## Run the drift test

```bash
go test -tags=integration ./demo/dashboard/...
```

If the test fails with "dashboard JSON drifted from generator
source", run `make dashboards` and commit the resulting diff.

## Override demo ports

Defaults: Grafana `3000`, Prometheus `9090`, exporter `9053`.

```bash
EXPORTER_PORT=19053 GRAFANA_PORT=13000 docker compose up -d
```

The README's URL examples reference the defaults.

## Add a new panel

1. Add a function `myPanel() *<pkg>.PanelBuilder` in
   `demo/dashboard/panels_<category>.go`.
2. Wire it into the `buildOverview()` builder chain (in
   `demo/dashboard/dashboard.go`).
3. `make dashboards` to regenerate.
4. `go test -tags=integration ./demo/dashboard/...` to update the
   golden files (or fail — the test will tell you to re-run).

## Add a new dashboard variant

Lift the conditional from `buildOverview(includeInfoText bool)` to a
new boolean (e.g., `includeOperatorRow bool`), wire a new emit step
in `main.go`, and add the new `uid`/`title`/`path` constants.
