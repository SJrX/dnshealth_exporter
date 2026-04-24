# Feature Specification: Production Probe Loop

**Feature Branch**: `003-production-probe-loop`
**Created**: 2026-04-23
**Status**: Draft
**Input**: User description: "Production grade periodic probing with delegation caching, config reload, timeout handling, and parallel DNS requests"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Metrics Reflect Current DNS State (Priority: P1)

An operator deploys the exporter and points Prometheus at it with a 60-second scrape interval. Each time Prometheus scrapes `/metrics`, the response reflects the current state of DNS for all configured zones — not stale data from startup.

**Why this priority**: Without fresh metrics, the exporter is useless for monitoring. Stale data from startup defeats the entire purpose.

**Independent Test**: Start the exporter, scrape metrics, change DNS state (e.g., update a SOA serial), wait for the next probe cycle, scrape again, and verify the metrics reflect the new state.

**Acceptance Scenarios**:

1. **Given** the exporter is running, **When** Prometheus scrapes `/metrics` at any time, **Then** the metrics reflect DNS state from within the last probe cycle (not from startup).
2. **Given** a zone's SOA serial changes, **When** the next probe cycle completes, **Then** the updated serial appears in subsequent scrapes.
3. **Given** a nameserver goes offline, **When** the next probe cycle runs, **Then** `query_success=0` is reported for that nameserver.

---

### User Story 2 - Operator Reloads Configuration (Priority: P2)

An operator modifies the configuration file (adds or removes zones) and sends SIGHUP or POSTs to `/-/reload`. The exporter picks up the new configuration without restarting, and subsequent probe cycles use the updated zone list.

**Why this priority**: Restarting the exporter to change zones causes a metrics gap. Hot reload avoids downtime.

**Independent Test**: Start the exporter with zone A, send SIGHUP, verify zone B appears in metrics on the next probe cycle. Verify zone A is no longer probed if removed.

**Acceptance Scenarios**:

1. **Given** the operator adds a new zone to the config file and sends SIGHUP, **When** the next probe cycle runs, **Then** metrics for the new zone appear.
2. **Given** the operator removes a zone from the config file and sends SIGHUP, **When** the next probe cycle runs, **Then** metrics for the removed zone are no longer updated (and eventually go stale in Prometheus).
3. **Given** the operator sends a reload with an invalid config file, **When** the exporter processes the reload, **Then** the exporter logs an error and continues using the previous valid configuration.

---

### User Story 3 - Exporter Handles Failures Gracefully Under Load (Priority: P3)

An operator configures the exporter with many zones (10+). The exporter probes all zones within a reasonable time, handles nameserver timeouts without blocking other zones, and does not overwhelm DNS root servers with redundant delegation walks.

**Why this priority**: Without parallelism and caching, the exporter is too slow for real deployments and risks being rate-limited by upstream DNS infrastructure.

**Independent Test**: Configure 10+ zones, verify all zones are probed within one probe cycle, verify root servers are queried at most once per delegation walk (cached), verify a single slow nameserver does not block probing of other zones.

**Acceptance Scenarios**:

1. **Given** 10 zones are configured, **When** a probe cycle runs, **Then** all zones complete probing within a bounded time (not 10x the single-zone time).
2. **Given** a nameserver times out for one zone, **When** the probe cycle runs, **Then** other zones are not delayed by that timeout.
3. **Given** multiple zones share the same TLD, **When** delegation walks run, **Then** the root and TLD servers are not queried redundantly (delegation results are cached).

### Edge Cases

- What happens if a probe cycle takes longer than the probe interval? (The next cycle should be skipped or delayed, not stacked.)
- What happens if all nameservers for a zone are unreachable? (The zone should report `check_success=0` and move on.)
- What happens if the config file is deleted while the exporter is running? (Reload should fail gracefully; exporter continues with previous config.)
- What happens during a reload while a probe cycle is in progress? (The current cycle should complete; the next cycle uses the new config.)

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The exporter MUST run probe cycles periodically on a configurable interval (default: 60 seconds).
- **FR-002**: Metrics served on `/metrics` MUST reflect the results of the most recently completed probe cycle, not startup data.
- **FR-003**: The exporter MUST cache delegation walk results (root → TLD → parent) with a configurable TTL (default: 30 minutes) to avoid hammering root and TLD servers. This cache applies only to non-target infrastructure (root servers, TLD servers, parent servers) — queries to the zone's own authoritative nameservers are NEVER cached and run every probe cycle.
- **FR-004**: The exporter MUST probe multiple zones concurrently within a single probe cycle.
- **FR-005**: Each individual DNS query MUST have a configurable timeout (default: 5 seconds). Each zone MUST have a configurable overall deadline (default: 30 seconds) after which outstanding queries for that zone are cancelled.
- **FR-006**: The exporter MUST support configuration reload via SIGHUP signal and HTTP POST to `/-/reload`.
- **FR-007**: A failed reload (invalid config) MUST NOT affect the running configuration. The exporter MUST log the error and continue operating.
- **FR-008**: Metrics for zones removed during reload MUST stop being updated (Prometheus handles staleness via its 5-minute lookback).
- **FR-009**: When a nameserver disappears from a zone's delegation, its metrics MUST NOT appear in subsequent scrapes. This is satisfied naturally by the scatter-gather approach — each cycle builds a fresh metric set from current results, so absent nameservers are simply not present.
- **FR-010**: If a probe cycle exceeds the probe interval, the next cycle MUST be delayed (not stacked).
- **FR-011**: Individual DNS queries MUST be retried once on transient failure (timeout, network error) before being marked as failed. The retry MUST use a short timeout (not the full query timeout again).
- **FR-012**: The exporter MUST expose operational metrics about its own health: probe cycle duration, number of zones probed, cache hit/miss counts for delegation walks, total DNS requests per server, total request time per server, and total timeouts per server.
- **FR-013**: Probes run on a background timer, decoupled from Prometheus scrape requests. `/metrics` serves the most recently completed probe results.
- **FR-014**: All new behavior (probe loop timing, delegation caching, config reload, parallel probing, retry logic, stale metric cleanup) MUST have integration test coverage before the feature is considered complete.
- **FR-015**: Before the first probe cycle completes, `/metrics` MUST return HTTP 503 (Service Unavailable). Once the first cycle completes, `/metrics` serves results normally.
- **FR-016**: Operational counters (total DNS requests, total request time, total timeouts per server) MUST be cumulative and persist across probe cycles. They are NOT part of the per-cycle metric swap.

### Key Entities

- **Probe Cycle**: A single run of all checks across all configured zones. Runs periodically.
- **Delegation Cache**: Stores the results of root → TLD → parent delegation walks with a TTL. Shared across zones and probe cycles.
- **Zone Probe**: A single zone being probed within a cycle. Runs concurrently with other zone probes.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Metrics on `/metrics` are never older than 2x the probe interval (e.g., within 120 seconds for a 60-second interval).
- **SC-002**: Probing 10 zones completes within 30 seconds (not 10x the single-zone probe time).
- **SC-003**: After a config reload adding a new zone, metrics for that zone appear within one probe interval.
- **SC-004**: Root DNS servers are queried at most once per delegation cache TTL per TLD, regardless of how many zones share that TLD.

## Assumptions

- The probe interval is configurable via CLI flag or config file, with a sensible default (60 seconds).
- Delegation cache TTL is configurable, with a sensible default (30 minutes). The cache covers all non-target DNS infrastructure (root, TLD, parent servers). Authoritative nameserver queries are never cached.
- Per-query timeout configurable, default 5 seconds. Per-zone overall deadline configurable, default 30 seconds.
- The existing prober functions will need refactoring to return structured result data instead of directly registering metrics on a registry. This is a necessary architectural change — the scatter-gather pattern collects results as data, then builds metrics from the complete result set at the end of each cycle.
- Stale metric cleanup for removed zones is handled by Prometheus's built-in staleness mechanism (5-minute lookback). Stale metrics for removed nameservers are handled by the scatter-gather pattern (fresh metric set each cycle).
- This feature does not change the metrics contract — the same metrics are produced, just refreshed periodically instead of once.
- A probe cycle fans out all DNS queries as independent concurrent operations (scatter), collects all results (gather), and builds a complete new metric set from the results. The new metric set replaces the old one atomically. This naturally handles nameserver removal — if a NS is gone from delegation, it won't be in the new results.
- If the cycle hits its overall timeout before all queries complete, it cancels outstanding queries, discards incomplete results for those queries, and builds metrics from whatever did complete. A subsequent cycle retries everything.
- The probers may need refactoring to return structured result data instead of directly registering metrics on a registry. The metrics are built from results at the end, not during probing.
