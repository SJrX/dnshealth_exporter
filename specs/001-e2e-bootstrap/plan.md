# Implementation Plan: E2E Bootstrap

**Branch**: `001-e2e-bootstrap` | **Date**: 2026-04-21 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/001-e2e-bootstrap/spec.md`

## Summary

Bootstrap the dnshealth_exporter project with a working end-to-end
flow: a Go binary that reads a YAML config of DNS zones, runs three
check types (SOA, recursion-available, glue consistency), and exposes
Prometheus metrics on `/metrics`. The glue consistency check is the
key proof point — it validates the multi-source comparison pattern
(`source` label) that underpins most valuable intodns/Zonemaster
checks. Includes Docker Compose + CoreDNS integration test fixtures
on `127.240.0.0/24`.

## Technical Context

**Language/Version**: Go 1.26.x (installed: 1.26.2)
**Primary Dependencies**: `prometheus/client_golang`, `prometheus/exporter-toolkit`, `prometheus/common/promslog`, `alecthomas/kingpin/v2`, `miekg/dns`
**Storage**: N/A (stateless exporter)
**Testing**: `go test` with `-tags=integration` build tag; Docker Compose + CoreDNS for integration fixtures
**Target Platform**: Linux (primary)
**Project Type**: Prometheus exporter (long-running daemon)
**Performance Goals**: Respond to scrape within 1s of readiness; start in under 2s
**Constraints**: Must work with standard `prometheus.yml` scrape config; no custom service discovery
**Scale/Scope**: Single binary, 1-100 configured zones, 1-8 nameservers per zone

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Robust Integration Testing | PASS | Docker/CoreDNS fixtures (FR-011..013). Integration tests call probers in-process. Glue mismatch fixture validates failure path. |
| II. Prometheus Naming Conventions | PASS | `dnshealth_` prefix, snake_case, unit suffixes, identifying labels (zone, nameserver, ip, source). |
| III. Modern Go Ecosystem | PASS | Go 1.26.x, miekg/dns v1, client_golang, Go modules. |
| IV. Structured Logging | PASS | promslog (slog-based). Log levels defined. |
| V. Zone-Focused Detection Scope | PASS | Monitors zones, raw metrics via `source` label pattern, no alerting logic. |
| VI. Prometheus Ecosystem Conventions | PASS | Follows blackbox_exporter structure. Uses kingpin, exporter-toolkit, promslog. |
| VII. Well-Behaved Binary | PASS | Signal handling, fail-fast, standard flags, health endpoint. |

All gates pass. No violations to justify.

## Project Structure

### Documentation (this feature)

```text
specs/001-e2e-bootstrap/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── metrics.md       # Metrics endpoint contract
├── checklists/
│   └── requirements.md  # Spec quality checklist
└── tasks.md             # Phase 2 output (/speckit-tasks)
```

### Source Code (repository root)

```text
.
├── main.go                      # Entry point: flags, config load, server
├── config/
│   ├── config.go                # YAML config parsing, validation, address overrides
│   └── config_test.go           # Config unit tests
├── prober/
│   ├── prober.go                # ProbeFn type, prober registry, delegation walker
│   ├── soa.go                   # SOA check prober
│   ├── soa_test.go              # SOA integration tests (10 tests)
│   ├── recursion.go             # Recursion-available check prober
│   ├── recursion_test.go        # Recursion integration tests (3 tests)
│   ├── glue.go                  # Glue consistency check prober
│   ├── glue_test.go             # Glue integration tests (5 tests)
│   └── integration_test.go      # TestMain: sets RootServers + ResolveAddress
├── testutil/
│   ├── fixture.go               # DNSFixture: Server, ReferralServer, Probe
│   ├── records.go               # SOA(), NS(), A() helpers (miekg/dns wrappers)
│   ├── records_test.go          # Record helper unit tests
│   └── assertions.go            # AssertGauge, AssertGaugeInRange, WithLabels
├── go.mod
├── go.sum
├── dnshealth.yml                # Example config
├── Makefile                     # Build, test, test-integration, vet, fmt
├── README.md
└── LICENSE
```

**Structure Decision**: Single flat project following blackbox_exporter
conventions. `config/` for config parsing, `prober/` for check
implementations and delegation walking, `testutil/` for in-process
DNS test servers and assertion helpers.
No `internal/` or `pkg/` — keep it simple for v0.1.

## Testing Approach

See `research.md` for full details. Key decisions:

- **In-process DNS servers** via `miekg/dns` — no Docker, no external
  dependencies. Each test starts its own servers on `127.240.0.x:10053`
  and tears them down after. Sub-second test runs.
- **Three-phase tests** (Meszaros): Fixture Setup → Exercise SUT
  → Verification. Important values visible in both setup and
  assertions.
- **Two server types**: `Server()` for authoritative nameservers,
  `ReferralServer()` for parent/TLD servers that return delegations
  in the Authority section. `ServerWithOptions()` for custom behavior
  (recursion available, NXDOMAIN, drop/timeout, garbage responses).
- **Thin record helpers** over `miekg/dns` types: `SOA(zone, Serial(n))`,
  `NS(zone, hostname)`, `A(name, ip)`. Defaults-with-override.
- **Domain assertion helpers**: `AssertGauge(t, registry, name, opts...)`,
  `AssertGaugeExists`, `AssertGaugeMissing`, `AssertGaugeInRange`,
  `WithLabels()`, `WithValue()`.
- **Same code path as production**: Tests override only `RootServers`
  and `ResolveAddress`. The delegation walker, nameserver discovery,
  and probers run identical code in tests and production.

## Check Types

Three checks for the E2E bootstrap, chosen to prove different
metric patterns:

### 1. SOA Check (numeric gauges)

Simplest meaningful check. Queries SOA from each nameserver.
All fields naturally numeric. Serial drift across nameservers is
detectable by comparing the same metric across label values.

See `research.md` for metric table.

### 2. Recursion Available (boolean gauge)

Queries each nameserver with RD flag set, checks RA flag in
response. Single bit, maps to a 0/1 gauge. Proves the simple
boolean check pattern.

### 3. Glue Consistency (multi-source info metrics)

**This is the key proof point for the E2E bootstrap.** Queries
the parent for NS records + glue, queries each authoritative NS
for its own NS + A records, and exposes both views as info metrics
with a `source` label (`parent` vs `self`). Grafana joins on
`(zone, nameserver, ip)` to detect mismatches.

This validates the `source` label pattern that most valuable
intodns/Zonemaster checks will eventually need. If this works
well, the pattern extends to other comparison checks. If it
doesn't, we learn what needs to change before building more.

See `research.md` for the full metric design and the open
questions about convenience booleans vs raw info metrics.

## Test Fixture Design

In-process DNS servers using `miekg/dns`. Each test creates its own
servers — no shared state between tests, no Docker.

| Server Type | API | Behavior |
|-------------|-----|----------|
| `Server()` | Authoritative | Returns records in Answer section, AA flag set |
| `ReferralServer()` | Parent/TLD | Returns NS in Authority + glue in Additional, no AA flag |
| `ServerWithOptions()` | Configurable | Supports RecursionAvailable, Rcode override, Drop (timeout), Garbage |

All servers bind to `127.240.0.x:10053` using `.test` TLD (RFC 2606).

**Test coverage** (18 integration tests):

- SOA: serial consistency, all fields, serial drift, no SOA in
  response, single NS, no-glue resolution, timeout, NXDOMAIN, garbage
- Recursion: refusal, allows recursion, mixed across NSes
- Glue: consistent, mismatched NS, different IPs by source,
  no glue from parent (3-level delegation), partial glue

## Complexity Tracking

No Constitution Check violations. Table not needed.
