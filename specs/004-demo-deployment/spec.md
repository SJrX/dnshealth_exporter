# Feature Specification: Demo Deployment

**Feature Branch**: `004-demo-deployment`
**Created**: 2026-05-11
**Status**: Draft
**Input**: User description: "we want to make a small demo deployment that people can use to test and examine things, specifically I'm talking about docker compose with prometheus and grafana configured, some dns servers (maybe core dns), configured with some samples, and a sample grafana dashboard that is imported so we can iterate quickly on features"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Spin up a working demo with one command (Priority: P1)

A new user (developer evaluating the project, contributor onboarding,
or maintainer doing local iteration) clones the repository, runs a
single command from a `demo/` directory, and within a few minutes has
the exporter scraping a set of pre-configured demo DNS zones, with
Prometheus collecting metrics and Grafana showing a pre-imported
dashboard at `http://localhost:<grafana-port>`.

**Why this priority**: This is the entire point of the feature. If a
user cannot get a functioning end-to-end demo standing up trivially,
nothing else in this spec matters. Everything below is layered on top
of this baseline.

**Independent Test**: From a clean checkout on a host with Docker and
Docker Compose installed, run the documented start command. Within 3
minutes, a browser pointed at the Grafana URL shows the demo
dashboard populated with non-empty data for at least one demo zone.

**Acceptance Scenarios**:

1. **Given** a clean checkout and Docker/Compose installed, **When** the user runs the documented start command, **Then** all services (exporter, Prometheus, Grafana, demo DNS servers) become healthy within 3 minutes.
2. **Given** the stack is running, **When** the user opens Grafana in a browser, **Then** they are signed in (anonymous or default credentials documented in README) and see the pre-imported dashboard listed without manual import.
3. **Given** the stack is running, **When** the user opens the dashboard, **Then** at least one panel shows non-empty data sourced from the exporter for the demo zones.
4. **Given** the stack is running, **When** the user runs the documented stop/teardown command, **Then** all containers, networks, and ephemeral volumes created by the demo are removed and the host is left clean.

---

### User Story 2 - Demo includes representative healthy and unhealthy zones (Priority: P2)

The demo ships with several pre-configured zones served by local
authoritative DNS servers. The set MUST include at least one
deliberately broken / unhealthy zone (e.g., missing glue, mismatched
SOA serials between primaries, NXDOMAIN where a record is expected,
or unreachable nameserver) so that a user can see the exporter
distinguish healthy from unhealthy state on the dashboard without any
extra setup.

**Why this priority**: Without unhealthy examples, the dashboard
looks the same whether the exporter works or not. This is what makes
the demo useful for evaluating the project and for iterating on UI/
metric design. It is P2 because P1 (a working stack) must exist
first; on its own this story has no value.

**Independent Test**: With the demo stack running, the dashboard
clearly distinguishes the healthy zones from the unhealthy ones (e.g.
different colors, alert states, non-zero failure metrics). A user
unfamiliar with the project can identify which zones are problematic
within 30 seconds of looking at the dashboard.

**Acceptance Scenarios**:

1. **Given** the demo stack is running, **When** the user views the dashboard, **Then** at least one zone shows healthy status across all configured checks.
2. **Given** the demo stack is running, **When** the user views the dashboard, **Then** at least one zone shows a clearly distinguishable failing or degraded state for at least one check type.
3. **Given** the demo stack is running, **When** the user inspects metrics in Prometheus directly, **Then** the failure metrics for the unhealthy zone(s) have non-zero values matching what the dashboard displays.

---

### User Story 3 - Fast iteration loop for feature development (Priority: P2)

A maintainer working on the exporter wants to rebuild the exporter
container with their local code changes and see the new behavior
reflected in the demo (metrics, dashboard panels) without restarting
Prometheus, Grafana, or the demo DNS servers, and without losing the
zone configuration or dashboard state.

**Why this priority**: This is the "iterate quickly on features"
requirement called out in the user input. It directly determines
whether the demo is useful to the team day-to-day or is a one-shot
showcase. P2 because the demo is still useful for external evaluators
without it, but it is the primary value driver for the team.

**Independent Test**: Make a trivial code change to the exporter,
run the documented rebuild command, and within 30 seconds observe
the change reflected in metrics scraped by Prometheus. Demo DNS
servers and Grafana dashboard state are not disturbed.

**Acceptance Scenarios**:

1. **Given** the demo stack is running, **When** the developer runs the documented rebuild command after editing exporter source, **Then** only the exporter container is rebuilt and restarted; other services keep running.
2. **Given** the exporter has been rebuilt, **When** the developer reloads the dashboard, **Then** Grafana retains its dashboard configuration and the panels reflect metrics produced by the new exporter build.
3. **Given** the demo dashboard JSON is edited on disk, **When** Grafana reloads it (either automatically via provisioning or via a documented reload step), **Then** the changes are visible without manually re-importing the dashboard through the Grafana UI.

---

### Edge Cases

- **Port conflicts on the host**: If a host already uses the demo's
  default ports (Grafana, Prometheus, exporter metrics, DNS), the
  failure mode MUST be a clear error message identifying which port
  is in use, not a silent partial start. Documentation MUST explain
  how to override ports.
- **Slow first start**: Container image pulls on first run can take
  longer than the 3-minute health target. The 3-minute success
  criterion is measured AFTER images are present locally; first-run
  pull time is excluded but MUST be documented.
- **Stale dashboard / Prometheus state across runs**: Tearing down
  and restarting the demo MUST result in a deterministic clean state
  by default. Persistent state across restarts (if offered at all)
  MUST be opt-in.
- **DNS port binding (53) on the host**: Many Linux hosts bind port
  53 (systemd-resolved, dnsmasq). The demo MUST NOT require binding
  port 53 on the host; demo DNS servers communicate with the exporter
  over the internal Docker network. The host-facing exposure of demo
  DNS, if any, MUST use a non-privileged port.
- **Time required to populate dashboard**: After services come up,
  the exporter needs at least one probe cycle before the dashboard
  has data. Documentation MUST tell the user how long to wait
  (a single probe interval) before expecting data.
- **Unhealthy demo zones do not break the stack**: The deliberately
  broken zones MUST cause exporter failure metrics to fire but MUST
  NOT cause the exporter container to crash, restart-loop, or block
  scraping of the healthy zones.

## Requirements *(mandatory)*

### Functional Requirements

#### Stack composition

- **FR-001**: The demo MUST live in a dedicated directory in the
  repository (e.g., `demo/`) so that it is clearly separate from
  production deployment artifacts.
- **FR-002**: The demo MUST be launchable via Docker Compose with a
  single documented command and stoppable/torn down with a single
  documented command.
- **FR-003**: The demo MUST include the dnshealth_exporter, a
  Prometheus instance, a Grafana instance, and one or more local
  authoritative DNS servers, all running as containers on a shared
  internal network.
- **FR-004**: The exporter container MUST be built from the local
  source tree by default (not pulled from a remote registry), so
  that local changes can be exercised end-to-end.

#### Demo DNS zones

- **FR-005**: The demo MUST ship with at least one healthy zone
  configured end-to-end (delegation, glue, SOA, NS, A/AAAA records
  consistent across primaries) such that all relevant exporter
  checks succeed for it.
- **FR-006**: The demo MUST ship with at least one zone that
  exhibits a clearly observable failure mode the exporter is
  designed to detect (e.g., mismatched SOA serial across primaries,
  missing glue, NXDOMAIN, unreachable nameserver, or similar).
  Failure modes MUST be selected to cover a representative subset
  of exporter check types, not just one.
- **FR-007**: All demo zones MUST be served by DNS servers running
  inside the compose stack; the demo MUST NOT depend on querying
  any zones on the public internet to demonstrate its core value.

#### Prometheus

- **FR-008**: Prometheus MUST be pre-configured to scrape the demo
  exporter container at a sensible default interval suitable for
  iteration (short enough that changes appear within a probe cycle
  without being so aggressive as to mask issues).
- **FR-009**: Prometheus configuration MUST be provisioned from
  files committed to the repository; users MUST NOT need to edit
  configuration through the Prometheus UI to use the demo.

#### Grafana

- **FR-010**: Grafana MUST come up with the demo Prometheus
  instance pre-configured as a data source, with no manual setup
  required by the user.
- **FR-011**: Grafana MUST come up with at least one dashboard
  pre-imported via provisioning, showing the most useful exporter
  metrics for the demo zones (overall zone health, per-check
  status, and at least one example of a per-nameserver breakdown).
  The dashboard MUST be laid out as a per-zone "report card"
  inspired by intodns.com — categorised tables (Parent / NS / SOA
  for v1) where each row is one test, with separate **status**
  tables (boolean PASS/FAIL with color coding) and **records**
  tables (raw data dumps showing the actual NS records, glue,
  per-NS SOA values, etc.). The dashboard MUST include a
  templating variable that lets the viewer switch the displayed
  zone without editing queries. Operator/debug time-series panels
  (probe cycle duration, query rate, cache hit ratio, etc.) MUST
  be present but collapsed by default.
- **FR-012**: The dashboard JSON MUST live in the repository under
  the demo directory and MUST be the source of truth — exporting
  from the Grafana UI back to the file MUST be a documented step
  for maintainers iterating on the dashboard.
- **FR-013**: Grafana access MUST be reachable in a browser without
  the user needing to look up generated credentials or run extra
  setup commands. Either anonymous read access or a documented
  default username/password is acceptable.

#### Iteration workflow

- **FR-014**: The demo MUST support rebuilding the exporter image
  from local source and restarting only that container without
  restarting Prometheus, Grafana, or the demo DNS servers, via a
  single documented command.
- **FR-015**: The demo MUST behave deterministically across teardown
  and restart cycles: by default, restarting the stack from a clean
  teardown MUST produce the same demo state, with no leftover data
  from previous runs influencing the result.

#### Documentation

- **FR-016**: The demo MUST include a README in its directory
  documenting: prerequisites, the start command, the stop/teardown
  command, the URL and credentials for Grafana, the URL for
  Prometheus, the URL for the exporter metrics endpoint, the
  expected wait time before data appears, the rebuild-only-the-
  exporter workflow, the list of demo zones with their intended
  health state, and how to override ports if conflicts occur.
- **FR-017**: The top-level project README MUST link to the demo
  README so new users can discover it.

#### Non-goals (explicit)

- **FR-018**: The demo MUST NOT be presented as a production-ready
  deployment template. The README MUST state explicitly that the
  demo is for evaluation and development only.

### Key Entities

- **Demo stack**: The set of containers (exporter, Prometheus,
  Grafana, demo DNS servers) and the Docker Compose definition that
  ties them together on a shared network.
- **Demo zone**: A DNS zone configured in the demo's authoritative
  DNS server(s) with a known intended health state (healthy or a
  specific failure mode), referenced by the exporter's configuration
  and visible on the dashboard.
- **Exporter demo config**: The dnshealth_exporter configuration
  file used by the demo, listing the demo zones and pointing at the
  in-stack authoritative servers.
- **Provisioned dashboard**: A Grafana dashboard JSON file in the
  repository that Grafana loads automatically on startup via its
  provisioning mechanism.
- **Provisioned data source**: The Prometheus data source definition
  loaded automatically by Grafana on startup.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user with Docker and Compose installed and the demo
  images already pulled can go from `git clone` to a populated
  Grafana dashboard in under 5 minutes of wall time, with no manual
  configuration steps beyond the documented start command.
- **SC-002**: The demo dashboard, on a fresh start, shows
  meaningfully different visual state for healthy versus unhealthy
  demo zones such that a viewer unfamiliar with the project can
  correctly identify which zones are problematic within 30 seconds.
- **SC-003**: A maintainer can apply a code change to the exporter,
  rebuild only the exporter container, and observe the change
  reflected in scraped metrics within 60 seconds of issuing the
  rebuild command.
- **SC-004**: Tearing down and restarting the demo from a clean
  state produces identical exporter health metrics on every run
  (same zones healthy, same zones unhealthy, same failure modes).
- **SC-005**: The demo runs successfully on a host that is already
  using port 53 (e.g., a typical Linux desktop with
  systemd-resolved) without requiring the user to disable any host
  services.

## Assumptions

- The demo's intended audience is developers and evaluators who
  already have Docker and Docker Compose installed; installing those
  is out of scope and can be referenced via upstream documentation.
- A single Compose file (or a small set of files in one directory)
  is sufficient; multi-environment overlays (dev/staging/prod
  variants of the demo) are out of scope for this feature.
- CoreDNS is a reasonable default for the local authoritative DNS
  server based on the user's suggestion, but the spec is agnostic
  to the specific DNS server implementation provided it can serve
  the configured zones with the required (mis)configurations.
- Grafana anonymous viewer access (or a fixed `admin/admin` default)
  is acceptable for a local demo; production-grade auth is
  explicitly out of scope.
- Persistent volumes for Prometheus and Grafana state are not
  required by default — the demo is designed to be torn down and
  recreated freely. Optional persistence may be added later but is
  not part of this feature.
- The demo is intended to run on a single host (developer laptop or
  a single shared evaluation VM). Distributed or clustered
  deployment is out of scope.
- Only the most common DNS check failure modes the exporter already
  supports need to be represented in the broken demo zones. Adding
  new exporter check types specifically to populate the demo is out
  of scope; the demo exercises what the exporter already does.
- The dashboard panel set is expected to evolve as the exporter
  grows; the v1 dashboard only needs to cover currently exported
  metrics that show zone-level and per-check health.
