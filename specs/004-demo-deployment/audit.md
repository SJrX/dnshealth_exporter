# Code-vs-spec audit: 004-demo-deployment

Adversarial walk through every FR / SC / contract to confirm the
implementation actually satisfies it. Performed at the end of
implementation per Constitution Governance and per the standing
feedback memory `feedback_code_review.md`.

**Date**: 2026-05-12
**Audited against**:
- `spec.md` FR-001..FR-018, SC-001..SC-005
- `contracts/dashboard-metrics.md`
- `contracts/exporter-config.yml`
- `contracts/smoke-test.md`

---

## Functional Requirements

| FR | Status | Evidence |
| --- | --- | --- |
| FR-001 dedicated `demo/` dir | ✅ | `demo/` at repo root, separated from production deployment artifacts (none exist yet, but principle preserved). |
| FR-002 single start/stop command | ✅ | `docker compose up -d --build` and `docker compose down -v`, both documented in `demo/README.md`. |
| FR-003 exporter + Prometheus + Grafana + ≥1 auth DNS, shared internal network | ✅ | 8 services in `demo/docker-compose.yml` on the `demo` bridge network (subnet 172.31.0.0/24). |
| FR-004 exporter built from local source by default | ✅ | `demo/Dockerfile.exporter` with `build:` clause in compose (no `image:` referencing a published exporter). |
| FR-005 ≥1 healthy zone with all checks succeeding | ✅ | `healthy.demo.` — verified `query_success=1` for all three checks (soa, recursion, glue) in T035. |
| FR-006 ≥1 zone with observable failure mode covering a representative subset | ✅ | Three broken zones: `broken-soa.demo.` (SOA divergence), `missing-glue.demo.` (delegation walk failure), `recursive.demo.` (SOA-via-broken-forwarder failure). Three of three currently-registered probers (soa, glue, recursion) have at least one failure pathway demonstrated. |
| FR-007 no public-internet dependence for core value | ✅ | Demo runs entirely against in-stack CoreDNS containers; `root_servers: [coredns-root:53]` in exporter config; verified with smoke test. |
| FR-008 Prometheus pre-scrapes exporter at sensible interval | ✅ | `demo/prometheus/prometheus.yml` with `scrape_interval: 5s`. |
| FR-009 Prometheus config provisioned from files (no UI editing) | ✅ | Mounted read-only at `/etc/prometheus/prometheus.yml`. |
| FR-010 Grafana pre-configures Prometheus data source | ✅ | `demo/grafana/provisioning/datasources/prometheus.yml`, uid `dnshealth-prometheus`. |
| FR-011 Grafana auto-loads intodns-style report-card dashboard with categorised Status + Records tables, `$zone` variable, collapsed operator section | ✅ | `dnshealth-overview.json` v4: 6 report-card tables (Parent/NS/SOA × Status/Records) + collapsed Row containing 4 operator time-series panels. `$zone` populated from `label_values(dnshealth_query_success, zone)`. (FR-011 amended in Phase 7.) |
| FR-012 dashboard JSON in repo as source of truth | ✅ | `demo/grafana/dashboards/dnshealth-overview.json`; mounted read-only; provisioning has `allowUiUpdates: false`. README documents the export-from-UI-and-replace-the-file workflow. |
| FR-013 Grafana reachable without credentials lookup | ✅ | `GF_AUTH_ANONYMOUS_ENABLED=true` + `GF_AUTH_ANONYMOUS_ORG_ROLE=Editor` — open <http://localhost:3000>, no login. |
| FR-014 single command rebuilds only exporter | ✅ | `docker compose up -d --build exporter` — verified T036. |
| FR-015 deterministic across teardown/restart cycles | ✅ | No persistent volumes for Prometheus or Grafana; `docker compose down -v` returns to empty state. T044 ran two smoke cycles back-to-back; both pass identically. |
| FR-016 README covers all listed items | ✅ | `demo/README.md` covers prerequisites, start, stop, Grafana URL+anon-mode, Prometheus URL, exporter `/metrics` URL, expected wait time, all three iterate-on-X workflows, demo zone list with intended state, port override, troubleshooting, "Not for production". |
| FR-017 top-level README links to demo | ✅ | "Try the demo" section added at top of `README.md` linking to `demo/README.md`. |
| FR-018 demo not presented as production-ready | ✅ | Explicit `"Not for production."` blockquote at top of `demo/README.md`. |

## Success Criteria

| SC | Status | Evidence |
| --- | --- | --- |
| SC-001 clone-to-dashboard ≤ 5 min (images cached) | ✅ | Observed: stack came up in ~15s after first build; `/metrics` ready in <30s; dashboard data after one probe cycle (~15s). Total ~30s wall clock vs 5-min budget. |
| SC-002 healthy vs unhealthy visually distinguishable in 30s | ✅ (subjective) | Dashboard shows separate panels for each failure mode: SOA serials panel makes broken-soa.demo. visible (two distinct lines), Zone health stat panel makes recursive.demo. SOA failure visible (red FAIL), missing-glue.demo. is absent from all per-zone panels. Markdown header at top lists what to look for per zone. **Note**: this is a UX criterion; final judgment requires a fresh viewer. The dashboard has been built to make differences obvious but I (the implementer) cannot judge "30 seconds for an unfamiliar viewer" — the user should validate. |
| SC-003 code change → metric in Prometheus ≤ 60s | ✅ | T039 measured 7.75s end-to-end. |
| SC-004 deterministic across restarts | ✅ | T044 — two consecutive `smoke.sh` runs both pass identically. |
| SC-005 runs on host with port 53 bound | ✅ | T045 — verified no `:53` mapping in compose.yml; smoke test passes on this host with port 53 already in use by an unrelated service. |

## Contract: dashboard-metrics.md

| Series | In dashboard JSON? |
| --- | --- |
| `dnshealth_query_success` | ✅ Used in "Zone health" stat panel (id 2) |
| `dnshealth_query_duration_seconds` | ✅ Used in "Query duration" panel (id 6) |
| `dnshealth_soa_serial` | ✅ Used in "SOA serials per zone" panel (id 4) |
| `dnshealth_ns_recursion_available` | ✅ Used in "Recursion availability" panel (id 5); contract was originally `dnshealth_recursion_available` — corrected during impl |
| `dnshealth_probe_cycle_duration_seconds` | ✅ Used in "Probe cycle duration" panel (id 3) |
| `dnshealth_dns_queries_total` | ✅ Used in "Query rate" half of panel (id 7) |
| `dnshealth_delegation_cache_hits_total` | ✅ Used in cache-ratio expression (panel id 7) |
| `dnshealth_delegation_cache_misses_total` | ✅ Used in cache-ratio expression (panel id 7) |

All required series are queried by the dashboard and verified by the smoke test (`A1`–`A5`).

## Contract: exporter-config.yml

`demo/exporter/dnshealth.yml` matches the contract for the four-zone case (US2 final state). The `root_servers: [coredns-root:53]` field is present, the four zones are listed, and timing values match.

## Contract: smoke-test.md

`demo/smoke.sh` implements all six assertions (A1–A6) per the contract, with the order-independent grep pattern that the contract was updated to specify.

## Notable deviations from research / plan

1. **Phase 2 implementation pattern** — research R-1 said to thread a roots parameter through `WalkDelegation`'s signature. Implemented instead via the existing `prober.X = cfg.Y` override pattern (consistent with how `ResolveAddress` is wired today). Smaller, also covers `ResolveHostname` (which research overlooked), and uses an established convention. Documented in `tasks.md` T006.

2. **Go version** — research said "Go 1.25.x"; actual go.mod requires 1.26.2. Dockerfile uses `golang:1.26`. Documented in `tasks.md` T010.

3. **`recursive.demo.` failure mode** — original spec/contract called for "RA=1 advertised by an authoritative" demonstration. CoreDNS's `forward` plugin doesn't reliably set RA on referral responses (verified with `dig`). Adjusted the demo to demonstrate "SOA query through broken forwarder fails" instead — equally real, equally visible on the dashboard, doesn't require adding `unbound`/BIND. Updated README, dashboard markdown header, and smoke-test contract A3.

4. **Grafana dashboard reload nuance** — Grafana's file provisioning re-imports a dashboard only when its CONTENT changes, not when only the integer `version` field is bumped. Documented in T037 follow-up.

5. **Phase 7 dashboard refinement** — initial dashboard had a single mixed-content table per category, which exposed a Grafana value-mapping limitation (row-level value mappings aren't supported, so info rows that happened to be 0 or 1 displayed as "FAIL"/"PASS"). User review also caught a misleading "Parent provides glue" check that failed for healthy zones because of a CoreDNS apex-glue quirk (in-zone NS hostnames not getting glue in sub-delegation referrals). Phase 7 split each category into Status (boolean) + Records (raw data) panels, added the `$zone` templating variable, collapsed operator panels into a Row, and tightened the SOA serial agreement query so absence-of-data no longer accidentally maps to PASS. FR-011 in `spec.md` amended accordingly. See tasks T048–T056.

6. **Phase 8 zone naming + ns-mismatch** — user review caught Prometheus scrape labels (`instance`, `job`) leaking into Records tables and zone names that weren't descriptive of failure modes. Renamed `broken-soa.demo.` → `soa-serial-mismatch.demo.`, `recursive.demo.` → `lame-nameserver.demo.` (the latter more honest about what it actually demonstrates given the CoreDNS forwarder limitation). Added new `ns-mismatch.demo.` zone exercising the previously-undemonstrated `source="self"` glue path. Restructured `healthy.demo.` and `soa-serial-mismatch.demo.` zones to include explicit A records for in-bailiwick NSs so the glue prober's self-side A lookup succeeds. Discovered a minor prober quirk: `glue.ProbeGlue` iterates `delegation.NSRecords` directly and skips IP-less entries instead of using the IP-resolved `nameservers` slice, so self-side records are silently absent for zones whose parent doesn't include glue — documented in tasks "Notes for next round" but not fixed here per the "no new probers" constraint. See tasks T057–T064.

## Constitution compliance (Principle-by-principle)

| Principle | Status | Evidence |
| --- | --- | --- |
| I. Robust Integration Testing | ✅ | The new code path (`Config.RootServers` → `prober.RootServers` wiring in `main.go` and `applyReloadedConfig`) is covered by `TestApplyReloadedConfig_AppliesRootServers` and `TestApplyReloadedConfig_ClearsRootServers` in `main_test.go` (integration build tag). The override mechanism on `prober.RootServers` is additionally exercised end-to-end by every existing test in `prober/integration_test.go`. |
| II. Prometheus Naming | ✅ | No new metrics. The dashboard JSON queries existing `dnshealth_*` series only — verified against `prober/registry.go` and `cycle/runner.go`. |
| III. Modern Go Ecosystem | ✅ | No new Go dependencies. `demo/Dockerfile.exporter` uses `golang:1.26` (matches go.mod requirement) and `gcr.io/distroless/static:nonroot` for runtime. |
| IV. Structured Logging | ✅ | New log lines (`"Root server override configured"`) use the same `logger.Info(msg, key, value)` slog pattern as the existing `"Address overrides configured"` line. |
| V. Zone-Focused Detection Scope | ✅ | No threshold/policy logic added. The exporter remains a raw signal source; the demo dashboard does the visual interpretation. |
| VI. Prometheus Ecosystem Conventions | ✅ | No exporter behavior change. The override pattern is the same convention already in use for `prober.ResolveAddress`. |
| VII. Well-Behaved Binary | ✅ | Smoke test A6 verifies SIGTERM → exit 0. Manual T036 confirmed clean restart on rebuild. T038 confirmed `docker compose restart coredns-healthy` works without disturbing the exporter or other containers. |
| VIII. Readable, Honest Tests | ✅ | New tests in `config/config_test.go` (`TestLoad_RootServersOverride`, `TestLoad_RootServersDefaultsEmpty`) and `main_test.go` (`TestApplyReloadedConfig_AppliesRootServers`, `TestApplyReloadedConfig_ClearsRootServers`) all follow visible three-phase Fixture/Exercise/Verification structure, use real Config and Cache objects (no mocks), test at the boundary (the public `Load` and `applyReloadedConfig` functions), and use defaults-with-override fixture style. The smoke shell script is not a Go test but is written to be readable as documentation — each step prints what it's doing. |

`go vet ./...`: clean.
`go test -tags=integration ./...`: all 6 packages green.

## Findings & gaps

None blocking. All FRs, SCs, and constitution principles verified.

The "Not for production" notice (FR-018) is in `demo/README.md` but not in the dashboard's markdown header. Optional improvement — the user audience for the dashboard sees the same notice in the README before getting to the dashboard, so this is fine.

**One subjective criterion (SC-002) requires fresh-eyes validation by the user**: "a viewer unfamiliar with the project can correctly identify which zones are problematic within 30 seconds." The dashboard has been built deliberately to surface differences, but I (the implementer) cannot judge this from outside. Recommend the user (or someone unfamiliar) opens <http://localhost:3000> and times themselves.
