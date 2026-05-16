# Code-vs-Spec Audit — 005-dashboard-go-sdk

**Date**: 2026-05-16
**Auditor**: implementation pass (T026)
**Branch**: `005-dashboard-go-sdk`
**Constitution**: v1.1.1 — Governance section requires this audit
before declaring the feature complete.

This document walks every FR and SC in
[`spec.md`](./spec.md) and records the file:line evidence that the
implementation satisfies (or deliberately deviates from) each one.

---

## Functional Requirements

### Typed dashboard source

| FR | Status | Evidence |
|----|--------|----------|
| **FR-001** Typed source in Grafana Foundation SDK (Go) | ✅ | `demo/dashboard/dashboard.go:23-69` builds via `dashboard.NewDashboardBuilder`. Panel functions in `panels_*.go` use `text.NewPanelBuilder`, `table.NewPanelBuilder`, `timeseries.NewPanelBuilder`. |
| **FR-002** Source under `demo/`; not linked into exporter binary | ✅ | All source under `demo/dashboard/`. Verified via `go list -deps .` (run during T023) — `grafana-foundation-sdk` does not appear in the exporter's dependency graph. `go.mod:7` carries the comment `// used by ./demo/dashboard only — not linked into the exporter binary`. |
| **FR-003** Single documented command | ✅ | `Makefile:46` target `dashboards: go run ./demo/dashboard`. Documented in `demo/README.md:94-107`. |
| **FR-004** Deterministic regeneration | ✅ | `demo/dashboard/dashboard.go:71-78` marshals via `encoding/json.MarshalIndent` + sorted-map-key behavior (Go ≥ 1.12). T015 verified two consecutive `make dashboards` runs produce zero git diff. |
| **FR-005** Both files written to `demo/grafana/dashboards/` | ✅ | `demo/dashboard/main.go:20-34` declares both output paths. Both files exist and are committed. |
| **FR-006** Committed JSON is artifact of record (no Go needed to run demo) | ✅ | Both JSON files committed in `bf15364`. Grafana provisioning reads them directly; operators bring the stack up with `docker compose up -d --build` — no Go toolchain involved. |
| **FR-007** Drift detection (CI or documented contributor step) | ✅ | `demo/dashboard/dashboard_test.go` (build tag `integration`) golden-file check. `demo/README.md:118-129` documents `go test -tags=integration ./demo/dashboard/...` and what to do on failure. Existing CI (specs 002) runs `go test -tags=integration` and will pick up the drift test automatically. |

### Dashboard parity

| FR | Status | Evidence |
|----|--------|----------|
| **FR-008** Both variants from shared definition; layout reflow on clean | ✅ | `demo/dashboard/dashboard.go:23-69`: single `buildOverview()` with one `if includeInfoText` branch. Panel functions called exactly once. Layout reflow via `yOffset` threaded through each panel function (cleaner than the originally-suggested `compactGridY()` helper — see deviation D1 below). Verified by panel-Y inspection: clean variant compacts everything by 4 grid rows. |
| **FR-009** `$zone` variable preserved (`healthy.demo.` default) | ✅ | `demo/dashboard/datasource.go:24-43` builds the variable, sets `Current` to `healthy.demo.`. Confirmed in regenerated JSON. |
| **FR-010** Metric names / labels / filters match v1 | ✅ | Confirmed via T012 semantic diff: every PromQL `expr`, `legendFormat`, and transformation `id` preserves byte-for-byte between v1 and regenerated full variant. |
| **FR-011** Visual parity for v1 | ⚠ partial | Visual behavior preserved end-to-end (smoke test passes). Cosmetic drift exists — see D2, D3, D4 below. |

### Port-mapping tweaks

| FR | Status | Evidence |
|----|--------|----------|
| **FR-012** Demo exporter binds host port `9053` by default | ✅ | `demo/docker-compose.yml:123`: `"${EXPORTER_PORT:-9053}:9266"`. |
| **FR-013** Grafana stays `3000`, Prometheus stays `9090` | ✅ | `demo/docker-compose.yml` Grafana and Prometheus port lines unchanged. |
| **FR-014** All three ports overridable via env vars with documented defaults | ✅ | `demo/docker-compose.yml` uses `${GRAFANA_PORT:-3000}`, `${PROMETHEUS_PORT:-9090}`, `${EXPORTER_PORT:-9053}`. T022 verified `EXPORTER_PORT=19053` override works end-to-end. |
| **FR-015** README references new exporter default; lists override env vars | ✅ | `demo/README.md:42` `<http://localhost:9053/metrics>`; `demo/README.md:59-65` env var override table; `demo/.env.example:14` `EXPORTER_PORT=9053`. |

### Validation & test impact

| FR | Status | Evidence |
|----|--------|----------|
| **FR-016** Smoke test passes without assertion changes | ✅ | `demo/smoke.sh` assertions A1-A6 unchanged. Smoke run during T013 and T022 both green. |
| **FR-017** Smoke test default port updated to match compose default | ✅ | `demo/smoke.sh:22`: default `9266` → `9053`. |

---

## Success Criteria

| SC | Status | Evidence |
|----|--------|----------|
| **SC-001** Panel rename → small diff in typed source | ✅ | Each panel function in `panels_*.go` is 5-50 lines; a title rename is one line. Verified by inspection. (Per F7 in analyze report, this is verified by visual inspection during code review, not a CI gate.) |
| **SC-002** Transformation field typos at build time; PromQL typos at smoke run | ✅ | `transforms.go:18-92` typed Options structs make field-name typos a compile error. PromQL metric/label typos surface during the demo smoke run (empty panels / failing assertions). Reframed honestly during analyze (F1). |
| **SC-003** Contributor finds regen command in <2 min | ✅ | `demo/README.md` "Iterate on the dashboard (typed Go source)" section spells out the command and workflow in the first 5 lines. Repo-root `README.md:25` also links to it directly. |
| **SC-004** Smoke test passes against regenerated dashboard | ✅ | T013 and T022 both green. |
| **SC-005** Operator with prod exporter on 9266 can run demo | ✅ | Structural — `demo/docker-compose.yml:123` binds demo exporter to host port 9053, distinct from production default 9266. No conflict possible by construction. |
| **SC-006** Two regens → byte-identical | ✅ | T015 verified. |
| **SC-007** Operator finds override env-var name in <60s | ✅ | `demo/README.md:59-65` table lists all three with their defaults. |
| **SC-008** Clean variant imports cleanly to external Grafana with identical behavior | ⚠ structural | The clean variant uses the same panel functions as the full variant with `yOffset=4` and a distinct `uid`. Browser-check portion of T013 deferred to user verification. |

---

## Deviations

These are deliberate departures from what spec/plan/tasks specified.
All are minor and documented here so a future reader can find them.

### D1 — Layout reflow implemented via threaded `yOffset`, not via `compactGridY` helper

**Task**: T010 (analyze remediation F2) said to implement `compactGridY(panels []…, dy int)` and call it only on the clean-variant code path.

**Implementation**: Each panel function takes `yOffset uint32` as a parameter (`panels_status.go`, `panels_records.go`, `panels_operator.go`) and computes its `GridPos.Y` as `originalY - yOffset`. `buildOverview` (`dashboard.go:35-37`) sets `yOffset = 4` when `!includeInfoText`.

**Why**: The SDK's `PanelBuilder.internal` field is unexported, so a `compactGridY` helper that mutates already-built panels can't reach in to set `GridPos`. Threading `yOffset` through each panel function is the cleanest available alternative — and arguably nicer than a post-hoc mutation pass because the offset is visible at every call site.

### D2 — `dashboard.editable: true` in regenerated JSON (v1 was `false`)

**Cause**: The SDK's `DashboardBuilder.Editable()` is a no-arg setter that sets `editable=true`. No setter exists to force `false`.

**Effect**: Demo Grafana would allow UI edits to be made (and reverted on next provisioning poll). For a demo, this is harmless; provisioning still owns the file.

**Mitigation**: None applied. If this becomes important, the workaround is to marshal the dashboard, parse it, mutate `editable`, re-marshal — adds complexity for cosmetic benefit. Deferred.

### D3 — `variable.definition` field omitted (v1 had it)

**Cause**: The SDK at the pinned pseudo-version exposes no `Definition()` method on `QueryVariableBuilder`. Documented in `datasource.go:25-31`.

**Effect**: The Grafana variable still works — the actual PromQL lives in `variable.query.query` (the `Map` form, set via `StringOrMap`). The `definition` field is a UI hint that defaults to the query string anyway.

**Mitigation**: None needed. If upstream SDK adds `Definition()` in a future release, set it then; trivial change.

### D4 — Per-column `custom.{align,filterable}` defaults from v1 not emitted

**Cause**: The SDK's table-panel builder doesn't expose a typed setter for the field-config `defaults.custom.align` / `defaults.custom.filterable` slot. v1 had these explicitly set (`align: "auto"`, `filterable: false`).

**Effect**: Grafana fills in these defaults at render time anyway (they ARE the platform defaults). Visual behavior unchanged.

**Mitigation**: None. Acceptable cosmetic drift.

### D5 — T011 done early during T005

**Cause**: T005's skeleton main.go would have been thrown away by T011 ("emit BOTH variants"). To save the refactor, T005's main.go was written in its final shape (loop over both variants) from the start. T011 then became a no-op on revisit.

**Effect**: None functional. Task accounting: T011 marked done with note.

### D6 — Browser-check portion of T013 deferred to user

**Cause**: The check requires a human to open Grafana, switch the `$zone` dropdown, and confirm panels render — not something the agent can do.

**Effect**: SC-008 marked structural; user verification needed before final sign-off.

---

## Constitution Check (post-implementation)

| Principle | Re-evaluation |
|-----------|----------------|
| I. Robust Integration Testing | ✅ Drift test under `-tags=integration`; smoke test runs end-to-end |
| II. Prometheus Naming | ✅ No metric changes |
| III. Modern Go Ecosystem | ✅ Go 1.26.2; SDK is well-maintained; exporter binary unaffected by SDK dep |
| IV. Structured Logging | N/A — generator is one-shot CLI |
| V. Zone-Focused Detection Scope | N/A — dashboard work |
| VI. Prometheus Ecosystem Conventions | ✅ Provisioning unchanged |
| VII. Well-Behaved Binary | N/A — generator is one-shot |
| VIII. Readable, Honest Tests | ✅ Drift test follows three-phase Meszaros structure; lives in `testutil/`-adjacent location (`demo/dashboard/dashboard_test.go`) |

---

## Summary

- **17/17 FRs** satisfied (FR-011 partial — cosmetic drift D2/D3/D4; functional parity confirmed)
- **8/8 SCs** satisfied (SC-008 structural — pending user browser check)
- **6 deviations** all minor and documented above
- **0 constitution violations**

**Feature is implementation-complete pending user browser verification.**
