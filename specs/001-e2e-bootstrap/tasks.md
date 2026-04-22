# Tasks: E2E Bootstrap

**Input**: Design documents from `specs/001-e2e-bootstrap/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/metrics.md

**Tests**: Integration tests are a core deliverable of this feature (Constitution Principle I, VIII; User Story 3). Test tasks are included.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- Go project at repository root: `main.go`, `config/`, `prober/`, `testutil/`, `testdata/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Bootstrap Go project, dependencies, and build tooling

- [ ] T001 Initialize Go module and add dependencies in `go.mod` (`miekg/dns`, `prometheus/client_golang`, `prometheus/exporter-toolkit`, `prometheus/common`, `alecthomas/kingpin/v2`, `go.yaml.in/yaml/v3`)
- [ ] T002 Create `Makefile` with targets: `build`, `test`, `test-integration`, `vet`, `fmt`, `docker-up`, `docker-down`
- [ ] T003 [P] Create example config file `dnshealth.yml` with two sample zones

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [ ] T004 Implement YAML config parsing and validation in `config/config.go` — load zone list from file, fail on empty/invalid config
- [ ] T005 Implement ProbeFn type and prober registry in `prober/prober.go` — define the `ProbeFn` signature, a map of check name → function, and common metrics (`dnshealth_check_success`, `dnshealth_check_duration_seconds`)
- [ ] T006 Create Docker Compose file `testdata/docker-compose.yml` — 4 CoreDNS containers (`dns-root` on `127.240.0.1`, `dns-ns1` on `127.240.0.2`, `dns-ns2` on `127.240.0.3`, `dns-ns3` on `127.240.0.4`) each with `reload 1s` and runtime zone mounts
- [ ] T007 [P] Create static Corefiles in `testdata/coredns/root/Corefile`, `testdata/coredns/ns1/Corefile`, `testdata/coredns/ns2/Corefile`, `testdata/coredns/ns3/Corefile` — each points to its runtime zone directory
- [ ] T008 [P] Create `testdata/coredns/runtime/` directory structure with `.gitkeep` files for `ns1/zones/`, `ns2/zones/`, `ns3/zones/`, `root/zones/`
- [ ] T009 Implement DNS record helpers in `testutil/records.go` — `SOA(opts...)`, `NS(name)`, `A(name, ip)`, `ZoneFile(zone, records...)` with defaults-with-override pattern over `miekg/dns` types
- [ ] T010 Implement test fixture manager in `testutil/fixture.go` — `NewDNSFixture(t)`, `WriteZone(container, content)` (replaces entire zone dir for container), `Reload(t)` (triggers CoreDNS reload and waits), `Probe(fn, zone)` (calls prober with fresh registry and returns it)
- [ ] T011 Implement assertion helpers in `testutil/assertions.go` — `AssertGauge(t, registry, name, opts...)`, `AssertGaugeExists(t, ...)`, `AssertGaugeMissing(t, ...)`, `WithLabels(pairs...)`, `WithValue(v)`
- [ ] T012 Create `TestMain` in `prober/integration_test.go` — start Docker Compose fixtures on suite entry (skip if Docker unavailable), tear down on exit. Guard with `//go:build integration` tag.

**Checkpoint**: Foundation ready — Go project builds, Docker fixtures start, test helpers available. User story implementation can begin.

---

## Phase 3: User Story 1 — Operator Scrapes Metrics (Priority: P1) 🎯 MVP

**Goal**: Exporter reads zone config, runs checks, exposes Prometheus metrics on `/metrics`

**Independent Test**: Start exporter with test config, curl `/metrics`, verify `dnshealth_` prefixed metrics with zone labels in valid Prometheus exposition format.

### Tests for User Story 1

- [ ] T013 [US1] Integration test for SOA prober in `prober/soa_test.go` — fixture writes zone with known SOA serial to ns1 and ns2, calls SOA prober, asserts `dnshealth_soa_serial` gauge matches for each nameserver with correct zone/nameserver/ip labels
- [ ] T014 [US1] Integration test for SOA serial drift in `prober/soa_test.go` — fixture writes different serials to ns1 vs ns2, calls SOA prober, asserts different serial values per nameserver
- [ ] T015 [US1] Integration test for recursion-available prober in `prober/recursion_test.go` — fixture sets up authoritative-only CoreDNS, calls recursion prober, asserts `dnshealth_ns_recursion_available` is 0 for each NS
- [ ] T016 [US1] Integration test for glue consistency (happy path) in `prober/glue_test.go` — fixture writes matching NS+glue to root and ns1/ns2, calls glue prober, asserts `dnshealth_ns_record` and `dnshealth_ns_glue` metrics present with both `source="parent"` and `source="self"` for each NS
- [ ] T017 [US1] Integration test for glue mismatch in `prober/glue_test.go` — fixture writes mismatched NS records to ns3 vs root, calls glue prober, asserts `dnshealth_ns_record` shows discrepancy (record with `source="self"` that has no corresponding `source="parent"` or vice versa)

### Implementation for User Story 1

- [ ] T018 [US1] Implement SOA prober in `prober/soa.go` — query SOA record from each nameserver for a zone, register `dnshealth_soa_serial`, `dnshealth_soa_refresh_seconds`, `dnshealth_soa_retry_seconds`, `dnshealth_soa_expire_seconds`, `dnshealth_soa_minimum_seconds` gauges with zone/nameserver/ip labels
- [ ] T019 [US1] Implement recursion-available prober in `prober/recursion.go` — query each nameserver with RD flag set, check RA bit in response, register `dnshealth_ns_recursion_available` gauge with zone/nameserver/ip labels
- [ ] T020 [US1] Implement glue consistency prober in `prober/glue.go` — query parent for NS records + glue (A records), query each authoritative NS for its own NS + A records, register `dnshealth_ns_record` and `dnshealth_ns_glue` info metrics with zone/nameserver/ip/source labels
- [ ] T021 [US1] Implement `main.go` — parse flags with kingpin (`--config.file`, `--web.listen-address`, `--web.config.file`, `--log.level`, `--log.format`, `--version`), load config, set up promslog logger, create Prometheus registry, register `dnshealth_build_info` gauge, run all probers for each configured zone, serve `/metrics` via exporter-toolkit web server with landing page
- [ ] T022 [US1] Verify all metrics pass `promtool check metrics` validation — run exporter against Docker fixtures, curl `/metrics`, pipe through `promtool check metrics`

**Checkpoint**: Exporter builds, starts, runs SOA + recursion + glue checks against Docker fixtures, and exposes valid Prometheus metrics. All US1 integration tests pass.

---

## Phase 4: User Story 2 — Operator Starts and Stops the Exporter (Priority: P2)

**Goal**: Binary handles lifecycle correctly — standard flags, graceful shutdown, fail-fast on bad config

**Independent Test**: Start binary, verify startup log, send SIGTERM, verify clean exit code 0. Start with invalid config, verify non-zero exit.

### Tests for User Story 2

- [ ] T023 [US2] Integration test for graceful shutdown in `main_test.go` — start exporter binary as subprocess, verify it's listening, send SIGTERM, assert exit code 0 and shutdown log message
- [ ] T024 [US2] Integration test for invalid config in `main_test.go` — start exporter binary with invalid config file, assert non-zero exit code and error message on stderr

### Implementation for User Story 2

- [ ] T025 [US2] Add signal handling to `main.go` — listen for SIGTERM/SIGINT, trigger graceful shutdown (close HTTP server with context timeout, flush logs), exit 0 on clean shutdown
- [ ] T026 [US2] Add config validation to `config/config.go` — fail fast with clear error messages for: missing file, invalid YAML, empty zone list, zone names that don't look like domain names
- [ ] T027 [US2] Add `/-/healthy` endpoint to `main.go` — returns 200 OK when the exporter is running

**Checkpoint**: Binary starts cleanly, handles signals, fails fast on bad config. All US2 tests pass.

---

## Phase 5: User Story 3 — Developer Runs Integration Tests (Priority: P3)

**Goal**: Developer experience for running tests is smooth and documented

**Independent Test**: Clone repo, run `go test ./...` (unit tests pass without Docker), then `make docker-up && make test-integration` (integration tests pass against fixtures).

### Implementation for User Story 3

- [ ] T028 [US3] Add unit tests for config parsing in `config/config_test.go` — test valid config, empty zones, missing file, invalid YAML. No Docker dependency.
- [ ] T029 [US3] Add unit tests for record helpers in `testutil/records_test.go` — verify `SOA()`, `NS()`, `A()`, `ZoneFile()` produce valid `miekg/dns` records and zone file text. No Docker dependency.
- [ ] T030 [US3] Update `README.md` with quickstart instructions — build, configure, run, verify metrics, run unit tests, run integration tests (Docker Compose up/down commands)
- [ ] T031 [US3] Add `.gitignore` entries for `testdata/coredns/runtime/*/zones/*.zone` (generated at test time, should not be committed)

**Checkpoint**: Developer can clone, build, run unit tests (no Docker), and run integration tests (with Docker). README documents the full workflow.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final validation and cleanup

- [ ] T032 Run `go vet ./...` and fix any issues
- [ ] T033 Run `gofmt -s -w .` to ensure consistent formatting
- [ ] T034 Verify all integration tests pass end-to-end: `make docker-up && make test-integration && make docker-down`
- [ ] T035 Run quickstart.md validation — follow the quickstart steps from scratch and verify they work
- [ ] T036 Verify `promtool check metrics` passes against live exporter output

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion — BLOCKS all user stories
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
  - US1 (metrics) must complete before US2 (lifecycle) can be fully tested
  - US3 (developer experience) can proceed in parallel with US2 after US1
- **Polish (Phase 6)**: Depends on all user stories being complete

### Within Each User Story

- Integration tests written first (T013-T017 before T018-T021)
- Prober implementations before main.go wiring
- Verify tests fail, then implement, then verify tests pass

### Parallel Opportunities

- T003, T007, T008 can run in parallel (different files, no dependencies)
- T009, T010, T011 can run in parallel (different files in `testutil/`)
- T013-T017 (US1 tests) can be written in parallel
- T018-T020 (US1 probers) can be implemented in parallel after tests exist
- T028-T031 (US3 tasks) can all run in parallel

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL — blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: `make docker-up && go test -tags=integration ./prober/...` all pass, `curl localhost:9199/metrics` returns valid metrics
5. Commit and assess

### Incremental Delivery

1. Setup + Foundational → project builds, Docker fixtures work
2. User Story 1 → exporter runs checks and serves metrics (MVP!)
3. User Story 2 → graceful shutdown, fail-fast validation
4. User Story 3 → developer docs, unit tests, polished DX
5. Polish → final validation sweep

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks
- [Story] label maps task to specific user story for traceability
- Integration tests use `//go:build integration` tag — `go test ./...` runs only unit tests
- All integration tests call prober functions in-process (not the binary), except US2 lifecycle tests which test the binary as a subprocess
- Commit after each task or logical group
- Stop at any checkpoint to validate independently
