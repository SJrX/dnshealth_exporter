# dnshealth_exporter

[![CI](https://github.com/sjr/dnshealth_exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/sjr/dnshealth_exporter/actions/workflows/ci.yml)

A Prometheus exporter for DNS zone health monitoring, inspired by [intodns.com](https://intodns.com).

Monitors DNS zones and exposes granular metrics for building Grafana dashboards that detect delegation, nameserver, SOA, MX, and other DNS health problems.

## Goals

- **DNS zone health as metrics** — surface the kinds of checks that intodns.com performs (parent delegation, NS consistency, SOA correctness, MX validity, etc.) as Prometheus metrics, enabling Grafana dashboards and alerting rather than one-off web reports.
- **Follow Prometheus conventions** — model the exporter after official projects like [blackbox_exporter](https://github.com/prometheus/blackbox_exporter), [node_exporter](https://github.com/prometheus/node_exporter), and [snmp_exporter](https://github.com/prometheus/snmp_exporter), using [exporter-toolkit](https://github.com/prometheus/exporter-toolkit) and the standard Prometheus library ecosystem.
- **Detection, not policy** — the exporter exposes raw signals; thresholds, alerting rules, and SLA definitions belong in Grafana/Alertmanager.
- **Explore Spec Kit** — this project is also an opportunity to practice and explore [Spec Kit](https://github.com/github/spec-kit), a spec-driven development toolkit for AI-assisted workflows. The `.specify/` directory contains the project's specifications, plans, and constitution generated through the Spec Kit process.

## Quick Start

### Prerequisites

- Go 1.26+

### Build

```bash
go build -o dnshealth_exporter .
```

### Configure

Create `dnshealth.yml`:

```yaml
zones:
  - example.com
  - example.org
```

### Run

```bash
./dnshealth_exporter --config.file=dnshealth.yml
```

Visit http://localhost:9199/metrics to see output.

### Test

Unit tests:

```bash
go test ./...
```

Integration tests (no Docker needed — uses in-process DNS servers):

```bash
go test -tags=integration ./...
```

## Status

Early development — SOA, recursion-available, and glue consistency checks implemented.

## License

[Apache License 2.0](LICENSE)