# Feature Specification: E2E Bootstrap

**Feature Branch**: `001-e2e-bootstrap`
**Created**: 2026-04-21
**Status**: Draft
**Input**: User description: "Build first version with end-to-end flow, project bootstrap, build tools and integration test framework"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Operator Scrapes Metrics (Priority: P1)

An operator configures the exporter with one or more DNS zones, starts the binary, and points Prometheus at its `/metrics` endpoint. Prometheus scrapes the endpoint and receives DNS health metrics for the configured zones.

**Why this priority**: This is the fundamental value proposition — without a working scrape endpoint producing real DNS metrics, nothing else matters.

**Independent Test**: Start the exporter configured against the Docker-based test DNS infrastructure, curl `/metrics`, and verify Prometheus-format output containing `dnshealth_` prefixed metrics with zone labels.

**Acceptance Scenarios**:

1. **Given** the exporter is configured with a test zone served by the Docker DNS fixtures, **When** Prometheus scrapes `/metrics`, **Then** the response contains `dnshealth_` prefixed metrics with a `zone` label matching the configured zone.
2. **Given** the exporter is running, **When** Prometheus scrapes `/metrics`, **Then** the response is valid Prometheus exposition format (parseable by `promtool check metrics`).
3. **Given** the exporter is configured with multiple zones, **When** Prometheus scrapes `/metrics`, **Then** metrics are returned for each configured zone.

---

### User Story 2 - Operator Starts and Stops the Exporter (Priority: P2)

An operator starts the exporter binary with standard flags (`--config.file`, `--web.listen-address`, `--log.level`). The binary starts cleanly, logs its startup, and shuts down gracefully on SIGTERM/SIGINT.

**Why this priority**: A well-behaved binary is essential for production deployment, but secondary to actually producing metrics.

**Independent Test**: Start the binary, verify it logs startup and listens on the configured address, send SIGTERM, verify clean exit with code 0.

**Acceptance Scenarios**:

1. **Given** a valid config file, **When** the operator starts the binary with `--config.file=config.yml`, **Then** the binary starts, logs its listen address, and begins serving metrics.
2. **Given** the exporter is running, **When** the operator sends SIGTERM, **Then** the binary shuts down cleanly and exits with code 0.
3. **Given** an invalid config file, **When** the operator starts the binary, **Then** it exits immediately with a non-zero exit code and a clear error message.

---

### User Story 3 - Developer Runs Integration Tests (Priority: P3)

A developer clones the repository, starts the Docker-based DNS fixtures, runs the integration test suite, and gets clear pass/fail results that validate the exporter's prober functions produce correct metrics against real CoreDNS servers.

**Why this priority**: The integration test framework is a key deliverable of this feature, but it supports the other stories rather than delivering direct user value.

**Independent Test**: Start Docker Compose fixtures, run `go test -tags=integration ./...` from the repo root, and verify tests call prober functions against the local CoreDNS containers and assert on metrics registered in a Prometheus registry.

**Acceptance Scenarios**:

1. **Given** the developer has Go installed, **When** they run `go test ./...`, **Then** unit tests pass without network access or Docker.
2. **Given** the developer has Go and Docker, **When** they start the Docker Compose fixtures and run `go test -tags=integration ./...`, **Then** integration tests call prober functions that query the local CoreDNS instances and validate metric output.
3. **Given** a CoreDNS fixture is configured to return an error for a zone (e.g., NXDOMAIN, SERVFAIL), **When** integration tests run, **Then** the test validates the prober handles the failure gracefully and still registers appropriate metrics.

### Edge Cases

- What happens when a configured zone returns NXDOMAIN?
- How does the exporter behave when all configured nameservers are unreachable (timeout)?
- What happens when the config file is empty or contains no zones?
- How does the exporter handle a zone with only one nameserver?
- What happens if Docker/CoreDNS fixtures are not running when integration tests start?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST accept a YAML configuration file specifying one or more DNS zones to monitor.
- **FR-002**: System MUST perform DNS checks against configured zones and expose results as Prometheus metrics on a `/metrics` HTTP endpoint.
- **FR-003**: System MUST use the `dnshealth_` metric namespace prefix for all exported metrics.
- **FR-004**: System MUST include a `zone` label on all zone-specific metrics.
- **FR-005**: System MUST expose a `dnshealth_build_info` gauge with version metadata.
- **FR-006**: System MUST start with `--config.file`, `--web.listen-address`, and `--log.level` flags at minimum.
- **FR-007**: System MUST shut down gracefully on SIGTERM and SIGINT.
- **FR-008**: System MUST fail fast with a clear error on invalid configuration.
- **FR-009**: System MUST produce structured log output via promslog.
- **FR-010**: System MUST include at least one DNS check type that exercises the full prober pipeline (the specific check type will be determined during task planning).
- **FR-011**: Integration tests MUST run against a Docker-based DNS test environment using CoreDNS containers — not public DNS infrastructure.
- **FR-012**: The test DNS environment MUST simulate a zone hierarchy: one container acting as parent/root and up to eight containers acting as authoritative nameservers, each bound to a distinct loopback IP in the `127.240.0.0/24` range (e.g., `127.240.0.1` through `127.240.0.9`) on port 53 to avoid conflicts with services on standard loopback.
- **FR-013**: The test DNS environment MUST be defined as a Docker Compose configuration committed to the repository.

### Key Entities

- **Zone**: A DNS zone to monitor, identified by domain name. The primary unit of configuration and metric labeling.
- **Check**: A specific DNS health check performed against a zone (e.g., NS record retrieval, SOA query). Produces one or more metrics.
- **Prober**: A function that performs a category of DNS checks for a zone and registers metrics on a Prometheus registry.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Operator can go from cloning the repo to seeing metrics in a Prometheus-compatible format in under 5 minutes (build + configure + run + curl).
- **SC-002**: Integration test suite validates the full path from DNS query to metric output with at least one check type.
- **SC-003**: The exporter binary starts in under 2 seconds and responds to the first scrape request within 1 second of readiness.
- **SC-004**: All metrics pass `promtool check metrics` validation.

## Design Note: Prometheus vs Report-Style Impedance Mismatch

Tools like intodns.com and Zonemaster produce rich, narrative reports with pass/fail verdicts, explanatory text, and RFC references. Prometheus metrics are numeric time series with labels. There is a fundamental impedance mismatch between these models:

- Some checks produce clear numeric values (SOA serial, TTL values, response time) — these map naturally to gauges.
- Some checks are boolean (does glue match, are NS records consistent) — these map to gauges with value 0 or 1.
- Some checks produce informational text (nameserver hostnames, IP addresses, PTR records) — these are harder. The exporter MAY use info-style metrics (gauge with value 1 and descriptive labels) to expose this data, but Grafana is ultimately responsible for assembling the narrative view.
- Some intodns/Zonemaster checks are compound judgments ("your setup is good because X, Y, Z") — these do NOT belong in the exporter. The exporter exposes the raw signals; the dashboard applies the judgment.

This is a known design tension. The exporter will need to make pragmatic compromises, and the right answer will emerge as we implement checks. For this first version, focus on checks that map cleanly to metrics and defer the harder representational questions.

## Assumptions

- Integration tests run against local Docker-based CoreDNS fixtures, not public DNS. The developer needs Go and Docker installed.
- CoreDNS containers bind to distinct loopback IPs in `127.240.0.0/24` on port 53. Linux natively routes the entire `127.0.0.0/8` range to loopback; macOS/Windows may need additional configuration (out of scope, Linux is primary target).
- Testing strategy strongly favors integration tests against CoreDNS over unit tests with mocks. Wire-format edge cases and malformed DNS responses are out of scope for this feature.
- Integration tests call prober functions in-process (not the compiled binary) with a Prometheus registry, similar to how blackbox_exporter tests its probers.
- The specific DNS check types (SOA, NS consistency, MX, etc.) will be determined during task planning — this spec intentionally leaves that flexible to focus on the E2E flow.
- A single YAML configuration file is sufficient; advanced configuration patterns (reload, per-zone overrides) are out of scope for this version.
- CI/CD pipeline setup is out of scope; the integration test framework needs to work locally with `go test` + Docker Compose.
- Grafana dashboard creation is out of scope; this version focuses on producing metrics that a dashboard could consume.
