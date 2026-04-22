# Data Model: E2E Bootstrap

**Date**: 2026-04-21
**Feature**: E2E Bootstrap (`001-e2e-bootstrap`)

## Entities

### Configuration (YAML file)

Intentionally minimal for v1. Just a list of zone names.

- `zones` ([]string, required): DNS zone names to monitor
  (e.g., `example.com`, `example.org`)

Nameservers are discovered by querying NS records — that discovery
is itself part of the health check. Per-zone overrides and check
toggles are out of scope for v1.

### Zone (Runtime)

The primary unit of monitoring, derived from config.

- `name` (string): DNS zone name from config
- `nameservers` ([]Nameserver): Discovered via NS query at runtime

### Nameserver (Runtime, discovered)

A nameserver serving a zone. Discovered by querying NS records,
not configured.

- `hostname` (string): NS record value (e.g., `ns1.example.com`)
- `ip` (string): Resolved A record for the hostname

### Check Result (Runtime)

Internal structure bridging probers and metrics. Not persisted.

- `zone` (string): Zone that was checked
- `check` (string): Check type name (`soa`, `recursion`, `glue`)
- `success` (bool): Whether the check completed without error
- `duration` (time.Duration): How long the check took

## Configuration File Example

```yaml
zones:
  - example.com
  - example.org
```

## Relationships

```text
Config File (YAML)
└── zones[] (list of strings)

Runtime
└���─ Zone (per configured zone name)
    ├── NS discovery → Nameserver[] (hostname + IP)
    └── Check Execution (per check type)
        ├── Queries parent and/or each nameserver
        ├── Check Result
        └── Prometheus Metrics (on shared registry)
```
