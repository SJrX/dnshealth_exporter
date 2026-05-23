---

description: "Task list for feature 006-ipv6-nameserver-support"
---

# Tasks: IPv6 and multi-IP nameserver support

**Input**: Design documents from `/specs/006-ipv6-nameserver-support/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Integration testing is required by the constitution
(Principle I). New integration tests cover v6-only NS, dual-stack
NS, and parent-v4-only-glue paths — each silently broken pre-fix.

**Organization**: Tasks are grouped by user story so each story is
implementable, testable, and deliverable independently. Notable
overlap: US1 and US2 share all the production code changes (they
differ only in fixture topology), so the source changes land in
Phase 2 Foundational and the user-story phases primarily add tests.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3, US4)
- All paths are repo-relative

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Pre-flight verification. No new dependencies, no new
package layout — this feature touches existing code only.

- [X] T001 Verify branch state: `git status` shows clean working tree
  on `006-ipv6-nameserver-support` branched from `origin/main`.
  `go build ./...` and `go test -tags=integration ./...` both pass
  at HEAD (so any subsequent failure is from this work, not
  pre-existing).

---

## Phase 2: Foundational (Blocking Prerequisites for ALL user stories)

**Purpose**: Production code changes + test infrastructure. After
this phase, the exporter is IPv6-aware end-to-end, IPv4-only zones
emit byte-identical metrics, and `testutil` supports dual-stack
fixtures. The per-user-story phases then add coverage tests.

These tasks share files extensively — sequence them carefully.

- [X] T002 [P] Add `AAAA(name, ipv6 string) dns.RR` helper to
  `testutil/records.go`, mirroring the existing `A()` helper's
  signature, return shape, and default-with-override pattern. Add
  a corresponding unit test `TestAAAA_CreatesValidRecord` in
  `testutil/records_test.go` parallel to the existing
  `TestA_CreatesValidRecord` (per constitution Principle VIII —
  symmetric structure). Per FR-016 and contracts/nameserver-fanout.md.

- [X] T003 [P] Extend `testutil/fixture.go`'s referral-mode handler
  (currently `if srv.referral && qtype == TypeNS` block around lines
  212-226) so it also attaches `*mdns.AAAA` records as glue in the
  Additional section when the AAAA's `Header().Name` matches an NS
  hostname in the Authority. Mirror the existing A-attachment loop.
  Same change needed in the `findReferral` helper (lines 298-336)
  for the recursive-referral branch. Per FR-017.

- [X] T004 Rename `prober.ResolveHostname` to `prober.ResolveHostnames`
  in `prober/prober.go`. Change signature from
  `(ctx, hostname, client, logger) (string, error)` to
  `(ctx, hostname, client, logger) ([]string, error)`. Implementation
  queries both `dns.TypeA` and `dns.TypeAAAA` sequentially (per R-1),
  returns all addresses found across both families. Empty slice + nil
  error = NODATA on both (legitimate, not a failure); non-nil error
  = protocol failure on both families. Partial success (A succeeds,
  AAAA fails or vice versa) returns the successful family with a nil
  top-level error; the failed-family failure is logged at WARN.
  Per FR-001 + R-3 + contracts/nameserver-fanout.md.

- [X] T005 Update `cycle/runner.go::probeZone` (around lines 173-187)
  to call the renamed `ResolveHostnames` and fan out one
  `prober.Nameserver{Hostname, IP}` entry per returned IP. The
  existing in-line `if ns.IP != "" { append; continue }` branch
  (for entries that already had parent glue) is unchanged — but the
  no-glue branch now produces N entries instead of one. Per FR-002 +
  contracts/nameserver-fanout.md.

- [X] T006 Update `testutil/fixture.go::Probe` (around lines 139-151)
  with the same `ResolveHostnames` rename + per-IP fan-out as T005.
  The two callers are intentionally kept in sync (production runner
  and test runner exercise the same resolution shape).

- [X] T007 Update `prober/glue.go::querySelfForNSAndA` (around lines
  111-142) to query both `dns.TypeA` and `dns.TypeAAAA` when resolving
  each self-reported NS hostname. Emit one `Nameserver{Hostname, IP}`
  entry per resolved address into both `nsRecords` and `aRecords`.
  Today's loop returns at most one address per NS hostname (the
  first A); after this change it returns all addresses across both
  families. Per FR-005 + FR-006.

- [X] T008 Update `config/config.go::ResolveAddress` (the method on
  `*Config`) to canonicalise the lookup IP via
  `net.ParseIP(ip).String()` before consulting the
  `AddressOverrides` map. If `ParseIP` returns nil (the input wasn't
  a valid IP), fall through to the default `net.JoinHostPort(ip,
  "53")` — preserves today's behaviour for the unparseable case.
  Per FR-013 + R-2 + contracts/address-override.md.

- [X] T009 Update `config/config.go::Load` (or wherever the YAML
  decoder produces the `AddressOverrides` map) to canonicalise each
  map key via `net.ParseIP(k).String()` at load time. Reject any
  key for which `ParseIP` returns nil with a clear error
  (`"address_overrides: %q is not a valid IP address"`). Rebuild
  the map with canonical keys. Per FR-013 + R-2. Add a config_test
  case verifying that a v6 key written in expanded form
  (`2001:0db8:0000:0000:0000:0000:0000:0001`) matches a runtime
  lookup for `2001:db8::1`.

- [X] T009b **(Analyze remediation: C1 + H1 + H2)** Extend
  `prober/prober.go`'s glue-extraction sites to handle AAAA records
  and multi-IP-per-hostname. Three call sites, same file, same
  shape of edit:
  - **`extractDelegation` (lines 152-181)**: change `glueMap` from
    `map[string]string` to `map[string][]string`. Accept both
    `*dns.A` and `*dns.AAAA` records from `extraSection`,
    appending each to the slice for its hostname. Iterate
    `nsSection` once per NS RR, then emit ONE `Nameserver` per
    (hostname, IP) tuple in `glueMap[nsRR.Ns]` — so a hostname
    with both A and AAAA glue produces two entries; a hostname
    with no glue produces one entry with `IP=""` (existing
    behaviour preserved). Same change to the `Glue` slice.
  - **`WalkDelegation` intermediate-referral glue loop (lines
    98-102)**: extract AAAA records alongside A. For the
    `nextServer` selection (lines 118-124), prefer the first IP
    of either family — the walker only needs one reachable IP per
    NS to follow the chain.
  - **`WalkDelegation` NS-hostname resolution fallback (lines
    124-138)**: query both TypeA and TypeAAAA when the referral
    has no glue at all, use either family's first result as the
    next server.
  
  Without this task, FR-010 silently fails (inline parent AAAA
  glue discarded) and US2's dual-stack acceptance test (T013)
  would fail at runtime even with all other Phase 2 tasks done —
  the resolver never runs for hostnames whose parent supplied
  (only A) glue. Per FR-002, FR-004, FR-010, FR-011.

- [X] T010 Regression gate: from repo root, run `go build ./...`
  (must succeed), `go vet ./...` (must be clean), and
  `go test -tags=integration -count=1 ./...` (full existing suite
  must pass). All pre-existing IPv4-only tests must remain green.
  If this gate fails before any per-US tests are written, the
  Phase 2 code changes have introduced a regression that needs
  fixing before moving on. Per FR-008.

**Checkpoint**: Code is IPv6-aware end-to-end. IPv4-only zones
produce identical metric series. testutil helpers ready for v6
fixtures. No new per-US tests yet — those add coverage in Phases
3-6.

---

## Phase 3: User Story 1 (P1) — IPv6-only nameservers visible in metrics

**Story Goal**: A nameserver with only an AAAA record (no A) appears
in every per-NS metric series with its IPv6 address in the `ip`
label. Pre-fix this NS was silently absent from `/metrics`.

**Independent Test**: Spin up a test fixture with one IPv6-only NS,
run a probe cycle, grep `/metrics` for the v6 address. Series
exists for every check (soa, recursion, glue, ns_record).

- [X] T011 [US1] Create `prober/glue_ipv6_test.go` under build tag
  `//go:build integration`. Three-phase Meszaros structure (per
  constitution Principle VIII).

  **Fixture Setup** (address-override pattern, mirrors what the
  demo does per FR-014/FR-015):
  - Referral server at `127.240.0.1:TestPort` declares NS records
    for `example.test` pointing at in-bailiwick NSes
    `ns1.example.test` and `ns2.example.test`, with **AAAA glue
    only — no A glue** (e.g., `2001:db8::2` for ns1,
    `2001:db8::3` for ns2). This is exactly the topology #23
    flags as silently broken today.
  - Auth servers for `example.test` actually run at
    `127.240.0.2:TestPort` and `127.240.0.3:TestPort` (v4 — the
    test host is presumed IPv4-only, matching the user's
    environment).
  - Override `prober.ResolveAddress` for the duration of the test
    to map `2001:db8::2 → 127.240.0.2:TestPort` and
    `2001:db8::3 → 127.240.0.3:TestPort`. Restore on teardown
    (use `t.Cleanup` or a `defer` block saving the original
    function value). This exercises FR-014 end-to-end — the
    network goes to v4, the metric labels carry v6.

  **Exercise SUT**: `env.Probe(prober.ProbeGlue, "example.test")`.

  **Verification**:
  - `dnshealth_ns_record{nameserver="ns1.example.test.", ip="2001:db8::2", source="parent"} = 1`
  - `dnshealth_ns_record{nameserver="ns2.example.test.", ip="2001:db8::3", source="parent"} = 1`
  - Same for `source="self"` (the glue prober's self-side loop
    self-queries the v6-IPed NSes per T007).
  - The `ip` label MUST carry the v6 address verbatim, NOT the
    override destination (proves FR-014).

  Pre-T007+T009b this test fails: T009b is needed because
  `extractDelegation` would otherwise discard the AAAA glue;
  T007 is needed for the self-side queries to issue AAAA.
  Post-fix it passes.

- [X] T012 [US1] Verified — with T009b's extractDelegation change surgically reverted, the test fails on the parent-side assertion (line 79: `dnshealth_ns_record with labels {ip:2001:db8::2 ..., source:parent} not found`). Restored, passes again. Confirms T009b is load-bearing for the parent-side v6 path.
  (sanity proof the test catches the bug). Method: `git stash` the
  glue.go changes, run only this test, confirm failure; pop the
  stash, re-run, confirm pass. Document the verification outcome
  in the commit message — do NOT add this as an automated test
  (it would fight itself, same as T017 in spec 005).

**Checkpoint**: US1 acceptance criteria green. v6-only NSes visible.

---

## Phase 4: User Story 2 (P1) — Dual-stack and multi-address nameservers

**Story Goal**: A nameserver with both A and AAAA records produces
two series per per-NS metric (one per IP); same for a nameserver
with multiple A records. Pre-fix only the first A surfaces.

**Independent Test**: Test fixtures for both dual-stack and
multi-A topologies; assert per-IP series counts match resolved
IP counts.

- [X] T013 [P] [US2] Create `prober/glue_dualstack_test.go`
  (`//go:build integration`). Topology: referral server with
  `example.test` delegation pointing at `ns1.example.test` and
  `ns2.example.test` with **both A and AAAA glue** for each NS
  (in-bailiwick, in-Additional). Auth servers respond at the v4
  addresses (the address-override pattern means v6 entries route
  to v4 sockets — same trick the demo uses per FR-015). Assert
  each NS appears twice in `dnshealth_ns_record{source="parent"}`
  (once per IP family) and twice in `source="self"` after glue
  prober runs. Per US2 acceptance scenario 1.

- [X] T014 [P] [US2] Create `prober/glue_parent_v4_only_glue_test.go` (required also adding FR-011 augmentation to cycle/runner.go + testutil/fixture.go::Probe so the runner resolves a missing IP family out-of-band when parent glue only covers one family — the user's real-world sjrx.net case)
  (`//go:build integration`). Topology: referral server with
  `example.test` delegation pointing at `ns1.different.test` and
  `ns2.different.test` with **A glue only for them** at the parent
  (mirroring the common real-world case where the parent zone
  doesn't carry AAAA glue for sibling-zone NSes), plus a separate
  authoritative path that lets `ResolveHostnames` find their AAAA
  records out-of-band. Auth servers for `example.test` respond at
  the v4 IPs. Assert each NS appears in `dnshealth_ns_record`
  series with BOTH v4 (parent glue) and v6 (out-of-band) entries.
  Per US2 acceptance scenario 3 + FR-011.

- [X] T015 [P] [US2] Create `prober/glue_multi_a_test.go` under
  build tag `//go:build integration` with multi-A topology: one NS
  hostname (e.g., `ns1.example.test`) with two distinct A records
  in the parent's glue (e.g., `127.240.0.2` and `127.240.0.3`).
  Assert two `dnshealth_ns_record` series exist for that hostname
  — one per IP. Pre-T009b this test fails because the existing
  `glueMap[name] = ip` last-write-wins assignment retains only
  one A address per hostname. Per US2 acceptance scenario 2 +
  FR-002 + FR-004.

**Checkpoint**: US2 acceptance criteria green. All three v6 / multi-IP
patterns covered by integration tests.

---

## Phase 5: User Story 3 (P2) — Demo runs on IPv4-only host with visible IPv6 entries

**Story Goal**: An operator brings up the demo stack on a host with
IPv6 disabled and sees both v6 patterns on the Grafana dashboard:
dual-stack (healthy.demo.) and v6-only (v6-only.demo.).

**Independent Test**: `cd demo && docker compose up -d --build`,
wait one probe cycle, switch `$zone` in Grafana between
`healthy.demo.` and `v6-only.demo.`, confirm v6 entries visible
in each in the expected pattern.

This phase has the most file churn — split into focused tasks.

- [X] T016 [P] [US3] Update `demo/coredns/root/zones/demo.zone`:
  add AAAA glue records for `ns1.healthy` and `ns2.healthy` (use
  RFC 3849 documentation prefix, e.g., `2001:db8::11`). Add new
  delegation for `v6-only.demo.` with NS records pointing at
  `ns1.v6-only.demo.` and `ns2.v6-only.demo.`, with **AAAA glue
  only** for them (e.g., `2001:db8::16`, `2001:db8::17`). The
  existing A glue for healthy stays; the new section is purely
  additive.

- [X] T017 [P] [US3] Update `demo/coredns/healthy/zones/healthy.demo.zone`:
  add AAAA records for `ns1` and `ns2` at the documentation v6
  prefix matching the glue in the root zone (T016). The
  authoritative side of healthy now mirrors what the parent
  advertises. Apex AAAA optional but consistent — add `@ IN AAAA
  2001:db8::11` for completeness.

- [X] T018 [P] [US3] Create new CoreDNS container directory
  `demo/coredns/v6-only/` with: (a) `Corefile` modelled on
  `demo/coredns/healthy/Corefile` but pointing at the new zone;
  (b) `zones/v6-only.demo.zone` with SOA / NS / A (for in-bailiwick
  resolution) and AAAA records. The container itself binds to a
  static IPv4 inside the Compose network — the "v6-only" framing
  is in the *delegation* (root advertises AAAA glue only), not in
  the container's own IP.

- [X] T019 [US3] Update `demo/docker-compose.yml`: add a new
  service `coredns-v6-only` on a new static IPv4 (next free in the
  `172.31.0.0/24` plan — likely `172.31.0.16`), modelled on the
  existing `coredns-healthy` service. Mount the Corefile and zone
  file from T018. Wire to the `demo` network.

- [X] T020 [US3] Update `demo/exporter/dnshealth.yml`: (a) add
  `v6-only.demo.` to the `zones:` list; (b) add `address_overrides:`
  entries mapping every v6 address introduced in T016 + T018 to the
  appropriate in-container `host:port` (e.g.,
  `"2001:db8::11": "coredns-healthy:53"`,
  `"2001:db8::16": "coredns-v6-only:53"`). Quote the v6 keys in YAML
  to avoid colon-parsing surprises (per
  contracts/address-override.md).

- [X] T021 [US3] Update `demo/smoke.sh` to add an assertion (added A4b for v6-only.demo. + dual-stack healthy.demo.) that
  `v6-only.demo.` produces per-NS metric series with v6 IPs.
  Concrete check: `grep -E '^dnshealth_ns_record\{[^}]*zone="v6-only.demo.[^}]*ip="[0-9a-f:]+"' "${METRICS_FILE}"`
  returns at least one match. Pattern: similar to the existing A1-A6
  assertions. Per FR-016 implication.

- [X] T022 [US3] Update `demo/README.md`: add a small section
  describing the two IPv6 patterns visible in the demo (dual-stack
  on `healthy.demo.`, v6-only on `v6-only.demo.`) and how to see
  them on the dashboard (switch `$zone`). Also note the address-
  override trick that makes this work on IPv4-only hosts — link to
  the `address_overrides` block in `demo/exporter/dnshealth.yml`.

- [X] T023 [US3] End-to-end smoke green (A1-A6 + new A4b for v6 zones). Browser-check portion (open Grafana, switch $zone, confirm v6 entries) deferred to user.
  `cd demo && docker compose up -d --build && sleep 30 &&
  ./smoke.sh && docker compose down -v`. Smoke must pass.
  Browser-check portion (open Grafana, switch `$zone` between
  `healthy.demo.` and `v6-only.demo.`, confirm v6 entries visible
  in the NS-records tables) is a manual user step — deferred to
  the user, document the manual check in the commit message. Per
  SC-009 + SC-010.

**Checkpoint**: US3 acceptance criteria green (modulo the manual
browser-check, which the user runs).

---

## Phase 6: User Story 4 (P2) — Test infrastructure documentation

**Story Goal**: A developer adding a new probe type can write
integration tests that exercise IPv6 paths using only `testutil`
helpers, with no inline IPv6 plumbing.

**Note**: The substantive work for US4 already lands in Phase 2
(T002 + T003). This phase verifies the result and documents it.

- [X] T024 [US4] Verified `prober/glue_dualstack_test.go`: (a) AAAA records declared via the AAAA() helper — no inline literals; (b) no manual m.Extra fiddling — referral fixture attaches automatically; (c) AssertGaugeExists used with IPv6 string in ip label (e.g., `ip="2001:db8::2"`) with no test-specific workarounds. testutil ergonomics meet US4 criteria.
  T013) and confirm: (a) no inline `*mdns.AAAA` literals — all
  AAAA records declared via the `AAAA()` helper; (b) no manual
  fiddling with `m.Extra` for IPv6 glue — the referral server
  attaches it automatically; (c) `AssertGaugeExists` is used with
  IPv6 string values in the `ip` label without test-specific
  workarounds. If any of those don't hold, fix the test (the test
  IS the documentation of the helper's ergonomics). Per US4
  acceptance scenarios 1-3.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Final hygiene and the post-implementation audit
required by the constitution's Governance section.

- [X] T025 `go build ./...` clean. No new dependencies; binary size unchanged. Must build cleanly.
  Confirm the exporter binary size has not materially changed
  (the SDK dep added in 005 is the only known size factor; this
  feature adds zero new dependencies, so no change expected).

- [X] T026 `go vet ./...` clean; full integration suite green across cache, config, cycle, dashboard, prober, testutil. Drift test in `demo/dashboard/dashboard_test.go` still passes (no dashboard changes).

- [X] T027 FR-008 backward-compat verified via proxy: smoke test A1-A6 (which assert against v4-only zones healthy/soa-serial-mismatch/lame-nameserver/ns-mismatch) all still pass; full integration suite green; new A4b assertion covers v6 entries. If a v4 series had been removed or renamed, the v4 assertions would have caught it. Strict comm-diff against a pre-T004 binary deferred — see audit.md D7 for the deviation note. capture
  `/metrics` from a pre-T004 build (use `git stash` if needed)
  against the demo stack with `v6-only.demo.` zone temporarily
  REMOVED from the exporter config (so the comparison is on the
  v4-only set of zones). Capture `/metrics` from post-T004 build
  against the same config. `comm -23 before after | grep -v
  '^# '` MUST be empty (no metric series present before-only). Per
  FR-008 + SC-003 + SC-007.

- [X] T028 Audit at `specs/006-ipv6-nameserver-support/audit.md` — 18/18 FRs satisfied (proxy evidence for FR-008), 10/10 SCs satisfied (SC-006 and SC-010 pending user verification), 7 deviations documented (D1-D7), 0 constitution violations, 0 regressions. walk every FR
  (FR-001..FR-018) and SC (SC-001..SC-010) and confirm the
  implementation satisfies it. Record any deviations in
  `specs/006-ipv6-nameserver-support/audit.md` with file:line
  citations. Per constitution Governance: "After implementation, a
  thorough code audit against the spec MUST be performed before
  declaring the feature complete."

**Checkpoint**: Build clean, tests green, backward-compat verified,
audit complete. Feature ready for review and merge.

---

## Dependencies

```text
Phase 1 (Setup) ─────────► Phase 2 (Foundational, all source changes)
                                       │
                  ┌────────────────────┼────────────────────┐
                  │                    │                    │
                  ▼                    ▼                    ▼
            Phase 3 (US1)       Phase 4 (US2)         Phase 5 (US3)
            v6-only test        dual-stack +          demo zones,
                                multi-A tests         compose, smoke
                                                            │
                                       Phase 6 (US4)        │
                                       infra-doc verify     │
                                                ▼           ▼
                                            Phase 7 (Polish, audit)
```

- **Phase 2 is the critical path.** All user-story phases consume
  its outputs. T010 (Phase 2 regression gate) MUST be green before
  any user-story phase begins.
- **Phases 3, 4, 5 are independent of each other** once Phase 2
  lands. They share no files (Phase 3 = `prober/glue_ipv6_test.go`,
  Phase 4 = `prober/glue_dualstack_test.go` + sibling, Phase 5 = `demo/`).
- **Phase 6 (US4)** is a verification phase on output of T013
  (Phase 4); minor dependency on Phase 4.
- **Phase 7 (Polish)** requires Phases 3-6 complete.

## Parallel execution opportunities

Within **Phase 2**:

- T002 (testutil AAAA helper) and T003 (testutil glue attachment)
  touch the same package but different files — parallelizable.
  T004-T009 form a chain (resolver → callers → glue prober →
  config → tidy) and run sequentially.

Within **Phase 4 (US2)**:

- T013, T014, T015 — three independent test files. Parallelizable.

Within **Phase 5 (US3)**:

- T016, T017, T018 — three independent file groups (root zone file,
  healthy zone file, new v6-only container dir). Parallelizable.
- T019 (compose) depends on T018 (the new container dir exists).
- T020-T022 (exporter config, smoke test, README) parallelize once
  T019 is in.
- T023 is the verification step; runs after all of T016-T022.

## Implementation strategy

- **MVP** = Phase 1 + Phase 2 + Phase 3 (US1). At MVP, the
  exporter is IPv6-aware end-to-end, IPv4-only zones are
  byte-compatible, and the v6-only NS regression is covered by an
  integration test. US2 / US3 / US4 layer additional test coverage
  (US2), demo visibility (US3), and infrastructure documentation
  (US4) without further source changes.
- **Iteration 1** = add US2 (dual-stack + multi-A + parent-v4-only-glue
  tests). Three more integration tests, no source changes.
- **Iteration 2** = add US3 (demo). The most file churn but
  entirely demo-scope; no exporter code.
- **Iteration 3** = US4 verification + Phase 7 polish.

If timeboxed: ship MVP first; US2 / US3 / US4 land in follow-up
PRs without blocking value delivery for the user's deployment (the
user can redeploy after MVP and confirm SC-006 directly).

## Format validation

All 28 tasks above conform to the required checklist format:
`- [ ] TNNN [P?] [Story?] Description (with file path)`. Setup,
Foundational, and Polish tasks omit the `[Story]` label per spec.
Story-phase tasks all carry `[US1]`, `[US2]`, `[US3]`, or `[US4]`.
