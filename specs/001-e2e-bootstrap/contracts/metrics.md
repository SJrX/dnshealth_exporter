# Metrics Contract: E2E Bootstrap

**Date**: 2026-04-21

## Endpoint

- **Path**: `/metrics`
- **Method**: GET
- **Content-Type**: `text/plain; version=0.0.4; charset=utf-8`
  (Prometheus exposition format)

## Global Metrics

Always present regardless of zone configuration.

```prometheus
# HELP dnshealth_build_info Build information for the exporter.
# TYPE dnshealth_build_info gauge
dnshealth_build_info{version="0.1.0",revision="abc1234",goversion="go1.26.2"} 1
```

## Per-Check Metrics

Present for each (zone, check) combination.

```prometheus
# HELP dnshealth_check_success Whether the check succeeded (1=success, 0=failure).
# TYPE dnshealth_check_success gauge
dnshealth_check_success{zone="example.test",check="soa"} 1
dnshealth_check_success{zone="example.test",check="recursion"} 1
dnshealth_check_success{zone="example.test",check="glue"} 1

# HELP dnshealth_check_duration_seconds Duration of the check in seconds.
# TYPE dnshealth_check_duration_seconds gauge
dnshealth_check_duration_seconds{zone="example.test",check="soa"} 0.042
dnshealth_check_duration_seconds{zone="example.test",check="recursion"} 0.018
dnshealth_check_duration_seconds{zone="example.test",check="glue"} 0.091
```

## SOA Check Metrics

```prometheus
# HELP dnshealth_soa_serial SOA serial number.
# TYPE dnshealth_soa_serial gauge
dnshealth_soa_serial{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2"} 2026042101

# HELP dnshealth_soa_refresh_seconds SOA REFRESH interval in seconds.
# TYPE dnshealth_soa_refresh_seconds gauge
dnshealth_soa_refresh_seconds{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2"} 3600

# HELP dnshealth_soa_retry_seconds SOA RETRY interval in seconds.
# TYPE dnshealth_soa_retry_seconds gauge
dnshealth_soa_retry_seconds{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2"} 300

# HELP dnshealth_soa_expire_seconds SOA EXPIRE interval in seconds.
# TYPE dnshealth_soa_expire_seconds gauge
dnshealth_soa_expire_seconds{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2"} 2419200

# HELP dnshealth_soa_minimum_seconds SOA MINIMUM TTL (negative caching) in seconds.
# TYPE dnshealth_soa_minimum_seconds gauge
dnshealth_soa_minimum_seconds{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2"} 300
```

## Recursion Available Metrics

```prometheus
# HELP dnshealth_ns_recursion_available Whether the nameserver allows recursive queries (1=allows, 0=refuses).
# TYPE dnshealth_ns_recursion_available gauge
dnshealth_ns_recursion_available{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2"} 0
dnshealth_ns_recursion_available{zone="example.test",nameserver="ns2.example.test",ip="127.240.0.3"} 0
```

## Glue Consistency Metrics

Info-style metrics with `source` label for multi-source comparison.

```prometheus
# HELP dnshealth_ns_record NS record presence by source (value always 1).
# TYPE dnshealth_ns_record gauge
dnshealth_ns_record{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2",source="parent"} 1
dnshealth_ns_record{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2",source="self"} 1
dnshealth_ns_record{zone="example.test",nameserver="ns2.example.test",ip="127.240.0.3",source="parent"} 1
dnshealth_ns_record{zone="example.test",nameserver="ns2.example.test",ip="127.240.0.3",source="self"} 1

# HELP dnshealth_ns_glue Glue/A record presence by source (value always 1).
# TYPE dnshealth_ns_glue gauge
dnshealth_ns_glue{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2",source="parent"} 1
dnshealth_ns_glue{zone="example.test",nameserver="ns1.example.test",ip="127.240.0.2",source="self"} 1
```

## Labels

| Label | Description | Cardinality |
|-------|-------------|-------------|
| `zone` | DNS zone name | Low (configured zones) |
| `check` | Check type (`soa`, `recursion`, `glue`) | Low (fixed set) |
| `nameserver` | NS hostname | Low (per-zone NS count) |
| `ip` | Nameserver IP address | Low (per-NS, usually 1) |
| `source` | Data source (`parent`, `self`) | 2 (fixed) |
| `version` | Exporter version | 1 |
| `revision` | Git revision | 1 |
| `goversion` | Go version | 1 |

## Other Endpoints

| Path | Method | Description |
|------|--------|-------------|
| `/` | GET | Landing page (exporter-toolkit) |
| `/-/healthy` | GET | Health check, returns 200 |

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config.file` | `dnshealth.yml` | Path to configuration file |
| `--web.listen-address` | `:9199` | Address to listen on |
| `--web.config.file` | `""` | TLS/auth config (exporter-toolkit) |
| `--log.level` | `info` | Log level (debug, info, warn, error) |
| `--log.format` | `logfmt` | Log format (logfmt, json) |
| `--version` | — | Print version and exit |
