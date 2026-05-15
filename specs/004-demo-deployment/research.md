# Phase 0 Research: Demo Deployment

This document resolves open design questions surfaced by the spec and the
Technical Context section of `plan.md`. Every decision here has a stated
rationale and lists the alternatives that were considered and rejected.

---

## R-1. How does the exporter resolve which DNS servers to start delegation walks from, and how can the demo redirect that to in-stack containers without depending on the public internet?

**Decision**: Add a single optional `root_servers []string` field to
`config.Config`. When present and non-empty, the field is plumbed through
to `prober.WalkDelegation` as the starting list (replacing the package-
level `var RootServers` default). When absent, behavior is unchanged —
production deployments continue to use the hardcoded public root server
IPs.

**Rationale**:

- `prober/prober.go:14-21` declares `var RootServers = []string{...real root IPs...}`
  and `WalkDelegation` directly uses `RootServers[0]` without going
  through `ResolveAddress`. So the existing `address_overrides` config
  knob, which is consulted only via `ResolveAddress`, cannot be used to
  redirect the initial root query — the override is bypassed.
- The fix is small and surgical: one optional config field, threaded
  through one function signature, with a unit test in `config/` and an
  integration test in `prober/` using `testutil/ReferralServer` as a
  fake root.
- This keeps the exporter's behavior in production deployments
  byte-identical to today (the `var` default still applies when the
  config field is empty), while letting the demo configure a closed
  delegation chain.

**Alternatives considered**:

- *Override `var RootServers` from a build tag or env var only.* Rejected:
  this is what tests do today via `var` mutation, but it's brittle for a
  user-facing demo (no validation, no documentation surface) and is
  exactly the kind of "test backdoor in production code" smell that
  Principle VIII warns against.
- *Map the public root IPs to demo containers using `address_overrides`.*
  Rejected: doesn't work because `WalkDelegation` doesn't route the
  initial root query through `ResolveAddress`. Would require a separate
  code change in `WalkDelegation` to start using `ResolveAddress` for
  root queries — a more invasive refactor than adding the field, and one
  that changes the meaning of `address_overrides` for all users.
- *Drop the offline requirement (FR-007) and let the demo query real
  root servers.* Rejected: violates the spec, introduces non-determinism
  (SC-004 requires identical metrics across restarts), and means the
  demo can't show a fully closed system.

---

## R-2. CoreDNS topology for serving fake root, fake TLD, healthy and unhealthy demo zones.

**Decision** (final, as of Phase 8): Six CoreDNS containers on a
shared user-defined Docker network with fixed IPv4 addresses so the
exporter's delegation walk reaches them deterministically. The
original five-container plan was extended in Phase 8 with the
`coredns-ns-mismatch` container; container names were also renamed to
match descriptive zone names:

| Container | Network alias | Role | Zones served |
|---|---|---|---|
| `coredns-root` (172.31.0.10) | `coredns-root` | Fake root + fake TLD | `.`, `demo.` (delegations to all demo zones) |
| `coredns-healthy` (172.31.0.11) | `ns1.healthy.demo.`, `ns2.healthy.demo.` | Authoritative for healthy zone | `healthy.demo.` |
| `coredns-soa-serial-mismatch-a` (172.31.0.12) | `ns1.soa-serial-mismatch.demo.` | Authoritative primary, SOA serial=100 | `soa-serial-mismatch.demo.` |
| `coredns-soa-serial-mismatch-b` (172.31.0.13) | `ns2.soa-serial-mismatch.demo.` | Authoritative secondary, SOA serial=101 | `soa-serial-mismatch.demo.` |
| `coredns-lame-nameserver` (172.31.0.14) | `ns1.lame-nameserver.demo.` | Lame nameserver — parent delegates here but container is a CoreDNS forwarder, not authoritative for the zone | — (no auth zones; SOA queries return no answer, surfacing as `query_success{check="soa"}=0`) |
| `coredns-ns-mismatch` (172.31.0.15) | `ns1.ns-mismatch.demo.` | Authoritative for `ns-mismatch.demo.` but reports DIFFERENT NS hostnames in its zone file than the parent advertised — exercises the glue prober's `source="self"` emission path | `ns-mismatch.demo.` |

**Rationale**:

- One root container is enough because the exporter walks `.` → `demo.`
  → target zone in two referrals; no need for an intermediate TLD
  container.
- Two soa-serial-mismatch containers are required to demonstrate
  divergent SOA serials between primaries — that's a multi-source check
  by definition.
- The lame-nameserver container was originally intended to demonstrate
  RA=1 advertised by an "authoritative" server. CoreDNS's `forward`
  plugin doesn't reliably set RA on referral responses without a real
  recursive upstream (verified with `dig`). The container is still
  useful — it demonstrates "auth NS that does not answer SOA
  authoritatively", a real failure mode caught by the SOA check.
- The ns-mismatch container (added in Phase 8 / T059) demonstrates a
  parent-vs-self NS-record divergence, exercising the glue prober's
  `source="self"` emission path that wasn't surfaced by any other
  zone in the original catalogue.
- Static container IPs in the Compose network let the root zone's NS+A
  glue records reference fixed addresses, which keeps the zone files
  human-readable and reproducible (SC-004).

**Alternatives considered**:

- *Single CoreDNS instance with multiple bind addresses.* Rejected:
  CoreDNS does support multi-bind via separate server blocks, but the
  divergent-SOA case requires two genuinely independent answers for the
  same zone, which isn't possible from a single CoreDNS process.
- *Use BIND or NSD instead of CoreDNS.* Rejected: CoreDNS is the
  user's stated preference, has a small image, easy zone-file
  configuration, and is widely understood by the target audience. No
  feature gap blocks it.
- *Use the existing `testutil/` `miekg/dns` server as the demo's DNS
  backend.* Rejected: `testutil/` is for in-process Go tests; running it
  in containers would mean shipping a small Go program just for the
  demo, which is more complex and less idiomatic than CoreDNS.

---

## R-3. Broken-zone catalog — which failure modes ship in v1?

**Decision** (final, as of Phase 8): Four deliberately broken zones
plus one healthy baseline, each chosen to exercise a different
existing prober or path through the glue prober:

| Zone | Failure mode | Prober / path that flags it | Visible signal |
|---|---|---|---|
| `healthy.demo.` | None (control case) | all probers | All `dnshealth_query_success` series = 1; both Records tables show identical NS hostnames; one SOA serial across NSs |
| `soa-serial-mismatch.demo.` | Two primaries reporting different SOA serials (100 vs 101) | `soa` | `dnshealth_soa_serial` differs between `nameserver` labels; "All NSs report same SOA serial" status = FAIL |
| `missing-glue.demo.` | Parent (`demo.`) lists `ns1.missing-glue.demo.` as NS but provides no A glue, and the hostname does not resolve | delegation walk in `prober/prober.go` | Delegation walk fails; zone produces no metrics at all → does not appear in dashboard `$zone` selector |
| `lame-nameserver.demo.` (originally specced as `recursive.demo.` for "RA=1 advertised") | Parent delegates to a server that is not authoritative for the zone (CoreDNS forwarder; no zone file). The originally-spec'd RA=1 demonstration is not feasible — see R-2 rationale. | `soa` | `dnshealth_query_success{check="soa",...}=0`; "NS records — from the zone" panel empty (no self response) |
| `ns-mismatch.demo.` (added in Phase 8 / T059) | Parent advertises one NS hostname; the auth server's zone file lists two different NS hostnames internally | `glue` (specifically the `source="self"` path of `prober.ProbeGlue`) | `dnshealth_ns_record{source="parent"}` and `{source="self"}` carry different `nameserver` label sets; "Parent and self report same NS records" status = FAIL; Records tables show the differing hostnames side-by-side |

**Rationale**:

- Covers all three currently-registered probers (`soa`, `glue`,
  `recursion` — confirmed by reading `prober/{soa,glue,recursion}.go`),
  satisfying FR-006's "representative subset of exporter check types,
  not just one".
- Each failure is a single, isolated misconfiguration so a viewer can
  attribute the failing dashboard panel to one zone and one root cause.
- All five zones together fit in 6 CoreDNS containers and one root
  zone file, keeping resource use low (per Scale/Scope in plan.md).
- The `missing-glue.demo.` case is implemented purely in the root
  container's `demo.` zone file (NS record without glue) — no extra
  CoreDNS container needed for it.
- The `ns-mismatch.demo.` zone surfaces the glue prober's
  `source="self"` path that the original catalogue didn't exercise,
  and is what makes the dashboard's "Parent and self report same NS
  records" status check meaningful.

**Alternatives considered**:

- *Add an "unreachable nameserver" zone.* Rejected for v1: would either
  require an extra container that doesn't run, or pointing at an IP
  that's not in the Compose network — the latter is non-deterministic
  on hosts behind picky firewalls. Can be added later.
- *Add an NXDOMAIN zone.* Rejected: no current prober checks for the
  presence of a specific record beyond SOA, so NXDOMAIN doesn't surface
  as a dashboard signal today. Would be misleading to ship.
- *Add an expired SOA / negative-TTL zone.* Rejected: not currently
  surfaced by any prober.

The Assumption "Adding new exporter check types specifically to populate
the demo is out of scope" rules out inventing a new check just to make a
broken zone visible.

---

## R-4. Prometheus scrape interval for the demo.

**Decision**: Scrape interval `5s`, and configure the exporter with
`probe_interval: 15s`. Document both in the demo README.

**Rationale**:

- SC-003 requires that a code change → rebuild → metric appears within
  60 seconds. With a 60s production probe cycle the worst case is ~60s
  for the next cycle plus build time, blowing the budget. Dropping
  `probe_interval` to 15s leaves headroom.
- A 5s scrape interval at 15s probe cadence means each probe cycle is
  scraped 2–3 times before the next one runs, giving the dashboard
  enough samples to look responsive without misleading the user about
  freshness.
- Both values are well within sane local-network limits (no risk of
  rate-limiting, no risk of overlapping cycles for ~5 zones).

**Alternatives considered**:

- *Default 60s probe interval.* Rejected: blows the SC-003 iteration
  budget.
- *5s probe interval.* Rejected: pointlessly aggressive for a demo and
  would mask any concurrency or cycle-overlap behavior the dev might
  want to inspect.

---

## R-5. Grafana provisioning approach (data sources and dashboards).

**Decision**: Use Grafana's file-based provisioning, mounted read-only
into the container at the conventional paths:

- `./grafana/provisioning/datasources/` →
  `/etc/grafana/provisioning/datasources/` (defines the Prometheus data
  source by URL `http://prometheus:9090`).
- `./grafana/provisioning/dashboards/dashboards.yml` →
  `/etc/grafana/provisioning/dashboards/dashboards.yml` (provider
  config pointing at the dashboards directory).
- `./grafana/dashboards/` → `/var/lib/grafana/dashboards/` (the actual
  dashboard JSON files).

Set `GF_AUTH_ANONYMOUS_ENABLED=true`, `GF_AUTH_ANONYMOUS_ORG_ROLE=Editor`
(so users can interactively tweak panels for iteration), and
`GF_USERS_DEFAULT_THEME=light` for screenshot legibility.

**Rationale**:

- File-based provisioning is the documented, supported way to ship
  pre-configured dashboards and data sources — no API calls or sidecar
  processes required.
- Anonymous Editor access (FR-013) lets a viewer see the dashboard
  immediately without credentials AND lets a developer tweak panels
  during iteration without having to log in. Changes made in-UI are
  intentionally lost on restart, which reinforces FR-012 (the JSON
  file is the source of truth).
- Provisioned dashboards are read-only in their original folder by
  default — Grafana surfaces a "Save As" button instead of "Save",
  which is the right default for "the file is canonical" (FR-012).

**Alternatives considered**:

- *Bake provisioning into a custom Grafana image.* Rejected: adds a
  build step, slows iteration, and makes it harder to edit the
  dashboard JSON during development.
- *API-based dashboard import after startup.* Rejected: adds a
  startup-ordering dependency and a separate "init" container or
  sidecar, for no benefit over file provisioning.
- *Persistent volume for Grafana state.* Rejected: violates the
  "default behavior is deterministic from a clean teardown" requirement
  (FR-015 / SC-004). Persistence can be added later as opt-in.

---

## R-6. Dockerfile and image strategy for the exporter.

**Decision**: Multi-stage Dockerfile at `demo/Dockerfile.exporter`, with
build context `../` (the repo root). Stage 1 uses `golang:1.25` to run
`CGO_ENABLED=0 go build -o /out/dnshealth_exporter .`. Stage 2 uses
`gcr.io/distroless/static:nonroot` and copies just the binary plus a
default config. The Compose service uses `build:` (not `image:`) so the
demo always builds from the local source tree (FR-004); no published
image is referenced.

**Rationale**:

- Multi-stage keeps the runtime image tiny (~10 MB), which makes the
  rebuild loop fast (FR-014, SC-003).
- Distroless `nonroot` means no shell, no extraneous packages,
  unprivileged user — appropriate even for a demo because it models
  reasonable defaults without claiming production readiness.
- `build:` with no `image:` field means `docker compose up --build
  exporter` rebuilds only the exporter service (FR-014); other services
  keep running.
- Building from `../` lets a single Dockerfile see the whole module
  (including `go.mod`, `go.sum`, all source). A `.dockerignore` at
  `demo/` excludes `specs/`, `steve-local/`, and the binary cache to
  keep the build context small.

**Alternatives considered**:

- *Pull a pre-built exporter image from a registry.* Rejected: violates
  FR-004 (must build from local source).
- *Single-stage build with `golang:1.25` as runtime.* Rejected:
  ~800 MB image, slow to rebuild, ships the toolchain.
- *Alpine instead of distroless.* Rejected: no benefit for a static
  Go binary, and distroless gives a smaller, more locked-down image.

---

## R-7. Port allocation and host-side exposure.

**Decision**:

| Service | Container port | Default host port | Override mechanism |
|---|---|---|---|
| Grafana | 3000 | 3000 | `GRAFANA_PORT` in `.env` |
| Prometheus | 9090 | 9090 | `PROMETHEUS_PORT` in `.env` |
| Exporter `/metrics` | 9266 | 9266 | `EXPORTER_PORT` in `.env` |
| CoreDNS containers | 53 | (not exposed to host) | n/a |

The default exporter port `9266` matches the project's existing default
listen port (per `main.go`), keeping consistency with non-demo
deployments. The `.env.example` file documents all three overrides; the
README explains how to copy it to `.env` and edit.

**Rationale**:

- Not exposing CoreDNS port 53 to the host is the explicit fix for
  edge case "DNS port binding (53) on the host" and SC-005. The
  exporter reaches CoreDNS over the internal Docker network.
- Using `.env`-based overrides is the idiomatic Compose pattern for
  port conflicts on the host (edge case "Port conflicts on the host").
- The exporter port is exposed for the convenience of inspecting
  `/metrics` directly during iteration; not strictly required by the
  spec but listed in FR-016 as a documented URL.

**Alternatives considered**:

- *Expose CoreDNS port 53 on a high host port like 5353.* Rejected:
  not needed for the demo's value (the exporter queries CoreDNS
  internally), and adds confusion. A maintainer who wants to dig at
  CoreDNS interactively can `docker exec` into the exporter container
  or `docker run --rm --network demo_default -it tutum/dnsutils dig ...`
  on the demo network — both are documented in the README.

---

## R-8. Smoke test scope and assertions.

**Decision**: Ship `demo/smoke.sh` as a bash script with `set -euo
pipefail`. It performs:

1. `docker compose up -d --build`
2. Wait (up to 90 s) for the exporter's `/metrics` to respond 200.
3. Wait one full probe cycle (`probe_interval` × 1.5 = ~25 s).
4. `curl /metrics` and assert the presence of expected series for both
   healthy and broken zones (using `grep -F` against known label sets).
5. Assert `dnshealth_dns_queries_total` is non-zero (cycle ran).
6. `docker compose down -v` and assert exit code 0 from the exporter
   container during shutdown.

The expected series are listed in
`specs/004-demo-deployment/contracts/smoke-test.md` so they can be
reviewed during planning, not buried in the script.

**Rationale**:

- Validates SC-001 (start works), SC-002 (broken series exist),
  SC-004 (deterministic from clean state), and SC-005 (no host:53
  binding — implicit because the script doesn't touch the host
  resolver).
- Bash + curl + grep is sufficient for the assertions; a Go
  integration test for this would be both heavier and would not
  exercise the actual deployment artifacts.
- Adding the script to a `make demo-smoke` target makes it
  copy-paste-runnable for CI later, but CI integration is explicitly
  out of scope for this feature (no FR requires it).

**Alternatives considered**:

- *Use Promtool to validate scrape config.* Rejected: only validates
  syntax; doesn't validate that the demo produces the expected series.
  Useful as a pre-check inside `smoke.sh` but not a substitute.
- *Selenium/Playwright check of the Grafana UI.* Rejected: enormous
  dependency for a small payoff. The dashboard JSON's correctness is
  better validated by checking that Prometheus has the series the
  dashboard queries, which is what `smoke.sh` already does.

---

## R-9. Where the dashboard JSON lives and how it gets edited.

**Decision**: The dashboard JSON lives at
`demo/grafana/dashboards/dnshealth-overview.json`. It is the single
source of truth (FR-012). The demo README documents the maintainer
workflow:

1. Edit the dashboard interactively in the Grafana UI (anonymous
   Editor mode lets you do this without login).
2. Use Grafana's "Share → Export → Save to file" UI to download the
   updated JSON.
3. Replace `demo/grafana/dashboards/dnshealth-overview.json` with the
   downloaded file.
4. Commit.

**Rationale**:

- This is the documented Grafana workflow; no automation needed for v1.
- A future improvement (out of scope here) would be a `make
  demo-export-dashboard` that hits Grafana's HTTP API and writes the
  file, but the manual workflow is fine to start.

**Alternatives considered**:

- *Use Grafana's `dashboard.uid` lookup + HTTP API in a Make target.*
  Rejected: nice-to-have, not in scope. README documents the manual
  flow.

---

## Open questions

None. All NEEDS CLARIFICATION items from the Technical Context are
resolved by the decisions above.
