# Implementation Plan: Demo Deployment

**Branch**: `004-demo-deployment` | **Date**: 2026-05-11 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/004-demo-deployment/spec.md`

## Summary

Ship a self-contained `demo/` directory that brings up the dnshealth_exporter, a Prometheus instance, a Grafana instance with a pre-imported dashboard, and a small set of in-stack CoreDNS authoritative servers serving deliberately healthy and unhealthy demo zones — all behind a single `docker compose up` command on a shared internal Docker network.

The demo runs entirely offline against a fake root → fake TLD → demo zones hierarchy served by CoreDNS containers. To make this possible without changing exporter check semantics, the exporter gains one new optional config field (`root_servers`) so that `prober.WalkDelegation` can be pointed at the in-stack fake root instead of the hardcoded public root server IPs in `prober/prober.go`. This is the only exporter code change; everything else is deployment artifacts (Compose file, CoreDNS Corefile + zone files, Prometheus config, Grafana provisioning + dashboard JSON, Dockerfile, README).

## Technical Context

**Language/Version**: Go 1.25.x (existing) — exporter binary built from local source via Dockerfile multi-stage build.
**Primary Dependencies (exporter)**: unchanged — `miekg/dns`, `prometheus/client_golang`, `kingpin/v2`, `exporter-toolkit`, `promslog`, `go.yaml.in/yaml/v3`.
**Demo platform dependencies (containers)**: CoreDNS (latest stable), Prometheus (v2.x latest stable), Grafana (latest stable OSS).
**Storage**: None for the demo — Prometheus and Grafana use ephemeral container storage by default. No persistent volumes (per FR-015 / Assumption "Persistent volumes ... not required by default").
**Testing**: Existing Go integration tests (`go test -tags=integration ./...`) cover the new config field. A separate `demo/smoke.sh` deployment smoke test brings the stack up, waits one probe cycle, and asserts expected metric series via `curl` to validate the demo end-to-end.
**Target Platform**: Linux host with Docker Engine and the Docker Compose plugin (Compose v2 syntax). The demo MUST NOT bind privileged ports on the host (per FR / SC-005).
**Project Type**: Existing single-project Go exporter, plus a new top-level `demo/` deployment directory. No restructure.
**Performance Goals**: Demo startup time ≤ 3 minutes after images are present locally (SC-001 derives this from the user-facing 5-minute clone-to-dashboard target). Probe cycle short enough that change-rebuild loop completes in ≤ 60s (SC-003) — implies `probe_interval` of 15–20s for the demo (well below the 60s production default).
**Constraints**: Demo MUST run offline once images are pulled (FR-007). MUST NOT require port 53 on the host (SC-005). Exporter image MUST be built from local source by default (FR-004) — no `:latest` pulls of the exporter.
**Scale/Scope**: Single host, ~5–7 containers (1 exporter, 1 prometheus, 1 grafana, 3–4 coredns), ~5–8 demo zones across one fake TLD.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Evaluated against `.specify/memory/constitution.md` v1.1.1.

| Principle | Status | Notes |
|---|---|---|
| I. Robust Integration Testing | PASS | The one new exporter code path (`Config.RootServers` plumbing into `WalkDelegation`) is covered by an in-process integration test using `testutil/` (a `ReferralServer` configured as a fake root, a config carrying its address, and a probe asserting it was queried). Demo deployment artifacts themselves are not exporter functionality and don't trigger this principle; the `demo/smoke.sh` script validates them at the deployment layer. |
| II. Prometheus Naming Conventions | PASS | No new metrics. The Grafana dashboard consumes existing `dnshealth_*` series only. |
| III. Modern Go Ecosystem | PASS | No new Go dependencies. The Dockerfile uses an official `golang:1.25` builder image and a `gcr.io/distroless/static` runtime image. |
| IV. Structured Logging | PASS | No logging changes. |
| V. Zone-Focused Detection Scope | PASS | The demo monitors zones; no per-host probing or threshold logic added. |
| VI. Prometheus Ecosystem Conventions | PASS | No architectural change to the exporter. The demo is a deployment artifact, not an exporter pattern shift. |
| VII. Well-Behaved Binary | PASS | The demo exercises this — `docker compose down` sends SIGTERM, exporter must shut down cleanly within Compose's stop timeout. The smoke test asserts the container exits cleanly (exit code 0) on teardown. |
| VIII. Readable, Honest Tests | PASS | The new config-plumbing test follows the three-phase structure, uses real `testutil/` fixtures, and verifies behavior at the prober boundary (not internal state). The smoke test is a shell script, not a Go test, so this principle does not apply directly to it — but it is written to be readable as documentation: each step prints what it's doing and why. |

**Verdict**: GATE PASSED. No violations. Proceeding to Phase 0.

## Project Structure

### Documentation (this feature)

```text
specs/004-demo-deployment/
├── plan.md                          # This file
├── spec.md                          # Feature specification
├── research.md                      # Phase 0 — decisions and rationale
├── data-model.md                    # Phase 1 — demo zone topology, provisioning layout
├── quickstart.md                    # Phase 1 — developer-facing workflow
├── contracts/
│   ├── exporter-config.yml          # Demo exporter config (concrete contract)
│   ├── dashboard-metrics.md         # Metric series the dashboard depends on
│   └── smoke-test.md                # What the smoke test asserts
├── checklists/
│   └── requirements.md              # Spec quality checklist (already passes)
└── tasks.md                         # Phase 2 — generated by /speckit.tasks
```

### Source Code (repository root)

```text
config/
├── config.go                        # +RootServers []string field, +applyDefaults wiring
└── config_test.go                   # +TestRootServersOverride

prober/
├── prober.go                        # WalkDelegation accepts root list; var RootServers becomes default only
├── integration_test.go              # +TestWalkDelegation_CustomRoot (uses testutil ReferralServer)
└── ... (unchanged otherwise)

cycle/
└── runner.go                        # Pass cfg.RootServers down to the prober when set

testutil/                            # No new helpers expected; existing ReferralServer covers it
└── ...

demo/                                # NEW — all demo artifacts live here
├── README.md                        # FR-016 — single source of demo docs
├── docker-compose.yml               # FR-002, FR-003 — single Compose file
├── Dockerfile.exporter              # FR-004 — multi-stage build from ../
├── .env.example                     # Documents overridable host ports
├── exporter/
│   └── dnshealth.yml                # Demo exporter config (zones + root_servers + address_overrides)
├── prometheus/
│   └── prometheus.yml               # FR-008, FR-009 — pre-provisioned scrape config
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── prometheus.yml       # FR-010
│   │   └── dashboards/
│   │       └── dashboards.yml       # Provisioning loader
│   └── dashboards/
│       └── dnshealth-overview.json  # FR-011, FR-012 — dashboard JSON, source of truth
├── coredns/
│   ├── root/
│   │   ├── Corefile                 # Serves fake "." and "demo." with delegations
│   │   └── zones/
│   │       ├── root.zone
│   │       └── demo.zone
│   ├── healthy/
│   │   ├── Corefile
│   │   └── zones/healthy.demo.zone  # FR-005 — fully healthy
│   ├── broken-soa-a/
│   │   ├── Corefile
│   │   └── zones/broken-soa.demo.zone  # SOA serial = 100
│   ├── broken-soa-b/
│   │   ├── Corefile
│   │   └── zones/broken-soa.demo.zone  # SOA serial = 101 — divergent
│   └── recursive/
│       └── Corefile                 # No zones; recursive resolver — RA=1 anomaly
└── smoke.sh                         # Brings stack up, waits, asserts /metrics, tears down
```

**Structure Decision**: The exporter code remains a single Go project (existing structure). All demo artifacts live under a new top-level `demo/` directory, isolated from production deployment templates (per FR-001). The Dockerfile lives under `demo/` (not at repo root) to reinforce the non-goal in FR-018 — there is no project-blessed production Dockerfile, only a demo one. The exporter source change is small and surgical (one new optional config field, plumbed through one function signature).

## Complexity Tracking

> No constitution violations to justify. Section intentionally empty.
