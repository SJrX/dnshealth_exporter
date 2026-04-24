# Research: Production Probe Loop

**Date**: 2026-04-23
**Feature**: Production Probe Loop (`003-production-probe-loop`)

## Architecture Decision: Scatter-Gather vs Registry Swap

- **Decision**: Scatter-gather with result structs, build metrics at end
- **Rationale**: Probers currently write directly to a Prometheus
  registry via `newGauge(registry, ...)`. This couples probing to
  metric registration. The scatter-gather pattern decouples them:
  probers return result data, the cycle runner builds a fresh
  registry from all results, and swaps it atomically. Benefits:
  (1) removed nameservers disappear naturally, (2) partial results
  from timeouts are easy to handle, (3) operational counters live
  on a separate permanent registry.
- **Alternatives**: Per-scrape probing (like node_exporter — too
  slow for DNS delegation walks), registry-per-cycle without
  refactoring probers (requires wrapping existing probers, messy).

## Prober Refactoring

Current prober signature:
```go
type ProbeFn func(ctx, zone, client, registry, logger) error
```

New signature returns results:
```go
type ProbeResult struct {
    Zone       string
    Check      string
    Nameserver string
    IP         string
    Success    bool
    Duration   time.Duration
    Metrics    map[string]float64  // metric name → value
    Labels     map[string]string   // extra labels (e.g., source)
}

type ProbeFn func(ctx, zone, client, logger) ([]ProbeResult, error)
```

The cycle runner collects all `ProbeResult`s, builds a registry,
and swaps it in. Operational counters (total requests, timeouts)
are updated on a permanent registry as a side effect.

## Two Registries

1. **Permanent registry**: `build_info`, operational counters
   (total DNS requests, total request time, total timeouts per
   server, probe cycle duration, zones probed, cache hits/misses).
   These are cumulative and never reset.

2. **Cycle registry**: All check-specific metrics (SOA fields,
   recursion_available, ns_record, ns_glue, query_success,
   query_duration). Rebuilt from scratch each cycle. Swapped
   atomically when the cycle completes.

The `/metrics` handler gathers from both registries.

## Delegation Cache Design

- **Key**: Zone FQDN (e.g., `example.com.`)
- **Value**: `DelegationResult` (NS records + glue + parent server)
- **TTL**: Configurable, default 30 minutes
- **Scope**: Only caches delegation walks (root → TLD → parent).
  Never caches authoritative nameserver queries.
- **Invalidation**: TTL-based. Also invalidated on config reload
  (in case NS infrastructure changed).
- **Concurrency**: `sync.RWMutex` — multiple zone probes can
  read concurrently, delegation walks acquire write lock.

## Probe Cycle Timing

```
ticker (60s default)
  │
  ├── cycle starts
  │   ├── fan out zone probes (goroutines)
  │   │   ├── zone A: delegation (cached?) → scatter queries → gather results
  │   │   ├── zone B: delegation (cached?) → scatter queries → gather results
  │   │   └── zone C: ...
  │   ├── collect all results
  │   ├── build new cycle registry
  │   └── atomic swap
  │
  ├── if cycle took > interval: skip next tick, log warning
  └── next tick
```

## Config Reload Flow

1. SIGHUP received or POST to `/-/reload`
2. Read and validate new config file
3. If valid: swap config atomically, invalidate delegation cache
4. If invalid: log error, keep old config
5. Next probe cycle uses new config (current cycle finishes first)

## Retry Strategy

- On transient DNS failure (timeout, network error): retry once
  with half the original timeout
- On non-transient failure (NXDOMAIN, REFUSED): no retry
- Retry is per-query, not per-zone or per-cycle
