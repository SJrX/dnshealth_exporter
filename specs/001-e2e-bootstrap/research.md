# Research: E2E Bootstrap

**Date**: 2026-04-21
**Feature**: E2E Bootstrap (`001-e2e-bootstrap`)

## Technology Decisions

### Go Version

- **Decision**: Go 1.26.x (installed: 1.26.2)
- **Rationale**: Current stable release, required for `log/slog` (used by promslog)
- **Alternatives**: Go 1.25.x (still supported, but no reason to target older)

### DNS Library

- **Decision**: `github.com/miekg/dns` v1
- **Rationale**: Used by CoreDNS itself, blackbox_exporter's DNS prober,
  and virtually every Go DNS tool. Battle-tested, well-documented.
- **Alternatives**: miekg/dns v2 (not yet released, moved to Codeberg,
  limited ecosystem adoption); net.Resolver (stdlib, too limited —
  can't inspect SOA fields, NS records, or wire-level details)

### Prometheus Libraries

- **Decision**: `prometheus/client_golang` v1.x, `prometheus/exporter-toolkit`
  v0.15.x, `prometheus/common/promslog`
- **Rationale**: Standard Prometheus ecosystem. blackbox_exporter uses
  the same stack. exporter-toolkit provides TLS, basic auth, and
  landing page out of the box.
- **Alternatives**: OpenTelemetry (too heavy, Prometheus-native is the
  right fit for an exporter)

### CLI Flags

- **Decision**: `alecthomas/kingpin/v2`
- **Rationale**: Used by all official Prometheus exporters. Integrates
  with exporter-toolkit's web flag group.
- **Alternatives**: cobra (common in Go ecosystem but not Prometheus
  convention), stdlib flag (too limited)

### Configuration Format

- **Decision**: YAML via `go.yaml.in/yaml/v3`
- **Rationale**: Consistent with blackbox_exporter and snmp_exporter
  config patterns. Human-readable, well-supported.
- **Alternatives**: TOML (not Prometheus convention), JSON (less
  human-friendly for config)

### Configuration Structure

- **Decision**: Simple zone list for v1. No modules, no per-zone
  check toggles, no per-zone nameserver overrides.
- **Rationale**: The exporter discovers nameservers by querying NS
  records — that discovery is itself part of the health check. A
  modules/overrides system adds complexity without value when we
  have only a few check types. Can revisit if config needs grow.
- **Example**:
  ```yaml
  zones:
    - example.com
    - example.org
  ```

### Integration Test Infrastructure

- **Decision**: Docker Compose + CoreDNS (`coredns/coredns` image)
- **Rationale**: CoreDNS is lightweight, highly configurable via
  Corefile, supports plugins for error injection (errc, template).
  Docker Compose allows defining a multi-container DNS hierarchy.
  Containers bind to `127.240.0.x:53` to avoid port conflicts.
- **Alternatives**: BIND (heavyweight, harder to configure for test
  scenarios); in-process miekg/dns test server (good for unit tests,
  but doesn't validate real DNS server behavior)

### CoreDNS Error Simulation

- **Decision**: Use CoreDNS `errc` plugin for SERVFAIL/REFUSED, and
  simply omit zones for NXDOMAIN. Timeout simulation via pointing
  exporter at an IP with nothing listening.
- **Rationale**: Keeps test fixtures simple and declarative. No custom
  code needed in CoreDNS config.
- **Alternatives**: Custom CoreDNS plugins (overkill for test fixtures)

## Testing Approach

### Philosophy

Follows xUnit Test Patterns (Meszaros). The governing principle:

> If something is important to understanding a test it must be in
> the test. If something is NOT important to understanding the test
> it is important that it is NOT in the test.

Every test has three visible phases: Fixture Setup, Exercise SUT,
Verification. No interleaving.

### Fixture Helpers (not a full DSL)

Thin functional builders over `miekg/dns` types. Each helper
returns a real `dns.RR` with sensible defaults — only test-relevant
fields are specified. `miekg/dns` handles serialization to zone
file format via `.String()`.

```go
func TestSOAProber_DetectsSerialDrift(t *testing.T) {
    // Fixture Setup — ns1 and ns2 have different serials
    env := NewDNSFixture(t).
        WriteZone("ns1", ZoneFile("example.test",
            SOA(Serial(2026042101)),
            NS("ns1.example.test"),
            NS("ns2.example.test"),
        )).
        WriteZone("ns2", ZoneFile("example.test",
            SOA(Serial(1)),
            NS("ns1.example.test"),
            NS("ns2.example.test"),
        )).
        Reload(t)

    // Exercise SUT
    metrics := env.Probe(prober.SOA, "example.test")

    // Verification
    AssertGauge(t, metrics, "dnshealth_soa_serial",
        WithLabels("zone", "example.test", "ip", "127.240.0.2"),
        WithValue(2026042101))
    AssertGauge(t, metrics, "dnshealth_soa_serial",
        WithLabels("zone", "example.test", "ip", "127.240.0.3"),
        WithValue(1))
}
```

The serial values that matter to the test are visible in both the
fixture setup and the verification. No hidden state in external
files.

### Record Helpers

Thin wrappers over `miekg/dns` types. Defaults-with-override
pattern — only specify what matters:

- `SOA(opts ...SOAOption)` — returns `*dns.SOA` with defaults
  for Mbox, Refresh, Retry, Expire, Minttl. Options like
  `Serial(n)`, `Refresh(n)` override specific fields.
- `NS(name string)` — returns `*dns.NS` with auto-filled header.
- `A(name, ip string)` — returns `*dns.A`.
- `MX(name string, pref uint16)` — returns `*dns.MX`.
- `ZoneFile(zone string, records ...dns.RR) string` — serializes
  records to zone file text via `rr.String()`.

These are ~50-100 lines of helper code total. They produce real
`dns.RR` objects, not fakes.

### Fixture Management

`NewDNSFixture(t)` manages the Docker Compose environment:

- Each CoreDNS container mounts a separate runtime directory
  (e.g., `testdata/runtime/ns1/zones/`).
- `WriteZone("ns1", content)` replaces the entire zone directory
  for that container. Each test fully defines what each nameserver
  serves — previous test state doesn't matter.
- `Reload(t)` signals CoreDNS to reload zone files (CoreDNS
  `reload` plugin). Blocks until reload is confirmed.
- Docker Compose containers start once for the test suite (via
  `TestMain`), not per-test.
- Integration tests run sequentially (`-count=1 -parallel=1`).

### Assertion Helpers

Domain-specific assertion functions for Prometheus metrics:

- `AssertGauge(t, registry, name, opts...)` — finds a metric by
  name and label matchers, asserts its value.
- `AssertGaugeExists(t, registry, name, opts...)` — asserts the
  metric exists (any value).
- `AssertGaugeMissing(t, registry, name, opts...)` — asserts no
  matching metric exists.
- `WithLabels(pairs ...string)` — label matcher option.
- `WithValue(v float64)` — value matcher option.

### Why Not a Full DSL

A full fluent context-navigation DSL (like the university enrollment
DSL pattern) could be powerful here but is premature for v1:

- We have ~3 check types and ~4 record helpers. The abstraction
  surface doesn't yet justify the investment.
- The thin functional helpers are easily refactorable into a DSL
  later if fixture complexity grows.
- The important thing is that tests are readable now, with the
  Meszaros principle satisfied. The fixture helpers achieve that.

## Architecture: Prober Pattern

Following blackbox_exporter, but adapted for zone monitoring:

- **blackbox_exporter**: `ProbeFn(ctx, target, module, registry, logger) bool`
  — called per-scrape, creates per-request registry.
- **Our adaptation**: Probers called by the exporter on its own schedule
  (not per-scrape), registering metrics on a shared registry that
  Prometheus scrapes. Each prober takes a zone config, DNS client,
  registry, and logger.

Prober signature (draft):

```go
type ProbeFn func(ctx context.Context, zone string,
    client *dns.Client, registry prometheus.Registerer,
    logger *slog.Logger) error
```

## Metric Design

### Design Pattern: The `source` Label

Many valuable DNS health checks compare what different sources say
about the same data. For example, glue consistency compares what
the parent says vs what the authoritative NS says. The Prometheus
metric model doesn't natively support "compare these two things,"
but we can enable it with a `source` label:

```prometheus
dnshealth_ns_record{zone="example.test",nameserver="ns1.example.test",
    ip="1.2.3.4",source="parent"} 1
dnshealth_ns_record{zone="example.test",nameserver="ns1.example.test",
    ip="1.2.3.4",source="self"} 1
```

Grafana can then join on `(zone, nameserver, ip)` and detect
mismatches where a record appears with one source but not the
other. This pattern extends to any multi-source comparison check.

**Open question**: This works for info-style metrics (value always
1, labels carry the data). For the convenience of alerting, we may
also want a pre-computed boolean like `dnshealth_glue_match{zone}`.
The right balance between raw info metrics and convenience booleans
will emerge as we implement. Start with the info metrics (they're
the ground truth) and add convenience booleans if needed.

### Check 1: SOA

Simplest meaningful check. Queries SOA record from each nameserver.
All fields are naturally numeric.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnshealth_soa_serial` | Gauge | zone, nameserver, ip | SOA serial number |
| `dnshealth_soa_refresh_seconds` | Gauge | zone, nameserver, ip | SOA REFRESH value |
| `dnshealth_soa_retry_seconds` | Gauge | zone, nameserver, ip | SOA RETRY value |
| `dnshealth_soa_expire_seconds` | Gauge | zone, nameserver, ip | SOA EXPIRE value |
| `dnshealth_soa_minimum_seconds` | Gauge | zone, nameserver, ip | SOA MINIMUM TTL |

Grafana can detect serial drift by comparing `dnshealth_soa_serial`
across nameservers for the same zone — no special logic needed.

### Check 2: Recursion Available

Queries each nameserver with RD flag set, checks if RA flag is
returned. Single bit in DNS header, exposed via `msg.RecursionAvailable`
in miekg/dns.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnshealth_ns_recursion_available` | Gauge | zone, nameserver, ip | 1 if NS allows recursion, 0 if not |

Authoritative-only nameservers SHOULD return 0. Grafana alerts if
any NS returns 1 (policy decision in the dashboard, not the exporter).

### Check 3: Glue Consistency (NS + A record comparison)

This is the hard one — and the reason this check is in the E2E
bootstrap. It proves we can handle the multi-source comparison
pattern that underpins many intodns/Zonemaster checks.

**What it does**: Query the parent zone for NS records and glue
(A records). Query each authoritative NS for its own NS records
and A records. Expose both views as info metrics so Grafana can
detect discrepancies.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnshealth_ns_record` | Gauge | zone, nameserver, ip, source | 1 for each (NS, IP) pair. `source` is `parent` or `self`. |
| `dnshealth_ns_glue` | Gauge | zone, nameserver, ip, source | 1 for each glue/A record. `source` is `parent` or `self`. |

**How Grafana uses this**: A PromQL query like
`count by (zone, nameserver, ip) (dnshealth_ns_record) != 2`
finds NS records that appear in only one source (mismatch).
Similarly for glue. Dashboard panels can show parent vs self
side-by-side.

**CoreDNS fixture plan**:
- ns1 + ns2: matching config (happy path)
- ns3 (on `127.240.0.4`): deliberately mismatched NS/glue to
  test the discrepancy detection path

### Common Metrics (all checks)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnshealth_check_success` | Gauge | zone, check | 1 if check completed without error, 0 if failed |
| `dnshealth_check_duration_seconds` | Gauge | zone, check | Time taken for check execution |

### Info Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnshealth_build_info` | Gauge | version, revision, goversion | Always 1 |
