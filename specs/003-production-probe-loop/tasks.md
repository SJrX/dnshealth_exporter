# Tasks: Production Probe Loop

**Input**: Design documents from `specs/003-production-probe-loop/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md

**Tests**: FR-014 mandates integration test coverage for all new behavior. Test tasks are included.

**Organization**: Tasks grouped by user story with foundational refactoring first.

## Format: `[ID] [P?] [Story] Description`

---

## Phase 1: Setup

**Purpose**: Create new packages and data types

- [x] T001 Create `prober/result.go` — define `ProbeResult` struct with Zone, Check, Nameserver, IP, Success, Duration, Metrics (map[string]float64), Labels (map[string]string)
- [x] T002 Create `cache/delegation.go` — delegation cache with `sync.RWMutex`, Get/Set methods, TTL-based expiry, Invalidate method
- [x] T003 Create `cycle/runner.go` — stub `CycleRunner` struct with `Run(ctx, config, cache, logger)` method signature returning `[]ProbeResult`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Refactor probers to return results instead of writing to registry. MUST complete before user stories.

**⚠️ CRITICAL**: This changes every prober file and test. All existing tests must still pass after refactoring.

- [x] T004 Refactor `prober/prober.go` — change `ProbeFn` signature from `(ctx, zone, client, registry, logger) error` to `(ctx, zone, client, logger) ([]ProbeResult, error)`. Remove `RunProber` function (replaced by cycle runner). Keep `WalkDelegation`, `discoverNameservers`, `resolveHostname` unchanged for now.
- [x] T005 Refactor `prober/soa.go` — `ProbeSOA` returns `[]ProbeResult` with SOA field values in `Metrics` map. Remove all `newGauge` calls. Remove `query_success` and `query_duration` (these become fields on ProbeResult).
- [x] T006 [P] Refactor `prober/recursion.go` — `ProbeRecursion` returns `[]ProbeResult`. Remove `newGauge` calls.
- [x] T007 [P] Refactor `prober/glue.go` — `ProbeGlue` returns `[]ProbeResult` with source label in Labels map. Remove `newGauge` calls.
- [x] T008 Create `prober/registry.go` — `BuildRegistry(results []ProbeResult) *prometheus.Registry` function that creates a registry and registers all metrics from ProbeResult data. This centralizes metric creation.
- [x] T009 Refactor all prober integration tests (`prober/soa_test.go`, `prober/recursion_test.go`, `prober/glue_test.go`) — update to call probers with new signature, use `BuildRegistry` to create registry from results, then assert with existing `testutil` helpers. All 18 existing tests must pass.
- [x] T010 Add config fields to `config/config.go` — `ProbeInterval` (duration, default 60s), `DelegationCacheTTL` (duration, default 30m), `QueryTimeout` (duration, default 5s), `ZoneDeadline` (duration, default 30s)
- [x] T011 Add unit tests for new config fields in `config/config_test.go` — test parsing with all new fields, test defaults when omitted

**Checkpoint**: All probers return `[]ProbeResult`, `BuildRegistry` works, all 18 existing integration tests pass with new architecture. No behavioral change yet — just refactored internals.

---

## Phase 3: User Story 1 — Metrics Reflect Current DNS State (Priority: P1) 🎯 MVP

**Goal**: Background probe loop runs periodically, `/metrics` serves fresh results.

**Independent Test**: Start exporter, change DNS state in test fixtures, verify metrics update after next cycle.

### Tests for US1

- [ ] T012 [US1] Integration test: probe cycle runs and updates metrics in `cycle/runner_test.go` — start fixture DNS servers, run one cycle, verify ProbeResults contain expected metrics, build registry, assert metrics present
- [ ] T013 [US1] Integration test: metrics refresh on subsequent cycles in `cycle/runner_test.go` — run cycle with serial X, change fixture to serial Y, run another cycle, verify serial Y in results
- [ ] T014 [US1] Integration test: 503 before first cycle completes in `main_test.go` — start exporter, immediately curl `/metrics`, assert HTTP 503
- [ ] T015 [US1] Integration test: stale NS removed from metrics in `cycle/runner_test.go` — cycle 1 has ns1+ns2, cycle 2 fixture removes ns2, verify ns2 metrics absent in cycle 2 results
- [ ] T016 [US1] Integration test: cycle overlap prevention in `cycle/runner_test.go` — configure very short interval with slow fixture, verify cycles don't stack

### Implementation for US1

- [x] T017 [US1] Implement cycle runner in `cycle/runner.go` — fan out goroutine per zone, each zone: get delegation (from cache or walk), scatter DNS queries per NS per check, gather `[]ProbeResult`, collect all results, return
- [x] T018 [US1] Implement per-query timeout and per-zone deadline in `cycle/runner.go` — each DNS query gets `QueryTimeout` context, each zone goroutine gets `ZoneDeadline` context, cancel outstanding queries when deadline expires
- [x] T019 [US1] Implement atomic metric swap in `main.go` — `atomic.Pointer[CycleState]` holds current cycle registry, background goroutine runs cycle on ticker, swaps CycleState on completion, `/metrics` handler gathers from permanent + cycle registries
- [x] T020 [US1] Implement 503 before first cycle in `main.go` — `/metrics` handler checks if CycleState is nil, returns 503 if so
- [x] T021 [US1] Implement cycle overlap prevention in `main.go` — if previous cycle still running when ticker fires, skip and log warning
- [x] T022 [US1] Wire delegation cache into cycle runner — `WalkDelegation` checks cache first, populates cache on miss, uses `DelegationCacheTTL` from config

**Checkpoint**: Exporter runs probe cycles on a timer, `/metrics` serves fresh results, stale NS removed naturally, 503 before first cycle.

---

## Phase 4: User Story 2 — Config Reload (Priority: P2)

**Goal**: SIGHUP and `/-/reload` reload configuration without restart.

**Independent Test**: Start exporter, modify config, send SIGHUP, verify new zones appear in next cycle.

### Tests for US2

- [ ] T023 [US2] Integration test: reload adds new zone in `main_test.go` — start exporter with zone A, write new config with zones A+B, trigger reload, verify zone B metrics appear
- [ ] T024 [US2] Integration test: reload removes zone in `main_test.go` — start with A+B, reload with only A, verify B metrics absent
- [ ] T025 [US2] Integration test: invalid reload keeps old config in `main_test.go` — start with A, write invalid config, trigger reload, verify A still probed, error logged
- [ ] T026 [US2] Integration test: reload invalidates delegation cache in `cache/delegation_test.go` — populate cache, call Invalidate, verify next access is a cache miss

### Implementation for US2

- [x] T027 [US2] Implement SIGHUP handler in `main.go` — on SIGHUP: read config file, validate, if valid: swap config atomically + invalidate delegation cache, if invalid: log error
- [x] T028 [US2] Implement `/-/reload` POST handler in `main.go` — same logic as SIGHUP, return 200 on success, 500 on failure with error message
- [x] T029 [US2] Ensure probe cycle reads config atomically in `main.go` — cycle runner receives config snapshot at start of each cycle, not a pointer to mutable config

**Checkpoint**: Config reload works via SIGHUP and HTTP. Invalid configs rejected gracefully. Cache invalidated on reload.

---

## Phase 5: User Story 3 — Graceful Failures Under Load (Priority: P3)

**Goal**: Parallel probing, delegation caching, retries, operational metrics.

**Independent Test**: Configure 10+ zones with mixed healthy/unhealthy fixtures, verify all complete within bounded time.

### Tests for US3

- [ ] T030 [US3] Integration test: parallel zone probing in `cycle/runner_test.go` — configure 5 zones with separate fixtures, verify all 5 probed (not sequential timing)
- [ ] T031 [US3] Integration test: slow zone doesn't block others in `cycle/runner_test.go` — one zone has Drop fixture (timeout), other zones complete normally
- [ ] T032 [US3] Integration test: delegation cache hit in `cache/delegation_test.go` — walk delegation, verify cached, walk again within TTL, verify no DNS query made
- [ ] T033 [US3] Integration test: delegation cache expiry in `cache/delegation_test.go` — walk, wait past TTL, walk again, verify fresh DNS query made
- [ ] T034 [US3] Integration test: retry on transient failure in `cycle/runner_test.go` — fixture drops first query but responds to second (retry), verify success
- [ ] T035 [US3] Integration test: no retry on NXDOMAIN in `cycle/runner_test.go` — fixture returns NXDOMAIN, verify only one query made (no retry)
- [ ] T036 [US3] Unit test: operational counters increment in `cycle/runner_test.go` — run cycle, verify `dnshealth_dns_queries_total`, `dnshealth_dns_query_duration_seconds_total`, `dnshealth_dns_timeouts_total` counters on permanent registry

### Implementation for US3

- [ ] T037 [US3] Implement retry logic in `cycle/runner.go` — on timeout/network error: retry once with half timeout. On NXDOMAIN/REFUSED: no retry.
- [ ] T038 [US3] Implement operational counters on permanent registry in `cycle/runner.go` — `dnshealth_dns_queries_total{server}`, `dnshealth_dns_query_duration_seconds_total{server}`, `dnshealth_dns_timeouts_total{server}`, `dnshealth_probe_cycle_duration_seconds`, `dnshealth_probe_zones_total`
- [ ] T039 [US3] Implement delegation cache TTL + concurrency in `cache/delegation.go` — `sync.RWMutex`, Get returns nil on miss or expired, Set stores with timestamp

**Checkpoint**: All zones probed concurrently, delegation cached, retries work, operational counters visible.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T040 Update `README.md` — document new config options (probe_interval, delegation_cache_ttl, query_timeout, zone_deadline), config reload (SIGHUP + /-/reload), operational metrics
- [ ] T041 Update `dnshealth.yml` example config — add commented-out examples of new fields with defaults
- [ ] T042 Run `go vet ./...` and `gofmt -s -w .`
- [ ] T043 Verify all integration tests pass: `go test -tags=integration -count=1 -v ./...`
- [ ] T044 Manual validation: run exporter with real domains, verify metrics refresh every interval, test SIGHUP reload

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Phase 1 (needs ProbeResult type). BLOCKS all user stories.
- **US1 (Phase 3)**: Depends on Phase 2 (refactored probers)
- **US2 (Phase 4)**: Depends on US1 (needs cycle runner + config swap)
- **US3 (Phase 5)**: Depends on US1 (needs cycle runner). Cache tasks (T032-T033, T039) can run in parallel with US1.
- **Polish (Phase 6)**: Depends on all user stories

### Parallel Opportunities

- T005, T006, T007: Prober refactors are independent files
- T012-T016: US1 tests can be written in parallel
- T023-T026: US2 tests can be written in parallel
- T030-T036: US3 tests can be written in parallel
- T032-T033, T039: Cache tasks independent of cycle runner

---

## Implementation Strategy

### MVP First (US1 only)

1. Phase 1 + Phase 2 → refactored probers, all existing tests pass
2. Phase 3 → probe loop running, metrics refresh
3. **STOP**: exporter is now production-viable for basic use

### Full Feature

4. Phase 4 → config reload
5. Phase 5 → parallelism, caching, retries, operational metrics
6. Phase 6 → polish

---

## Notes

- The prober refactor (Phase 2) is the riskiest part — it touches every prober and every test. Get this right before moving forward.
- Existing 18 integration tests are the safety net for the refactor. If they pass after Phase 2, the refactor is correct.
- The `testutil/fixture.go` `Probe()` method needs updating to work with the new prober signature. Consider adding a `ProbeAndBuild()` helper that calls the prober and runs `BuildRegistry`.
