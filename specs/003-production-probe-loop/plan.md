# Implementation Plan: Production Probe Loop

**Branch**: `003-production-probe-loop` | **Date**: 2026-04-23 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/003-production-probe-loop/spec.md`

## Summary

Refactor the exporter from probe-once-at-startup to a production
architecture: background probe loop with scatter-gather, delegation
caching, config reload, per-query retries, and parallel zone probing.
Probers are refactored to return structured result data instead of
writing directly to a Prometheus registry.

## Technical Context

**Language/Version**: Go 1.26.x
**Key Changes**: New packages `cycle/` and `cache/`, refactored `prober/`
**Concurrency**: `sync.RWMutex` for cache, `sync.WaitGroup` for scatter-gather, `atomic.Pointer` for registry swap
**Config**: New fields (probe_interval, delegation_cache_ttl, query_timeout, zone_deadline)

## Constitution Check

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Robust Integration Testing | PASS | FR-014 mandates test coverage for all new behavior. |
| II. Prometheus Naming Conventions | PASS | New operational metrics follow `dnshealth_` prefix convention. Counters use `_total` suffix. |
| III. Modern Go Ecosystem | PASS | Go 1.26.x, standard concurrency primitives. |
| IV. Structured Logging | PASS | Cycle events logged via promslog. |
| V. Zone-Focused Detection Scope | PASS | No change to detection model. |
| VI. Prometheus Ecosystem Conventions | PASS | Two-registry pattern follows Prometheus patterns. Config reload via SIGHUP follows blackbox_exporter. |
| VII. Well-Behaved Binary | PASS | SIGHUP reload, graceful shutdown drains in-flight cycle. |
| VIII. Readable, Honest Tests | PASS | Tests use testutil fixtures. |
| Dev Workflow | PASS | README updated per constitution requirement. |

## Project Structure

### New/Modified Files

```text
.
├── main.go                      # MODIFIED: probe loop, config reload, 503 handler
├── config/
│   └── config.go                # MODIFIED: new fields (intervals, timeouts)
├── cycle/
│   ├── runner.go                # NEW: probe cycle runner (scatter-gather)
│   └── runner_test.go           # NEW: cycle runner integration tests
├── cache/
│   ├── delegation.go            # NEW: delegation cache with TTL
│   └── delegation_test.go       # NEW: cache tests
├── prober/
│   ├── prober.go                # MODIFIED: ProbeFn returns []ProbeResult
│   ├── result.go                # NEW: ProbeResult type definition
│   ├── registry.go              # NEW: build registry from []ProbeResult
│   ├── soa.go                   # MODIFIED: return results, don't write registry
│   ├── recursion.go             # MODIFIED: same
│   ├── glue.go                  # MODIFIED: same
│   └── *_test.go                # MODIFIED: test against new return types
├── testutil/
│   ├── fixture.go               # MODIFIED: add cycle-level test helpers
│   └── assertions.go            # POSSIBLY MODIFIED: if new assertion patterns needed
└── README.md                    # MODIFIED: document new config options
```

## Key Architecture Decisions

### Scatter-Gather Pattern

Each probe cycle:
1. Fan out one goroutine per zone
2. Each zone goroutine: check delegation cache → scatter individual DNS queries → gather ProbeResults
3. Collect all ProbeResults from all zones
4. Build a new `prometheus.Registry` from results
5. Atomic swap via `atomic.Pointer[CycleState]`

### Two Registries

- **Permanent**: `build_info`, operational counters (`_total` suffixed)
- **Cycle**: All check metrics, rebuilt each cycle

`/metrics` handler gathers from both. Before first cycle: 503.

### Prober Refactor

Current: `ProbeFn(ctx, zone, client, registry, logger) error`
New: `ProbeFn(ctx, zone, client, logger) ([]ProbeResult, error)`

Probers return data. Registry is built centrally. This is the
biggest code change — every prober file and test needs updating.

### Delegation Cache

- `sync.RWMutex`-guarded map
- Key: zone FQDN
- Value: DelegationResult + timestamp
- TTL: configurable (default 30m)
- Invalidated on config reload
- Cache misses trigger delegation walk (acquires write lock)

### Config Reload

- SIGHUP handler + `/-/reload` POST endpoint
- Read → validate → swap config atomically
- Invalidate delegation cache on successful reload
- Current probe cycle finishes first, next cycle uses new config

## Complexity Tracking

No Constitution Check violations. Table not needed.
