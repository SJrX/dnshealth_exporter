# Metrics Contract: E2E Bootstrap

**Date**: 2026-04-21 (updated)

## Endpoint

- **Path**: `/metrics`
- **Method**: GET
- **Content-Type**: `text/plain; version=0.0.4; charset=utf-8`

## Global Metrics

```prometheus
# HELP dnshealth_build_info Build information for the exporter.
# TYPE dnshealth_build_info gauge
dnshealth_build_info{version="0.1.0",revision="abc1234",goversion="go1.26.2"} 1

# HELP dnshealth_check_success Whether the check succeeded (1=success, 0=failure).
# TYPE dnshealth_check_success gauge
dnshealth_check_success{zone="example.test",check="soa"} 1

# HELP dnshealth_check_duration_seconds Duration of the check in seconds.
# TYPE dnshealth_check_duration_seconds gauge
dnshealth_check_duration_seconds{zone="example.test",check="soa"} 0.042
```

## SOA Check Metrics

Per-nameserver query status and timing:

```prometheus
# HELP dnshealth_soa_query_success Whether the SOA query succeeded (1=success, 0=failure).
# TYPE dnshealth_soa_query_success gauge
dnshealth_soa_query_success{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2"} 1

# HELP dnshealth_soa_query_duration_seconds Duration of the SOA query in seconds.
# TYPE dnshealth_soa_query_duration_seconds gauge
dnshealth_soa_query_duration_seconds{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2"} 0.023
```

Per-nameserver SOA field gauges (only present when query succeeds):

```prometheus
dnshealth_soa_serial{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2"} 2026042101
dnshealth_soa_refresh_seconds{zone="...",nameserver="...",ip="..."} 3600
dnshealth_soa_retry_seconds{...} 300
dnshealth_soa_expire_seconds{...} 2419200
dnshealth_soa_minimum_seconds{...} 300
```

## Recursion Check Metrics

```prometheus
# HELP dnshealth_recursion_query_success Whether the recursion query succeeded.
# TYPE dnshealth_recursion_query_success gauge
dnshealth_recursion_query_success{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2"} 1

# HELP dnshealth_ns_recursion_available Whether the nameserver allows recursion (1=allows, 0=refuses).
# TYPE dnshealth_ns_recursion_available gauge
dnshealth_ns_recursion_available{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2"} 0
```

## Glue Consistency Metrics

Per-nameserver query status:

```prometheus
# HELP dnshealth_glue_query_success Whether the glue self-query succeeded.
# TYPE dnshealth_glue_query_success gauge
dnshealth_glue_query_success{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2"} 1
```

Info-style metrics with `source` label for multi-source comparison:

```prometheus
# HELP dnshealth_ns_record NS record presence by source (value always 1).
# TYPE dnshealth_ns_record gauge
dnshealth_ns_record{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2",source="parent"} 1
dnshealth_ns_record{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2",source="self"} 1

# HELP dnshealth_ns_glue Glue/A record presence by source (value always 1).
# TYPE dnshealth_ns_glue gauge
dnshealth_ns_glue{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2",source="parent"} 1
dnshealth_ns_glue{zone="example.test",nameserver="ns1.example.test.",ip="127.240.0.2",source="self"} 1
```

## Labels

| Label | Description | Cardinality |
|-------|-------------|-------------|
| `zone` | DNS zone name | Low (configured zones) |
| `check` | Check type (`soa`, `recursion`, `glue`) | Low (fixed set) |
| `nameserver` | NS hostname | Low (per-zone NS count) |
| `ip` | Nameserver IP address | Low (per-NS, usually 1) |
| `source` | Data source (`parent`, `self`) | 2 (fixed) |

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
