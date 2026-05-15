---
description: "Task list for feature: Demo Deployment"
---

# Tasks: Demo Deployment

**Input**: Design documents from `/specs/004-demo-deployment/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Test tasks are included only where the spec or constitution explicitly requires them. The single foundational exporter code change (`root_servers` config plumbing) requires unit + integration tests per Constitution Principles I and VIII; deployment artifacts are validated by `demo/smoke.sh` in the Polish phase rather than by Go tests.

**Organization**: Tasks are grouped by user story (US1, US2, US3) so each story can be implemented and demoed independently.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Maps task to a user story from spec.md (US1, US2, US3)
- All paths are repository-root relative

## Path Conventions

- Single Go project: existing source under `config/`, `prober/`, `cycle/`, `testutil/`, `main.go`
- Demo deployment artifacts: new top-level `demo/` directory (per plan.md)
- Spec artifacts: `specs/004-demo-deployment/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the demo directory skeleton and tooling that the rest of the work fills in.

- [X] T001 Create the `demo/` directory tree per the plan: `demo/{exporter,prometheus,grafana/provisioning/{datasources,dashboards},grafana/dashboards,coredns/{root/zones,healthy/zones,broken-soa-a/zones,broken-soa-b/zones,recursive}}`
- [X] T002 [P] Create `demo/.dockerignore` excluding `specs/`, `steve-local/`, `cache/`, `*.test`, and any built binaries so the Docker build context for `Dockerfile.exporter` (built from `..`) stays small
- [X] T003 [P] Create `demo/.env.example` with `GRAFANA_PORT=3000`, `PROMETHEUS_PORT=9090`, `EXPORTER_PORT=9266` and a top-of-file comment explaining the override workflow

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Add the single exporter code change that lets the demo walk delegation against an in-stack fake root instead of the public root servers (R-1 in research.md). Without this, FR-007 cannot be satisfied and no user story can complete.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete and `go test -tags=integration ./...` is green.

- [X] T004 Add `RootServers []string` field with `yaml:"root_servers"` tag to the `Config` struct in `config/config.go`. Treat empty/nil as "use default"; do NOT mutate `applyDefaults` (no default value — empty means defer to the prober's hardcoded list).
- [X] T005 [P] Add `TestLoad_RootServersOverride` and `TestLoad_RootServersDefaultsEmpty` to `config/config_test.go`: load a YAML config with `root_servers: [...]`, assert the slice round-trips; assert empty when omitted. Three-phase, real objects, no mocks.
- [X] T006 In `prober/prober.go`: split `var RootServers` into `DefaultRootServers` (immutable canonical defaults) + `RootServers = DefaultRootServers` (active list). **Deviation from research R-1**: instead of threading roots through function signatures, used the existing `prober.X = cfg.Y` override pattern that already works for `prober.ResolveAddress` (main.go:54-57). Smaller change, also covers `ResolveHostname` (which research overlooked), consistent with existing convention.
- [X] T007 In `main.go`: after the `AddressOverrides` block, added a parallel `RootServers` block. Updated `applyReloadedConfig` to symmetrically rebind `prober.RootServers` from the new config (or restore `DefaultRootServers` when removed) — same reload-regression handling as `ResolveAddress`.
- [X] T008 Added `TestApplyReloadedConfig_AppliesRootServers` and `TestApplyReloadedConfig_ClearsRootServers` to `main_test.go` (reload behavior is what's new and untested). Pre-existing `prober/integration_test.go` already overrides `prober.RootServers` for every test in the file, so the WalkDelegation code path under override is already integration-tested.
- [X] T009 `go vet ./...` clean; `go test -tags=integration ./...` green for all 6 packages.

**Checkpoint**: The exporter binary now supports an optional `root_servers` config field with full test coverage. Demo work can begin.

---

## Phase 3: User Story 1 - One-command working demo with one healthy zone (Priority: P1) 🎯 MVP

**Goal**: From a clean checkout, `cd demo && docker compose up -d --build` brings up a stack (exporter, Prometheus, Grafana, root CoreDNS, healthy CoreDNS) where Grafana at `http://localhost:3000` shows non-empty data for one healthy demo zone within 3 minutes.

**Independent Test**: Acceptance Scenarios 1, 2, 3, 4 of US1 in spec.md. Concretely: clean clone → start command → after wait, browser at `localhost:3000` shows the demo dashboard listed (no manual import) with at least one panel populated for `healthy.demo.`. `docker compose down -v` leaves the host clean.

### Implementation for User Story 1

- [X] T010 [US1] Created `demo/Dockerfile.exporter` (multi-stage, distroless runtime). **Note**: bumped builder image from `golang:1.25` to `golang:1.26` because go.mod requires `go 1.26.2` — research's "Go 1.25.x" claim was outdated.
- [X] T011 [P] [US1] Created `demo/coredns/root/Corefile` with two server blocks (`.`, `demo.`).
- [X] T012 [P] [US1] Created `demo/coredns/root/zones/root.zone` — fake root delegating `demo.` to `ns1.root.` with A glue at 172.31.0.10.
- [X] T013 [P] [US1] Created `demo/coredns/root/zones/demo.zone` — `demo.` TLD with US1's healthy.demo. delegation; ns1/ns2.demo. glue at 172.31.0.11.
- [X] T014 [P] [US1] Created `demo/coredns/healthy/Corefile`.
- [X] T015 [P] [US1] Created `demo/coredns/healthy/zones/healthy.demo.zone` with SOA, NS, A records.
- [X] T016 [P] [US1] Created `demo/exporter/dnshealth.yml` with single zone + `root_servers: [coredns-root:53]`.
- [X] T017 [P] [US1] Created `demo/prometheus/prometheus.yml` with 5s scrape interval.
- [X] T018 [P] [US1] Created `demo/grafana/provisioning/datasources/prometheus.yml` with uid `dnshealth-prometheus`.
- [X] T019 [P] [US1] Created `demo/grafana/provisioning/dashboards/dashboards.yml` with `updateIntervalSeconds: 10`.
- [X] T020 [P] [US1] Created `demo/grafana/dashboards/dnshealth-overview.json` with markdown header, Zone health stat panel (with FAIL/OK value mappings), and Probe cycle duration time series.
- [X] T021 [US1] Created `demo/docker-compose.yml` with bridge network 172.31.0.0/24, static IPs for both CoreDNS containers matching zone-file glue, all five services. `docker compose config` validates clean.
- [X] T022 [US1] Created `demo/README.md` covering all FR-016 items for US1 + "Not for production" notice (FR-018).
- [X] T023 [US1] **Validated end-to-end**: stack came up in ~15s, `/metrics` exposes `dnshealth_query_success{zone="healthy.demo."}=1` for all three checks (soa, recursion, glue), SOA serial matches zone file, Grafana auto-loaded the dashboard at `/d/dnshealth-overview/dns-health-overview`, Prometheus scrapes successfully. Verified on a host with `systemd-resolved` active — no `:53` host bind anywhere.

**Checkpoint**: US1 (MVP) complete. The demo can be demonstrated end-to-end with a single healthy zone.

---

## Phase 4: User Story 2 - Healthy + unhealthy zones for visible contrast (Priority: P2)

**Goal**: Add three deliberately broken zones (`broken-soa.demo.`, `missing-glue.demo.`, `recursive.demo.`) and the dashboard panels that show their failing state, so a viewer unfamiliar with the project can identify which zones are problematic within 30 seconds.

**Independent Test**: With the stack running after Phase 4, the dashboard contains at least one zone showing healthy status across all checks AND at least one zone showing a clearly distinguishable failing/degraded state, AND the underlying Prometheus metrics for the unhealthy zones have non-zero failure values matching what the dashboard displays.

### Implementation for User Story 2

- [X] T024 [P] [US2] Added NS-without-glue records for `missing-glue.demo.` in `demo/coredns/root/zones/demo.zone`.
- [X] T025 [P] [US2] Added NS+A glue delegation for `broken-soa.demo.` (ns1=172.31.0.12, ns2=172.31.0.13).
- [X] T026 [P] [US2] Added NS+A glue delegation for `recursive.demo.` (ns1=172.31.0.14).
- [X] T027 [P] [US2] Created `demo/coredns/broken-soa-a/{Corefile,zones/broken-soa.demo.zone}` with SOA serial=100.
- [X] T028 [P] [US2] Created `demo/coredns/broken-soa-b/{Corefile,zones/broken-soa.demo.zone}` with SOA serial=101.
- [X] T029 [P] [US2] Created `demo/coredns/recursive/Corefile` (forward plugin → coredns-root). **Note**: CoreDNS `forward` doesn't reliably set RA on referral responses without a real recursive upstream (verified with `dig`: "recursion requested but not available"). Adjusted the demo to demonstrate a different real failure for recursive.demo. — the SOA query through the broken forwarder returns no useful answer (`query_success{check="soa"}=0`). Updated README, dashboard markdown, and smoke-test contract A3 accordingly. The recursion-availability panel still ships for real-world deployments.
- [X] T030 [US2] Added three services to `demo/docker-compose.yml` with static IPs matching glue. Updated exporter `depends_on`.
- [X] T031 [US2] Updated `demo/exporter/dnshealth.yml` to list all four zones.
- [X] T032 [US2] Extended dashboard JSON with SOA serials, Recursion availability, Query duration, and Query rate / cache hit ratio panels. Used `dnshealth_ns_recursion_available` (actual metric name; fixed `contracts/dashboard-metrics.md` which had `dnshealth_recursion_available`).
- [X] T033 [US2] Updated dashboard markdown header to enumerate all four zones with their intended states (using corrected recursive.demo. behavior).
- [X] T034 [US2] Updated `demo/README.md` "Demo zones" table for all four zones.
- [X] T035 [US2] **Validated end-to-end**: stack came up clean, one probe cycle produced all expected series — healthy.demo. all checks=1; broken-soa.demo. SOA serials 100 vs 101 visible as two distinct values for the same zone; missing-glue.demo. has no metrics (delegation walk fails as designed); recursive.demo. shows `query_success{check="soa"}=0` exposing the broken forwarder.

**Checkpoint**: US2 complete. The dashboard now demonstrates the exporter's value by showing a clear contrast between healthy and unhealthy zones.

---

## Phase 5: User Story 3 - Fast iteration loop for feature development (Priority: P2)

**Goal**: A maintainer can rebuild only the exporter container after a code change and see the new behavior in scraped metrics within 60 seconds, without disturbing Prometheus, Grafana, or CoreDNS state.

**Independent Test**: Acceptance Scenarios 1, 2, 3 of US3 in spec.md. Concretely: trivial code edit → `docker compose up -d --build exporter` → metric reflecting the change appears in Prometheus within 60s; other containers' uptimes are unchanged; Grafana retains its dashboard configuration. Editing the dashboard JSON on disk causes Grafana to reload it without manual import.

### Implementation for User Story 3

- [X] T036 [US3] Verified `docker compose up -d --build exporter` rebuilds and restarts only the exporter; other services (Prometheus, Grafana, all five CoreDNS containers) keep running. README "Iterate on exporter code" section documents the command.
- [X] T037 [US3] Verified Grafana picks up content changes to the dashboard JSON within the ~10s `updateIntervalSeconds` window. **Note**: bumping only the `version` integer field in the JSON does NOT trigger Grafana re-import — Grafana compares content. Real edits (panel titles, queries, etc.) reload reliably. README "Iterate on the dashboard" section documents the export-from-UI workflow.
- [X] T038 [US3] Verified zone-file edit + `docker compose restart coredns-healthy` reflects new SOA serial in metrics within 13s end-to-end (0.5s restart + one probe cycle). README "Iterate on demo zones" section documents the flow.
- [X] T039 [US3] **SC-003 measured**: code edit (added log line) → `docker compose up -d --build exporter` → marker visible in container logs in **7.75 seconds**. Well within the 60s budget.

**Checkpoint**: All three user stories complete. The demo is usable both by external evaluators (US1 + US2) and as a daily iteration tool by maintainers (US3).

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Smoke-test automation, top-level discoverability, deterministic-state and host-port verification, and the constitution-mandated post-implementation audit.

- [X] T040 [P] Created `demo/smoke.sh` implementing all six assertions A1–A6. Uses piped `grep -F` instead of single regex with `[^}]*` to handle alphabetical label ordering (Prometheus emits labels in alphabetical order — the original contract regex assumed positional order and would never match). Smoke-test contract updated.
- [X] T041 [P] Added `demo-smoke` target to top-level `Makefile`.
- [X] T042 [P] Added "Try the demo" section at top of top-level `README.md` linking to `demo/README.md`.
- [X] T043 `./smoke.sh` from clean state exits 0 with all assertions passing.
- [X] T044 SC-004: two consecutive smoke runs both exit 0 with identical results.
- [X] T045 SC-005: confirmed no `:53` host port mapping in compose.yml; demo runs successfully on a host with port 53 already in use by an unrelated service (10.240.0.0:53 UDP+TCP bound).
- [X] T046 Adversarial code-vs-spec audit complete — see `specs/004-demo-deployment/audit.md`. All 18 FRs and 5 SCs verified; deviations documented (Phase 2 wiring pattern, Go version bump, recursive.demo. failure mode, dashboard-version-only-edits don't trigger Grafana reload). One subjective criterion (SC-002 "30 seconds for unfamiliar viewer") flagged for user validation.
- [X] T047 Constitution compliance audit complete — all 8 principles pass. `go vet` clean, `go test -tags=integration ./...` all 6 packages green. See `specs/004-demo-deployment/audit.md` § Constitution compliance.

---

## Phase 7: Dashboard refinement — intodns-style report card

Added after initial implementation in response to user review (`Better a few issues`). The original dashboard satisfied FR-011 literally but read more as "generic exporter overview" than as a per-zone health report card. This phase amends FR-011 (in `spec.md`) and rebuilds the dashboard.

- [X] T048 Amended FR-011 in `spec.md` to specify intodns-style layout: per-category tables (Parent / NS / SOA), separate Status (boolean) and Records (raw data) tables per category, `$zone` templating variable, operator panels collapsed by default.
- [X] T049 Added `$zone` Grafana templating variable populated from `label_values(dnshealth_query_success, zone)`. Documented in dashboard markdown header that zones whose delegation walk fails entirely (e.g., `missing-glue.demo.`) won't appear in the selector — the absence is itself the failure signal.
- [X] T050 Replaced the single mixed-content table per category with TWO panels per category — Status (boolean PASS/FAIL only, with color-background value mappings) and Records (raw data via `format: "table"` instant queries). Fixes the value-mapping ambiguity where info rows happening to be 0 or 1 displayed as "FAIL"/"PASS".
- [X] T051 Removed the misleading "Parent provides glue for every listed NS" boolean (it FAILed for `healthy.demo.` because CoreDNS doesn't include apex glue in sub-delegation referrals — a real-DNS quirk, not a health failure). Replaced with the Records table that shows actual glue presence per NS via the `Glue IP` column (empty rendered as "(not provided)").
- [X] T052 Tightened the SOA serial agreement query to distinguish "all NSs report same serial" (PASS) from "no SOA data exists" (FAIL): `((max - min) == bool 0) and on(zone) (count > bool 0) or on() vector(0)`. Previously, the absence of SOA data accidentally mapped to PASS via the missing-data fallback.
- [X] T053 Built SOA per-nameserver detail table by joining 5 instant queries (`dnshealth_soa_serial`, `_refresh_seconds`, `_retry_seconds`, `_expire_seconds`, `_minimum_seconds`) with the `joinByField` transformation on `nameserver`. Renamed Value #A..#E columns to Serial / Refresh (s) / Retry (s) / Expire (s) / Min TTL (s).
- [X] T054 Built NS authoritative-server table joining `dnshealth_query_success{check="soa"}` and `dnshealth_ns_recursion_available` by nameserver, with per-column value mappings (Responded: 0=no/red, 1=yes/green; Recursion: 0=no/green, 1=RA=1/red).
- [X] T055 Wrapped the four operator panels (Probe cycle duration, Query rate / cache ratio, SOA serials over time, Query duration) in a Grafana row panel with `collapsed: true` titled "Operator / debug views". The four panels are inline children of the row in the JSON.
- [X] T056 Verified end-to-end: `docker compose up -d`, dashboard loads via `/api/dashboards/uid/dnshealth-overview` with 8 top-level panels (markdown + 6 tables + 1 collapsed row containing 4 inner panels), `$zone` variable enumerates the 3 zones with metric data, all PromQL spot-checks return expected values for healthy/broken/recursive zones. `smoke.sh` still exits 0.

### Deferred (out of scope; user asked to skip without a new prober)

- AXFR/IXFR support (zone-transfer status) — confirmed via grep that no prober for this exists. User explicitly asked NOT to implement. Would require a new `prober/axfr.go` plus integration tests; tracked here so it isn't lost.

---

## Phase 8: Zone naming + Prometheus label cleanup + parent/self NS check

Added in response to user review: prometheus scrape labels (`instance`, `job`) leaking into Records tables; zones not descriptive of their failure modes; new ns-mismatch zone exercising the previously-undemonstrated `source="self"` glue path.

- [X] T057 Hid Prometheus scrape labels (`instance`, `job`, `__name__`) from all three Records tables (Parent / NS / SOA per-NS) by adding them to `excludeByName` in the `organize` transformations.
- [X] T058 Renamed zones for descriptive failure-mode names: `broken-soa.demo.` → `soa-serial-mismatch.demo.`, `recursive.demo.` → `lame-nameserver.demo.` (the `recursive` name was misleading since CoreDNS's `forward` plugin doesn't set RA — the actual failure is "auth NS that doesn't serve the zone authoritatively"). Kept `healthy.demo.` and `missing-glue.demo.` (already descriptive). Updated CoreDNS dirs/files, Compose service names, exporter config, dashboard markdown, README, smoke.sh, and contracts.
- [X] T059 Added `ns-mismatch.demo.` zone and `coredns-ns-mismatch` container at 172.31.0.15. Parent advertises `ns1.ns-mismatch.demo.` (1 NS, with glue); the auth server's zone file lists `ns-internal-a.ns-mismatch.demo.` and `ns-internal-b.ns-mismatch.demo.` (2 different NSs). Exercises the glue prober's previously-undemonstrated `source="self"` emission path that user thought was missing.
- [X] T060 Added "Parent and self report same NS records" check to NS Status table — PromQL: `((count parent NSs) == bool (count self NSs)) and on(zone) (count(self) > 0) or on() vector(0)`. The `and on(zone) count(self) > 0` guard prevents false PASS when self records don't exist (e.g., for `lame-nameserver.demo.`).
- [X] T061 Restructured `healthy.demo.` zone to use in-bailiwick NSs (`ns1.healthy.demo.`/`ns2.healthy.demo.` with explicit A glue in both the parent's `demo.zone` and the zone's own file). Without this, the glue prober skips the self-side query entirely (it iterates `delegation.NSRecords` and skips entries with empty IP), which would falsely fail the new "parent and self match" check on the control case.
- [X] T062 Added explicit A records for `ns1`/`ns2` in the soa-serial-mismatch zone files so the glue prober's self-side A lookup succeeds. Without these, self-side ns_records weren't emitted for that zone either.
- [X] T063 Added smoke test assertion A3b for ns-mismatch — counts `source="parent"` vs `source="self"` rows for the zone and asserts parent=1, self>=2. Updated `contracts/smoke-test.md` to match.
- [X] T064 Verified end-to-end: smoke.sh exits 0; per-zone "Parent and self match" check correctly reports PASS for healthy + soa-serial-mismatch (NSs identical), FAIL for ns-mismatch (intentional divergence) and lame-nameserver (auth doesn't respond at all).

### Notes for next round

- The glue prober's `source="self"` emission only runs for parent-listed NSs that have an IP. `cycle/runner.go` resolves missing IPs via `prober.ResolveHostname` for the SOA/recursion checks but `prober.ProbeGlue` itself iterates `delegation.NSRecords` directly, skipping IP-less entries — so self-side records are silently absent for zones whose parent doesn't include glue. Filed as **GitHub issue #14**.
- Adding `demo/smoke.sh` to CI as a Compose-based regression check — deferred per merge-review judgment call (Docker-in-CI flake trade-off). Filed as **GitHub issue #15**.
- Single-NS zone, zone with private-IP NSs, AS-diversity check — possible future demo zones if you want broader coverage; existing probers already capture the underlying signals.

---

## Phase 9: Records-table split + per-zone operator panels

Added in response to user review of Phase 8: the unified "NS records — parent vs self" table conflated two distinct viewpoints in one panel and made it hard to read what each side reported. Operator-section time-series panels also showed all zones at once, which didn't follow the `$zone` selector.

- [X] T065 Reverted Panel 5 to a simple "NS records — from parent" table showing only `dnshealth_ns_record{source="parent",zone="$zone"}`. Columns: Nameserver, Glue IP. Empty IP rendered as "(not provided)".
- [X] T066 Rebuilt Panel 6 as "NS records — from the zone": outer-join of three queries — `dnshealth_ns_record{source="self",zone="$zone"}` (refId A), `dnshealth_query_success{check="soa",zone="$zone"}` (refId B), `dnshealth_ns_recursion_available{zone="$zone"}` (refId C) — joined on `nameserver`, then filtered with `filterByValue` `Value #A isNotNull` so only rows that exist in the self-side ns_record metric appear. Columns: Nameserver, IP, Responded, Recursion. For NSs only present on the self side (e.g. `ns-mismatch.demo.`'s `ns-internal-a/b`), Responded and Recursion cells are empty — the exporter only probes parent-listed NSs, so no probe data exists for self-only NSs. This is intentional and accurate.
- [X] T067 Added `{zone="$zone"}` filter to the two operator-section time-series panels that have a per-zone interpretation: "SOA serials per nameserver over time" (id 103) and "Query duration (per check / nameserver)" (id 104). Panel titles now interpolate `${zone}` so the selected zone is visible. The other two operator panels (Probe cycle duration, Query rate / cache hit ratio) remain global because their underlying metrics have no `zone` label.
- [X] T068 Bumped dashboard JSON `version` to 7. Verified end-to-end: `smoke.sh` exits 0; both Records panels render correctly across all four scrapeable zones (healthy, soa-serial-mismatch, lame-nameserver, ns-mismatch); operator panels follow the Zone selector.

### Why two-table split rather than one-table parent-vs-self

Phase 8 / T060 originally rendered a single "NS records — parent vs self" table with a `Source` column. User review found this hard to read for the common case (you wanted to see each side independently with its own probe-result columns where applicable). The two-table layout matches the intodns pattern more directly — left panel "what the parent said", right panel "what the zone said with response info" — at the cost of slightly more dashboard real estate.

---

## Phase 10: Code-review follow-ups

Findings B1, B2, C1, C2, C3, C4, T1 from the post-Phase-9 code review. All low/medium severity, all small. No new functionality.

- [X] T069 (B1) Fixed slice-aliasing footgun: `var RootServers = DefaultRootServers` was a slice-header copy (same backing array). Now `var RootServers = append([]string(nil), DefaultRootServers...)` — independent backing array. Same change applied in `applyReloadedConfig`'s else-branch (restore-defaults path). Doc comment on `RootServers` updated to forbid element-level mutation. Existing reload tests still pass (they compare values, not slice identity).
- [X] T070 (B2) Rewrote `demo/coredns/lame-nameserver/Corefile` header comment. The old comment still claimed "Recursive resolver... The exporter's `recursion` check flags this as an anomaly" — left over from before the Phase 8 rename. New comment honestly describes the lame-nameserver semantics and points at audit deviations #3 and #6 for the why.
- [X] T071 (C1) Rewrote the misleading "Apex glue" comment in `demo/coredns/root/zones/demo.zone`. The previous text claimed coredns-healthy is "authoritative for the apex of the demo. zone" — it isn't (coredns-root serves demo.). Records kept (a valid zone needs A records for its apex NSs); comment now accurately notes they're not exercised by the walker.
- [X] T072 (C2) Removed vestigial `aliases: [ns1.demo., ns2.demo.]` from the `coredns-healthy` Compose service. Healthy.demo. uses in-bailiwick NSs since Phase 8 / T061; the Docker DNS aliases were leftover from before that change and weren't referenced anywhere. Static IP alone is what makes the parent's glue records valid.
- [X] T073 (C3) Set explicit `current` value on the dashboard `$zone` template variable to `healthy.demo.` (was `{}`). First-load now lands on a predictable, healthy zone instead of relying on Grafana's alphabetical-first behaviour.
- [X] T074 (C4) Tightened `smoke.sh` exit-code 3 description in the file header. Header said "teardown reported non-zero exit from any service" but the actual A6 check inspects only the exporter container's exit code. Now reads "exporter container did not exit with code 0 in response to SIGTERM".
- [X] T075 (T1) Added `TestStartup_WiresRootServersFromConfig` to `main_test.go`. Closes the coverage asymmetry where the reload path (`applyReloadedConfig`) was tested but the initial-load gate at `main.go:60-64` was not. Mirrors the conditional from `main()` exactly. Three-phase, real `Config` and `prober.RootServers`.
- [X] T076 Verified end-to-end: `go vet` clean, `go test -tags=integration ./...` green for all 6 packages, `docker compose config` parses cleanly, `smoke.sh` exits 0 with all assertions passing.

### Not addressed in Phase 10 (deferred)

- **T2** (`ResolveHostname` direct override test). The audit acknowledges this as an existing gap; existing prober/integration_test.go tests already exercise the override mechanism for `WalkDelegation` indirectly. Direct `ResolveHostname` coverage requires a fixture where the parent omits glue (forcing the resolution path); not a one-line addition. Tracked here as next-round work.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately.
- **Foundational (Phase 2)**: Depends on Phase 1 (specifically T001 — the demo dir doesn't matter here, but Phase 2 should logically run before any demo artifact references the new config field). T009 (full test pass) is the gate that releases Phase 3.
- **US1 (Phase 3)**: Depends on Phase 2 complete. T010–T020 are mostly parallel; T021 (compose.yml) is the integration point and depends on T010, T002, T011–T020. T022 (README) depends on T021. T023 (validation) depends on everything in Phase 3.
- **US2 (Phase 4)**: Depends on US1 complete (extends compose.yml, exporter config, dashboard JSON, README — none of which exist before US1). T030 depends on T021 + T025/T026 + T027/T028/T029. T034 depends on T031.
- **US3 (Phase 5)**: Depends on US1 complete. Does NOT depend on US2 — could be worked in parallel with US2 if staffed (the iteration workflow is independent of which zones are configured). Listed after US2 for sequential clarity.
- **Polish (Phase 6)**: T040 depends on US1 + US2 complete (the smoke test asserts series for the broken zones too). T043 depends on T040. T044 depends on T040 + T043. T046, T047 depend on all prior phases.

### User Story Dependencies (within the demo-feature scope)

- **US1 (P1)**: Standalone after Phase 2. Standalone-deployable as MVP.
- **US2 (P2)**: Builds on US1 artifacts (extends compose, config, dashboard, README). NOT independently implementable from scratch — but independently *testable* (you can validate US2's acceptance scenarios in isolation by viewing the dashboard).
- **US3 (P2)**: Builds on US1 (needs the running stack to verify rebuild-only and dashboard-reload). NOT dependent on US2.

### Parallel Opportunities

- T002, T003 within Setup: parallel.
- T005 within Foundational: parallel with T006 (different files: test vs prober source); both depend on T004.
- T011–T020 within US1: highly parallel (10 different files, no inter-dependencies). T021 must wait for the file referenced by each Compose mount to exist.
- T024–T029 within US2: parallel. T030 waits for them.
- T036, T037, T038 within US3: parallel (verify-and-document of three independent flows).
- T040, T041, T042 within Polish: parallel.

---

## Parallel Example: US1 implementation

```bash
# After Phase 2 is green, launch all US1 file-creation tasks together:
Task: "Create demo/Dockerfile.exporter (T010)"
Task: "Create demo/coredns/root/Corefile (T011)"
Task: "Create demo/coredns/root/zones/root.zone (T012)"
Task: "Create demo/coredns/root/zones/demo.zone — US1 entries only (T013)"
Task: "Create demo/coredns/healthy/Corefile (T014)"
Task: "Create demo/coredns/healthy/zones/healthy.demo.zone (T015)"
Task: "Create demo/exporter/dnshealth.yml — single zone (T016)"
Task: "Create demo/prometheus/prometheus.yml (T017)"
Task: "Create demo/grafana/provisioning/datasources/prometheus.yml (T018)"
Task: "Create demo/grafana/provisioning/dashboards/dashboards.yml (T019)"
Task: "Create demo/grafana/dashboards/dnshealth-overview.json — MVP panels (T020)"

# Then T021 (docker-compose.yml) integrates them all.
```

---

## Implementation Strategy

### MVP First (US1 only)

1. Phase 1 (Setup) → directories and tooling skeleton.
2. Phase 2 (Foundational) → exporter config field + tests green.
3. Phase 3 (US1) → working stack with one healthy zone.
4. **STOP and VALIDATE**: T023 — confirm SC-001 passes (clone-to-dashboard ≤ 5 min on a host with images pulled).
5. Demo is shippable as MVP at this point.

### Incremental Delivery

1. Setup + Foundational + US1 → MVP demo (one healthy zone, one-command up).
2. Add US2 → demo now shows healthy/unhealthy contrast (the most common evaluator use case).
3. Add US3 → maintainer iteration loop documented and verified.
4. Polish → smoke test, top-level README link, audits.

### Parallel Team Strategy

After Phase 2 is green:

- Developer A: Phase 3 (US1) end-to-end.
- Developer B: Once US1 gets to T021 + T022, start Phase 4 (US2).
- Developer C: Once US1 stack is up, start Phase 5 (US3) verification + docs (independent of US2).
- All converge on Phase 6 (smoke test, audits) once all three stories complete.

---

## Notes

- All file paths are exact and repository-root relative.
- Manual validation tasks (T023, T035, T039, T044, T045) are not skippable — the success criteria in spec.md are wall-clock and visual measurements that automation can't verify.
- T046 (code-vs-spec audit) is mandatory per Constitution Governance, not optional polish.
- Commit after each task or logical group of [P] tasks.
- Stop at any checkpoint to validate the increment independently.
- Keep the dashboard JSON edits in T020 → T032 → T033 in this order; merge conflicts inside dashboard JSON are unpleasant.
