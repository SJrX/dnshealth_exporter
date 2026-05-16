---

description: "Task list for feature 005-dashboard-go-sdk"
---

# Tasks: Port demo dashboard to Grafana Foundation SDK (Go) + port-mapping tweaks

**Input**: Design documents from `/specs/005-dashboard-go-sdk/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Integration testing is required by the constitution (Principle I,
Principle VIII). For this feature the integration surface is the
golden-file drift test described in `research.md` R-7. Unit tests
are not requested.

**Organization**: Tasks are grouped by user story so each story is
implementable, testable, and deliverable independently.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- All paths are repo-relative

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Pull in the Grafana Foundation SDK and put the
generator skeleton in place.

- [X] T001 Add the Grafana Foundation SDK dependency: from repo root, run `go get github.com/grafana/grafana-foundation-sdk/go@v11.6.x+cog-v0.0.x` and verify `go.mod` lists it under `require` and `go.sum` has matching entries. Add a one-line trailing comment to that require line: `// used by ./demo/dashboard only — not linked into the exporter binary` (per research R-8).
- [X] T002 Create `demo/dashboard/` directory and add a Makefile target at repo root. The Makefile (which may not yet exist) MUST contain a `dashboards:` target that runs `go run ./demo/dashboard` from repo root, plus a `.PHONY: dashboards` line. If a `Makefile` already exists, append the target without disturbing existing targets.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Helper code every panel function depends on
(transformations wrapper, datasource constant, `$zone` variable
helper, generator skeleton). Implement BEFORE any user-story phase.

- [X] T003 Create `demo/dashboard/transforms.go` defining: (a) string constants `TransformJoinByField = "joinByField"`, `TransformOrganize = "organize"`, `TransformFilterByValue = "filterByValue"`, `TransformReduce = "reduce"`; (b) typed option structs `JoinByFieldOptions`, `OrganizeOptions`, `FilterByValueOptions`, `ReduceOptions` with the JSON-tagged fields used in the v1 dashboard (read `demo/grafana/dashboards/dnshealth-overview.json` to enumerate them); (c) constructor helpers `JoinByField(JoinByFieldOptions) dashboard.DataTransformerConfig` etc. that return a `dashboard.DataTransformerConfig` with the matching `Id` and `Options` set to the typed struct. Per data-model.md Entity 4.
- [X] T004 Create `demo/dashboard/datasource.go` defining a package-level `prometheusDS = common.DataSourceRef{Type: cog.ToPtr("prometheus"), Uid: cog.ToPtr("dnshealth-prometheus")}` and a helper `zoneVariable() *dashboard.QueryVariableBuilder` that returns the `$zone` template variable per data-model.md Entity 2 (driven by `label_values(dnshealth_query_success, zone)`, default `healthy.demo.`).
- [X] T005 Create `demo/dashboard/main.go` skeleton (also did T011's both-variants wiring early to avoid refactor; T011 was a no-op on revisit): a `main()` that calls `buildOverview(true)` (full variant), marshals via `json.MarshalIndent(dashboard, "", "  ")`, appends a single trailing newline, and writes via `os.WriteFile` to `demo/grafana/dashboards/dnshealth-overview.json`. Errors → `panic(err)` (idiomatic for a code generator; per plan.md Constitution table, Principle IV note). Stub `buildOverview(includeInfoText bool) (dashboard.Dashboard, error)` in `demo/dashboard/dashboard.go` that for now returns `dashboard.NewDashboardBuilder("DNS Health Overview").Uid("dnshealth-overview").Build()` — empty dashboard, just enough to validate the round-trip compiles and writes a file. After this task, `make dashboards` MUST succeed and produce a (mostly empty but valid) JSON file.

**Checkpoint**: `make dashboards` runs end-to-end, produces a valid JSON file. SDK is wired in.

---

## Phase 3: User Story 1 (P1) — Typed source replaces v1 dashboard with full panel parity, both variants emitted

**Story Goal**: A maintainer can edit the dashboard as Go source.
The typed source emits two equivalent dashboard JSON files (full +
clean) from one shared definition, preserving every panel and the
`$zone` variable from the v1 dashboard. Renaming a panel is a small
diff; misspelling a metric name is a build error.

**Independent Test**: Replace the v1 `dnshealth-overview.json` with
the regenerated file. Bring the demo up. Switch `$zone` through all
demo zones. Every panel from the v1 dashboard renders with
equivalent data. Then rename a single panel in the typed source and
regenerate — the JSON diff is confined to that one panel's `title`.

- [X] T006 [P] [US1] Create `demo/dashboard/panels_info.go` with `infoTextPanel() *text.PanelBuilder` reproducing the markdown info-text panel from the v1 dashboard (read `demo/grafana/dashboards/dnshealth-overview.json` to extract the markdown content, panel title, GridPos, Mode, Code/Content fields).
- [X] T007 [P] [US1] Create `demo/dashboard/panels_status.go` with three functions returning `*table.PanelBuilder`: `parentStatusTable()`, `nsStatusTable()`, `soaStatusTable()`. Each builds the corresponding "Parent / NS / SOA status" table from v1, including all PromQL queries (use `prometheus.NewDataqueryBuilder()` from research R-2, referencing `prometheusDS` from T004), all transformations (use the helpers from T003), GridPos, fieldConfig (color thresholds, value mappings), and panel title. Match the v1 JSON exactly (preserve PromQL strings byte-for-byte to avoid changing semantics).
- [X] T008 [P] [US1] Create `demo/dashboard/panels_records.go` with three functions returning `*table.PanelBuilder`: `parentNSRecordsTable()`, `selfNSRecordsTable()`, `soaSerialsTable()`. Same approach as T007 — read the v1 JSON, reproduce queries / transforms / fieldConfig / GridPos / titles via the SDK builders.
- [X] T009 [P] [US1] Create `demo/dashboard/panels_operator.go` with four functions returning `*timeseries.PanelBuilder`: `probeCycleDurationTimeseries()`, `dnsQueryRateTimeseries()`, `cacheHitRatioTimeseries()`, `delegationCacheTimeseries()`. Same approach — match v1 panel definitions via SDK builders.
- [X] T010 [US1] Replace the stub in `demo/dashboard/dashboard.go` with one shared builder used by **both** variants. Signature: `func buildOverview(uid, title string, includeInfoText bool) (dashboard.Dashboard, error)`. Implementation chains `dashboard.NewDashboardBuilder(title).Uid(uid)`, sets `Tags`, `Editable`, `Refresh`, `Time`, `Timezone` to match v1 root keys, attaches `zoneVariable()` from T004 via `.WithVariable(...)`, then **conditionally** `.WithPanel(infoTextPanel())` (the *only* per-variant branch), then attaches the three status tables, three records tables, and a `dashboard.NewRowBuilder("Operator")` row containing the four operator timeseries (collapsed by default — set `.Collapsed(true)`). Returns `.Build()`. **Code-reuse contract**: each panel function from T006–T009 MUST be called exactly once in this function. There MUST NOT be parallel panel chains for "full" vs "clean"; the only thing that differs between variants is the three function arguments and the one `if includeInfoText` line. If implementing this naturally produces duplicated panel listings, stop and refactor — the entire point of the typed source (FR-008, research R-5) is that adding a panel touches one place. **Layout reflow**: when `includeInfoText` is false, compact each remaining panel's `GridPos.Y` by the info panel's height. Implement as a tiny helper `compactGridY(panels []…, dy int)` in `dashboard.go`, called only on the clean-variant code path. Per FR-008 (b).
- [X] T011 [US1] Update `demo/dashboard/main.go` (done early during T005) to emit BOTH variants from the single `buildOverview` function (replacing the Phase-2 single-variant skeleton). The body MUST be ~6 lines: build full → write full → build clean → write clean. Use a small `[]struct{ uid, title, path string; includeInfoText bool }` slice and loop, or two consecutive `buildOverview(...)` + `os.WriteFile(...)` pairs — implementer's choice, but the loop form is preferred because it makes the "same builder, different args" point visually obvious. Concretely: `("dnshealth-overview", "DNS Health Overview", "demo/grafana/dashboards/dnshealth-overview.json", true)` and `("dnshealth-overview-clean", "DNS Health Overview (clean)", "demo/grafana/dashboards/dnshealth-overview-clean.json", false)`. Per contracts/dashboard-output.md and contracts/generator-cli.md.
- [X] T012 [US1] Done — semantic diff: 12 panels preserved, all titles/exprs/legendFormats/transformation IDs identical. Text diff large (~1000 lines) but cosmetic (SDK defaults + key reordering + schemaVersion 38 → 41). Original: Run `make dashboards` from repo root. Diff the regenerated `demo/grafana/dashboards/dnshealth-overview.json` against the previous git HEAD version. Differences SHOULD be limited to: SDK-imposed key ordering, `schemaVersion`, `id` field renumbering, and any field-default normalization the SDK applies. There MUST NOT be any panel-data difference (titles, queries, transformation IDs, transformation options) — those preserve from v1 exactly. If a panel-data diff appears, investigate and fix the corresponding panel function from T006-T009.
- [X] T013 [US1] Smoke test passed (all assertions A1-A6 green). Browser-check portion (open Grafana, confirm both dashboards appear, switch $zone) is a manual user step — pending. The smoke test MUST pass with zero modifications to its assertions (FR-016). Then open Grafana in a browser, confirm the **two** dashboards (`DNS Health Overview` and `DNS Health Overview (clean)`) appear in the dashboard list, open each, switch `$zone` to `healthy.demo.`, and confirm panels render data in both. Tear down with `docker compose down -v`. If the smoke test fails or panels are empty in either dashboard, fix and re-run. (Validates SC-004 + SC-008.)
- [X] T013b [US1] Stage and commit both regenerated dashboard files: `git add demo/grafana/dashboards/dnshealth-overview.json demo/grafana/dashboards/dnshealth-overview-clean.json && git status` to confirm the clean variant is now tracked. Per FR-006: the committed JSON is the artifact of record; operators MUST NOT need a Go toolchain to bring up the demo.

**Checkpoint**: Both dashboard JSON files exist, are committed (T011 wrote them, but the working tree must be checked), the demo's full dashboard renders identically to before, and the smoke test passes. The clean variant is browsable in Grafana under its own UID.

---

## Phase 4: User Story 2 (P1) — One documented command regenerates both dashboards; drift is caught by a test

**Story Goal**: A contributor finds the regeneration command in the
demo README in under a minute, runs it, and the JSON files update.
A test catches the case where someone hand-edits the JSON without
re-running the generator.

**Independent Test**: Hand-edit `demo/grafana/dashboards/dnshealth-overview.json`
(e.g., change a panel title). Run `go test -tags=integration ./demo/dashboard/...`.
The test fails with a diff and a "run `make dashboards` to fix" hint.
After running `make dashboards`, the test passes again.

- [X] T014 [US2] Create `demo/dashboard/dashboard_test.go` under build tag `//go:build integration`. The test follows the project's three-phase Meszaros structure (see CLAUDE.md and constitution Principle VIII): **Fixture Setup** — define the two output paths and matching `buildOverview(includeInfoText)` calls in a slice of structs `{path, includeInfoText, uid, title}`. **Exercise SUT** — for each case, call `buildOverview`, marshal via the same `json.MarshalIndent` + trailing newline as `main.go`, read the committed file via `os.ReadFile`. **Verification** — `bytes.Equal(generated, committed)`. On mismatch, print the path, a short hint ("dashboard JSON drifted from generator source — run `make dashboards` to regenerate"), and `t.Errorf` (not `t.Fatalf`) so both files are checked in one run. To keep the marshal logic in one place, factor it into a small `marshalDashboard(d dashboard.Dashboard) []byte` helper in `dashboard.go` and call it from both `main.go` and the test.
- [X] T015 [US2] Verify `make dashboards` reproduces both committed JSON files byte-identically (verified — git diff after two consecutive runs returned 0 changes): run twice in a row, then `git diff --stat -- demo/grafana/dashboards/`. Output MUST be empty (zero changes). This validates SC-006 (deterministic regeneration). If diff is non-empty, investigate non-determinism source (sorted-map order, time-of-day in defaults, etc.) and fix.
- [X] T016 [US2] Update `demo/README.md`: add a "Dashboard maintenance" section documenting (a) the regeneration command (`make dashboards` + the alternative `go run ./demo/dashboard`), (b) the two emitted files and their purpose (full vs clean), (c) the drift-test command (`go test -tags=integration ./demo/dashboard/...`) and what to do when it fails, (d) where the typed source lives (`demo/dashboard/`). Keep it concise — under 30 lines. Per quickstart.md.
- [X] T017 [US2] Smoke-check the drift detector by hand (verified — sed-edit title → test fails with useful hint; revert → test passes again): temporarily edit a panel title in `demo/grafana/dashboards/dnshealth-overview.json`, run `go test -tags=integration ./demo/dashboard/...`, confirm test fails with a useful message. Revert the edit. Confirm test passes again. Document this manual check passed in the commit message; do NOT add it as an automated test (it would fight itself).

**Checkpoint**: Two dashboards exist, regenerable in one command, defended by a drift test, documented in `demo/README.md`. US2's MVP is live.

---

## Phase 5: User Story 3 (P2) — Demo exporter port default 9266 → 9053; all overridable

**Story Goal**: Operators with a production `dnshealth_exporter`
already bound to `9266` can run the demo without conflict; the demo
exporter binds to `9053` (DNS-themed, prober range). Grafana 3000
and Prometheus 9090 stay on defaults but remain overridable.

**Independent Test**: With another exporter on `9266`, run
`cd demo && docker compose up -d --build && curl -fsS http://localhost:9053/metrics`.
Both succeed.

- [X] T018 [P] [US3] Edit `demo/docker-compose.yml` to change the exporter service's host-port mapping from `${EXPORTER_PORT:-9266}:9266` to `${EXPORTER_PORT:-9053}:9266`. Container-internal port stays `9266`. Confirm Grafana (`${GRAFANA_PORT:-3000}:3000`) and Prometheus (`${PROMETHEUS_PORT:-9090}:9090`) lines are unchanged. Per contracts/port-mappings.md.
- [X] T019 [P] [US3] Edit `demo/smoke.sh` to change `EXPORTER_PORT="${EXPORTER_PORT:-9266}"` to `EXPORTER_PORT="${EXPORTER_PORT:-9053}"`. The downstream `METRICS_URL` line is unchanged (it interpolates the variable). Per contracts/port-mappings.md.
- [X] T020 [P] [US3] Edit `demo/README.md` (also updated `.env.example` default since it advertises the same value): update every URL referencing the exporter from `:9266` to `:9053`. Add (or update if present) a "Port overrides" subsection listing the three env vars in a table: `EXPORTER_PORT` (default `9053`), `PROMETHEUS_PORT` (default `9090`), `GRAFANA_PORT` (default `3000`). Note that the *production* exporter default remains `9266`; only the demo defaults to `9053`.
- [X] T021 [P] [US3] Inspect `README.md` (repo root) — no port refs found; no-op — if it links to or shows the demo exporter URL with `:9266`, update to `:9053`. If no such reference exists, this task is a no-op (still mark complete after verification).
- [X] T022 [US3] Verified end-to-end: smoke.sh passed with new 9053 default; EXPORTER_PORT=19053 override also returned /metrics on first attempt. Original: Run `cd demo && docker compose up -d --build && sleep 30 && ./smoke.sh && docker compose down -v` to validate the new defaults end-to-end. Smoke test must pass. Then run with overrides: `EXPORTER_PORT=19053 docker compose up -d --build && curl -fsS http://localhost:19053/metrics > /dev/null && docker compose down -v` to confirm the override path still works.

**Checkpoint**: Default ports tweaked, overrides documented and verified. US3 done.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final hygiene and the post-implementation audit
required by the constitution's Governance section.

- [ ] T023 Run `go build ./...` from repo root. The exporter binary MUST build cleanly (Principle III, Principle VII). Confirm the SDK dep is in `go.mod` but the exporter binary size has not materially changed (Go strips unused deps at link time). Optional confirmation: `go list -deps ./` should NOT include `grafana-foundation-sdk` (the exporter's `main` does not import it).
- [ ] T024 Run `go vet ./...` and `go test -tags=integration ./...`. Both MUST pass with zero new warnings or failures (constitution: Development Workflow).
- [ ] T025 Update `README.md` (repo root) to add a one-line link from the existing demo section to the regen command (e.g., "Maintainers: regenerate the dashboards via `make dashboards` — see [demo/README.md](demo/README.md#dashboard-maintenance) for details.") if no equivalent link exists.
- [ ] T026 Post-implementation code-vs-spec audit: walk every FR (FR-001 through FR-017) and SC (SC-001 through SC-008) in `spec.md` and confirm the implementation satisfies it. Record any deviations in a new `specs/005-dashboard-go-sdk/audit.md` with file:line citations. Per constitution Governance: "After implementation, a thorough code audit against the spec MUST be performed before declaring the feature complete."

**Checkpoint**: Build clean, tests green, audit complete. Feature ready for review.

---

## Dependencies

```text
Phase 1 (Setup) ──────────────► Phase 2 (Foundational) ─┬─► Phase 3 (US1)
                                                          │      │
                                                          │      ▼
                                                          │   Phase 4 (US2)
                                                          │      │
                                                          ├──────┘
                                                          │
                                                          └─► Phase 5 (US3)  [parallel with US1/US2]
                                                                 │
                                                                 ▼
                                                          Phase 6 (Polish)
```

- **US3 (port tweak)** is independent of US1 and US2 and can ship in parallel — it touches only `docker-compose.yml`, `smoke.sh`, and `README.md`.
- **US2 (regen + drift)** depends on US1's generator producing the two committed JSON files; its drift test compares against them.
- Phase 6 (Polish) requires all three user stories complete.

## Parallel execution opportunities

Within Phase 3 (US1):

- T006, T007, T008, T009 can be authored in parallel — four independent
  files. After all four complete, T010 wires them in.

Within Phase 5 (US3):

- T018, T019, T020, T021 can be authored in parallel — four independent
  files. T022 is the verification step that runs after.

## Implementation strategy

- **MVP** = Phase 1 + Phase 2 + Phase 3 (US1). At MVP, the typed
  source has replaced the v1 JSON, both variants exist, the demo
  smoke test passes. Maintainers can edit the dashboard as code
  even before US2's drift test exists.
- **Iteration 1** = add US2 (regen ergonomics + drift test).
- **Iteration 2** = add US3 (port tweak) — small, independent.
- **Wrap** = Phase 6 polish + audit.

If timeboxed: ship MVP first; US2 and US3 can land in follow-up
PRs without blocking value delivery.

## Format validation

All 26 tasks above conform to the required checklist format:
`- [ ] TNNN [P?] [Story?] Description (with file path)`. Setup,
Foundational, and Polish tasks omit the `[Story]` label as per
spec. Story-phase tasks all carry `[US1]`, `[US2]`, or `[US3]`.
