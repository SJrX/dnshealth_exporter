# Data Model: E2E Bootstrap

**Date**: 2026-04-21 (updated)
**Feature**: E2E Bootstrap (`001-e2e-bootstrap`)

## Entities

### Configuration (YAML file)

Minimal for v1. Zone list with optional address overrides.

- `zones` ([]string, required): DNS zone names to monitor
- `address_overrides` (map[string]string, optional): Maps an IP
  to a custom host:port pair. Used in tests (port 10053) and
  for production scenarios with non-standard ports.

### Zone (Runtime)

The primary unit of monitoring, derived from config.

- `name` (string): DNS zone name from config
- `nameservers` ([]Nameserver): Discovered via delegation walk

### Nameserver (Runtime, discovered)

Discovered by walking the delegation chain from root servers.

- `hostname` (string): NS record value (e.g., `ns1.example.com.`)
- `ip` (string): Resolved A record for the hostname

### DelegationResult (Runtime)

The parent's delegation response, obtained by walking from root.

- `ParentServer` (string): Address of the parent that provided delegation
- `NSRecords` ([]Nameserver): NS records from delegation
- `Glue` ([]Nameserver): A records from delegation additional section

## Configuration File Example

```yaml
zones:
  - example.com
  - example.org

# Optional: override addresses for testing or non-standard ports
address_overrides:
  "127.240.0.2": "127.240.0.2:10053"
```

## Relationships

```text
Config File (YAML)
├── zones[] (list of strings)
└── address_overrides (optional map)

Runtime
└── Zone (per configured zone name)
    ├── WalkDelegation (root → TLD → parent)
    │   └── DelegationResult (parent's NS + glue)
    ├── discoverNameservers (from delegation + hostname resolution)
    │   └── Nameserver[] (hostname + IP)
    └── Check Execution (per prober)
        ├── Per-nameserver queries
        ├── query_success + query_duration metrics
        └── Check-specific metrics (on registry)
```
