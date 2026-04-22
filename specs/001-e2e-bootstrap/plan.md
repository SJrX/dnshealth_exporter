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
│   └── config.go                # YAML config parsing, validation
├── prober/
│   ├── prober.go                # ProbeFn type, prober registry
│   ├── soa.go                   # SOA check prober
│   ├── soa_test.go              # SOA integration tests
│   ├── recursion.go             # Recursion-available check prober
│   ├── recursion_test.go        # Recursion integration tests
│   ├── glue.go                  # Glue consistency check prober
│   └── glue_test.go             # Glue integration tests
├── testutil/
│   ├── fixture.go               # DNSFixture: WriteZone, Reload, Probe
│   ├── records.go               # SOA(), NS(), A(), ZoneFile() helpers
│   └── assertions.go            # AssertGauge, WithLabels, WithValue
├── testdata/
│   ├── docker-compose.yml       # CoreDNS fixture orchestration
│   ├── coredns/
│   │   ├── root/
│   │   │   └── Corefile          # Root/parent Corefile (static)
│   │   ├── ns1/
│   │   │   └── Corefile          # NS1 Corefile (static, points to runtime zones)
│   │   ├── ns2/
│   │   │   └── Corefile          # NS2 Corefile (static)
│   │   ├── ns3/
│   │   │   └── Corefile          # NS3 Corefile (static)
│   │   └── runtime/             # Zone files written by tests at runtime
│   │       ├── ns1/zones/
│   │       ├── ns2/zones/
│   │       ├── ns3/zones/
│   │       └── root/zones/
│   └── configs/
│       └── integration.yml       # Exporter config for tests
├── go.mod
├── go.sum
├── dnshealth.yml                 # Example config
├── Makefile                      # Build, test, lint targets
├── README.md
└── LICENSE
```

**Structure Decision**: Single flat project following blackbox_exporter
conventions. `config/` for config parsing, `prober/` for check
implementations, `testdata/` for Docker Compose + CoreDNS fixtures.
No `internal/` or `pkg/` — keep it simple for v0.1.

## Testing Approach

See `research.md` for full details. Key decisions:

- **Three-phase tests** (Meszaros): Fixture Setup → Exercise SUT
  → Verification. Important values visible in both setup and
  assertions.
- **Thin fixture helpers** over `miekg/dns` types: `SOA(Serial(n))`,
  `NS("hostname")`, `A("name", "ip")`, `ZoneFile(zone, records...)`.
  Defaults-with-override — only test-relevant fields specified.
- **Per-test zone files**: `WriteZone("ns1", ...)` replaces the
  entire zone directory for that container. Each test fully declares
  what each nameserver serves. No cleanup needed — previous state
  is overwritten.
- **Domain assertion helpers**: `AssertGauge(t, registry, name, opts...)`
  with `WithLabels()` and `WithValue()` matchers.
- **Docker containers start once** in `TestMain`, not per-test.
  `Reload(t)` triggers CoreDNS zone reload between tests.
- **Sequential execution**: integration tests run with
  `-count=1 -parallel=1`.
- **Not a full DSL**: thin functional helpers are sufficient for
  ~3 check types. Refactorable into a fluent DSL later if fixture
  complexity grows.

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

## CoreDNS Test Fixture Design

| Container | Role | IP | Port | Purpose |
|-----------|------|-----|------|---------|
| `dns-root` | Parent/root | `127.240.0.1` | 53 | Delegates `example.test` to ns1, ns2, ns3 |
| `dns-ns1` | Authoritative NS1 | `127.240.0.2` | 53 | Serves `example.test` (correct config) |
| `dns-ns2` | Authoritative NS2 | `127.240.0.3` | 53 | Serves `example.test` (matches ns1) |
| `dns-ns3` | Authoritative NS3 | `127.240.0.4` | 53 | Serves `example.test` (deliberately mismatched glue/NS) |

Using `.test` TLD (RFC 2606 reserved) to avoid any real DNS
leakage.

**ns3 mismatch scenario**: The parent delegates to ns1, ns2, ns3
with specific glue IPs. ns3 returns different NS records or A
records for itself, creating a detectable glue inconsistency.
This exercises the failure path in integration tests.

CoreDNS Corefile sketch (ns1/ns2 — static, points to runtime dir):
```
. {
    file /runtime/zones/{zonefile}
    reload 1s
    log
}
```

The `reload 1s` directive lets CoreDNS pick up zone file changes
written by test fixtures without container restarts.

## Complexity Tracking

No Constitution Check violations. Table not needed.
