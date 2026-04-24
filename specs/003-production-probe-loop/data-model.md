# Data Model: Production Probe Loop

**Date**: 2026-04-23
**Feature**: Production Probe Loop (`003-production-probe-loop`)

## Entities

### ProbeResult (Runtime)

The output of a single prober for a single nameserver. Data only —
no Prometheus types. Metrics are built from these at the end of
each cycle.

- `Zone` (string): Zone that was probed
- `Check` (string): Check type (soa, recursion, glue)
- `Nameserver` (string): NS hostname
- `IP` (string): NS IP address
- `Success` (bool): Whether the query succeeded
- `Duration` (time.Duration): Query response time
- `Metrics` (map[string]float64): Check-specific metric values
  (e.g., `{"soa_serial": 2026042101, "soa_refresh_seconds": 3600}`)
- `Labels` (map[string]string): Extra labels beyond the standard
  zone/nameserver/ip (e.g., `{"source": "parent"}`)

### DelegationCacheEntry (Runtime)

Cached delegation walk result.

- `Result` (*DelegationResult): NS records + glue from parent
- `CachedAt` (time.Time): When the entry was cached
- `TTL` (time.Duration): How long the entry is valid

### CycleState (Runtime)

The atomic unit that gets swapped on each probe cycle.

- `Registry` (*prometheus.Registry): Built from all ProbeResults
- `CompletedAt` (time.Time): When the cycle finished
- `ZoneCount` (int): Number of zones probed
- `Duration` (time.Duration): Total cycle time

### Config Changes

New fields in the YAML config:

- `probe_interval` (duration, optional): Default 60s
- `delegation_cache_ttl` (duration, optional): Default 30m
- `query_timeout` (duration, optional): Default 5s
- `zone_deadline` (duration, optional): Default 30s

## Relationships

```
Background Timer (probe_interval)
└── Probe Cycle
    ├── For each zone (concurrent goroutines):
    │   ├── Delegation Walk (check cache first)
    │   │   └── DelegationCacheEntry (hit or miss)
    │   ├── Scatter DNS queries (per NS × per check)
    │   │   └── Each query: timeout, retry once, → ProbeResult
    │   └── Gather ProbeResults
    ├── Build CycleState (new registry from all results)
    ├── Atomic swap (old CycleState → new CycleState)
    └── Update operational counters on permanent registry

/metrics handler
├── Gather from permanent registry (build_info, counters)
├── Gather from current CycleState registry (check metrics)
└── If no CycleState yet → 503
```
