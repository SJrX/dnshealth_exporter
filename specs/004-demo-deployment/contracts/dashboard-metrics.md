# Dashboard ↔ Exporter Metric Contract

The demo Grafana dashboard
(`demo/grafana/dashboards/dnshealth-overview.json`) queries the metric
series listed below. If any of these series is renamed, removed, or
gains/loses a label, the dashboard JSON MUST be updated in the same
change.

This file is the single place to look up "will this exporter change
break the demo dashboard?".

## Required metric series

| Metric | Type | Labels used by dashboard | Source (`grep`) | Used in panel |
|---|---|---|---|---|
| `dnshealth_query_success` | gauge | `zone`, `nameserver`, `ip`, `check` | `prober/registry.go` | Zone health overview |
| `dnshealth_query_duration_seconds` | gauge | `zone`, `nameserver`, `ip`, `check` | `prober/registry.go` | Query duration |
| `dnshealth_soa_serial` | gauge | `zone`, `nameserver`, `ip` | `prober/registry.go` (from `Metrics["soa_serial"]`) | SOA serials per zone |
| `dnshealth_ns_recursion_available` | gauge | `zone`, `nameserver`, `ip` | `prober/registry.go` (from `Metrics["ns_recursion_available"]`) | Recursion availability |
| `dnshealth_probe_cycle_duration_seconds` | gauge | none | `cycle/runner.go` | Probe cycle duration |
| `dnshealth_dns_queries_total` | counter | `server` | `cycle/runner.go` | Cycle queries |
| `dnshealth_delegation_cache_hits_total` | counter | none | `cycle/runner.go` | Cache hit ratio |
| `dnshealth_delegation_cache_misses_total` | counter | none | `cycle/runner.go` | Cache hit ratio |

## Optional / nice-to-have

These are not currently in the dashboard but are good candidates for
later panels. Listed here so dashboard authors know they exist:

- `dnshealth_dns_query_duration_seconds_total` (counter, label `server`)
- `dnshealth_dns_timeouts_total` (counter, label `server`)
- `dnshealth_probe_zones_total` (gauge)
- `dnshealth_build_info` (gauge, info-style)

## Verification

`demo/smoke.sh` queries `/metrics` and asserts that each of the
"Required metric series" above is present with at least one sample.
This protects the dashboard from silently breaking when the exporter
changes.
