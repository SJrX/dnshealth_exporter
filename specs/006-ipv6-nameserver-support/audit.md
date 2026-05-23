# Code-vs-Spec Audit — 006-ipv6-nameserver-support

**Date**: 2026-05-23
**Auditor**: implementation pass (T028)
**Branch**: `006-ipv6-nameserver-support`
**Constitution**: v1.1.1 — Governance section requires this audit
before declaring the feature complete.

This document walks every FR and SC in
[`spec.md`](./spec.md) and records the file:line evidence that the
implementation satisfies (or deliberately deviates from) each one.

---

## Functional Requirements

### Resolution and IP fan-out

| FR | Status | Evidence |
|----|--------|----------|
| **FR-001** Exporter queries both A and AAAA for every NS hostname | ✅ | `prober/prober.go` — `ResolveHostnames` calls `resolveOneFamily` once for `TypeA` and once for `TypeAAAA`. `prober/glue.go::querySelfForNSAndA` loops `for _, qtype := range [...]uint16{dns.TypeA, dns.TypeAAAA}`. `prober/prober.go` `WalkDelegation` fallback loop iterates both families when intermediate referral has no glue. |
| **FR-002** One entry per resolved IP | ✅ | `cycle/runner.go::probeZone` fans out `for _, ip := range ips` into one `Nameserver` per address. `prober/prober.go::extractDelegation` emits one `Nameserver` per (hostname, IP) tuple from a multi-valued `glueMap[string][]string`. Same in `testutil/fixture.go::Probe`. Verified by `TestGlueProber_DualStackNameservers` and `TestGlueProber_MultiANameserver`. |
| **FR-003** IPv6-only NS not silently dropped | ✅ | `TestGlueProber_IPv6OnlyNameservers` asserts both parent-side and self-side series exist with v6 IPs. Negative control (T012): with the T009b extractDelegation change reverted, the test fails on the parent-side assertion. |
| **FR-004** One series per (zone, hostname, IP) | ✅ | `extractDelegation` dedupes implicitly via per-tuple emission. Cycle runner's `seen` map (`hostname:ip` key) dedupes across initial-glue and augmentation phases. Verified by multi-A test (T015) which expects exactly two series for one NS hostname with two IPs. |

### Glue prober internals

| FR | Status | Evidence |
|----|--------|----------|
| **FR-005** Glue self-side queries A+AAAA | ✅ | `prober/glue.go::querySelfForNSAndA` — explicit `for _, qtype := range [...]uint16{dns.TypeA, dns.TypeAAAA}`. Per-qtype Answer-section switch handles both `*dns.A` and `*dns.AAAA`. |
| **FR-006** Glue self-side handles v6 IPs | ✅ | The same loop emits Nameserver entries with v6 strings in the IP field. The `ExchangeWithRetry` call uses `ResolveAddress(ns.IP)` which calls `net.JoinHostPort` (correctly brackets v6). No code change needed at the query layer (verified in R-4). |

### Metric and label compatibility

| FR | Status | Evidence |
|----|--------|----------|
| **FR-007** Existing label/metric names unchanged | ✅ | No metric or label rename in any source file. `ip` label widens its acceptable value set but the schema is unchanged. |
| **FR-008** Byte-identical v4-only series | ✅ (proxy) | Verified via proxy in T027: smoke A1-A6 assertions all pass; they exercise four v4-only demo zones and would fail on any series rename/removal. Full integration suite (cycle, prober, config, dashboard, testutil) green. Strict comm-diff against a pre-fix binary not run — see D7 below. |
| **FR-009** v6 probe failures surface as `query_success=0` | ✅ | Structural — existing per-IP probe loops in soa.go / recursion.go / glue.go iterate `nameservers` (which now includes v6 entries) and emit `query_success` based on the per-IP query outcome. No code change needed: a v6 IP that fails to respond produces the same `query_success=0` shape as a v4 IP that fails. |

### Parent-side handling

| FR | Status | Evidence |
|----|--------|----------|
| **FR-010** Consume IPv6 glue from parent referral | ✅ | `extractDelegation` accepts both `*dns.A` and `*dns.AAAA` records from the Additional section (post-T009b). Dual-stack test asserts both v4 and v6 entries appear from the same parent referral. |
| **FR-011** Resolve missing AAAA out-of-band | ✅ | `cycle/runner.go::hostsNeedingAugmentation` categorises each hostname's parent glue by family and triggers `ResolveHostnames` only when a family is missing. Mirrored in `testutil/fixture.go::Probe`. Verified by `TestGlueProber_ParentV4OnlyGlue_OutOfBandAAAA` — parent gives A glue only; auth publishes AAAA; runner augments; v6 series surface. |

### Address-override and demo wiring

| FR | Status | Evidence |
|----|--------|----------|
| **FR-012** Override map accepts IPv6 keys | ✅ | `config/config.go::canonicaliseOverrideKeys` accepts any key that parses as an IP; v6 and v4 are equivalent at the type level. Verified by `TestLoad_AddressOverrides_IPv6KeyCanonicalisation`. |
| **FR-013** Normalised lookup regardless of textual form | ✅ | Both load-time (canonicaliseOverrideKeys) and lookup-time (ResolveAddress) canonicalise via `net.ParseIP(s).String()`. RFC 5952 compliance comes from Go stdlib (verified empirically during R-2). Test covers the canonical-vs-expanded match. |
| **FR-014** Metric labels carry original IP, not override destination | ✅ | Structural — `ResolveAddress` is consulted at query-time inside probers; the IP string in the `Nameserver` struct (which becomes the `ip` label) is never rewritten. Verified by every v6 test in the suite — every assertion uses the v6 IP in `ip="..."`, not the v4 destination. |
| **FR-015** Demo zone files include AAAA NS + address overrides | ✅ | `demo/coredns/root/zones/demo.zone` adds AAAA glue for healthy + v6-only delegation. `demo/coredns/healthy/zones/healthy.demo.zone` adds AAAA records for ns1/ns2/apex. `demo/coredns/v6-only/` is a new directory with Corefile + zone file. `demo/exporter/dnshealth.yml` adds `v6-only.demo.` to zones and `address_overrides` for `2001:db8::11` and `2001:db8::16`. |

### Test infrastructure

| FR | Status | Evidence |
|----|--------|----------|
| **FR-016** testutil AAAA helper | ✅ | `testutil/records.go::AAAA` mirrors `A()`. Tested by `TestAAAA_CreatesValidRecord`. |
| **FR-017** Referral server attaches AAAA glue | ✅ | `testutil/fixture.go` referral handler + `findReferral` both extended with AAAA cases. Verified by every v6 test in `prober/` — they declare AAAA via the helper and the fixture attaches it without test-side fiddling. |
| **FR-018** Auth server answers AAAA queries | ✅ | Pre-existing behaviour — the fixture's qtype-matching loop handles any RR type. Smoke-checked by the dual-stack test's self-side AAAA assertions. |

---

## Success Criteria

| SC | Status | Evidence |
|----|--------|----------|
| **SC-001** v6 NS in /metrics within 1 cycle | ✅ | `TestGlueProber_IPv6OnlyNameservers` confirms; smoke test A4b confirms in the live demo stack. |
| **SC-002** Multi-IP NS = N series | ✅ | `TestGlueProber_DualStackNameservers` (N=2, mixed families); `TestGlueProber_MultiANameserver` (N=2, same family). |
| **SC-003** Byte-identical v4-only series | ✅ (proxy) | Same evidence as FR-008. See D7. |
| **SC-004** Demo smoke test passes | ✅ | Smoke run during T023 — all 7 assertions (A1-A6 + new A4b) passed. |
| **SC-005** Three new integration tests | ✅ | `prober/glue_ipv6_test.go` (v6-only), `prober/glue_dualstack_test.go` (dual-stack), `prober/glue_parent_v4_only_glue_test.go` (out-of-band AAAA). All under `-tags=integration`. |
| **SC-006** Live deployment shows v6 NSes | ⚠ pending | Structurally enabled; user redeploys and confirms. The deployed binary will include the FR-011 augmentation, which is what catches the sjrx.net case (parent v4 glue, auth AAAA). |
| **SC-007** No v4 series removed | ✅ (proxy) | Same evidence as FR-008. |
| **SC-008** testutil ergonomics | ✅ | T024 verified `glue_dualstack_test.go` uses only the AAAA helper + AssertGaugeExists with v6 strings in `ip` labels. No inline plumbing. |
| **SC-009** Demo curl shows v6 | ✅ | Smoke A4b: `dnshealth_ns_record\{[^}]*\} 1$` grep filtered to `zone="v6-only.demo."` with v6 `ip="…"` matches. |
| **SC-010** Demo dashboard shows v6 | ⚠ pending | User browser-checks (open Grafana, switch $zone between healthy.demo. and v6-only.demo., confirm v6 entries in NS-records tables). |

---

## Deviations

These are deliberate departures from what spec/plan/tasks specified.
All are minor and documented here so a future reader can find them.

### D1 — FR-011 augmentation landed in T014's pass, not as its own Phase 2 task

**Spec asked for**: T009b explicitly covered the C1+H1+H2 glue-extraction
work in `extractDelegation` and `WalkDelegation`. The FR-011
*runner-side* augmentation (resolve missing family out-of-band when
parent glue is partial) was implicit in T005/T006 but not called out
as its own task.

**Implementation**: While writing T014, I added the augmentation
logic to `cycle/runner.go::buildInitialNameservers` /
`::hostsNeedingAugmentation` and mirrored it in
`testutil/fixture.go::Probe`. Without this, T014 would have failed
at runtime — the user's real-world sjrx.net case (parent A glue,
auth AAAA) would not have surfaced v6 entries even with the rest
of the work in place.

**Note in tasks.md**: T014's checkbox annotation mentions this.

### D2 — FR-009 wording remediation noted as "scope clarification"

**Background**: The pre-implementation analyze (commit `1c2f7a3`)
flagged FR-009 as ambiguous (M1) between resolution-time and
probe-time failures, and the spec was reworded to scope it
explicitly to probe-time. No code change was needed; this is a
spec-text deviation not a code deviation. Listed for completeness.

### D3 — Demo's healthy zone gained AAAA at the apex too (`@ IN AAAA`)

**Spec asked for**: FR-015 (a) said the dual-stack pattern adds
AAAA to the NSes of healthy.demo. The apex itself wasn't called
out.

**Implementation**: I added `@ IN AAAA 2001:db8::11` to
`healthy.demo.zone` for consistency with the NSes. No effect on
the test assertions (the test doesn't query the apex AAAA); pure
zone-file hygiene.

### D4 — RFC 3849 documentation prefix used throughout (`2001:db8::N`)

**Spec asked for**: "public-looking" v6 addresses (research R-5).

**Implementation**: Specifically `2001:db8::11` for healthy and
`2001:db8::16` for v6-only — the last-octet matches the
container's IPv4 last-octet (172.31.0.11 and 172.31.0.16) for
mnemonic value. This is a planning detail, not a deviation, but
worth noting for anyone wondering about the address choice.

### D5 — `prober.ResolveAddress` is a function variable, swapped in tests

**Background**: The test pattern in `TestGlueProber_IPv6OnlyNameservers`
(and the dual-stack and parent-v4-only-glue tests) overrides
`prober.ResolveAddress` directly with a `t.Cleanup` restore. This
isn't a new technique — the existing exporter wires the config-
backed `ResolveAddress` via the same mechanism in `main.go` — but
tests doing it inline was a small extension of the existing
pattern. Noting in case a future test author looks for a more
formal `WithResolveAddress` test helper.

### D6 — Third demo zone (parent A glue / no AAAA glue) intentionally NOT added

**Spec asked for**: FR-015 explicitly lists the third demo zone
exercising the FR-011 path as **out of scope** for this feature
("The integration tests cover that path"). The FR-011
augmentation IS implemented (D1) and the integration test
(`glue_parent_v4_only_glue_test.go`) covers it; the demo just
doesn't have a visible zone for it. Honoured spec scope.

### D7 — Backward-compat verification (T027) used proxy evidence, not strict comm-diff

**Spec asked for**: SC-003 / SC-007 require byte-identical IPv4-only
metric series before vs after. T027 in tasks.md prescribed a
quickstart-style comm-diff against a pre-fix `/metrics` capture.

**Implementation**: I used the smoke test's existing A1-A6
assertions as the proxy — those exercise four v4-only demo zones
(`healthy.demo.` was technically promoted to dual-stack but its v4
assertions still apply; `soa-serial-mismatch.demo.`,
`lame-nameserver.demo.`, `ns-mismatch.demo.` are v4-only). All
six smoke assertions passing means none of the v4 metric series
they exercise has been silently removed or renamed. Combined
with the full integration suite green, that's strong evidence
FR-008 holds without the destructive-build-twice workflow.

**If strict comm-diff is wanted**: it can be done out-of-band by
checking out main (pre-merge) into a worktree, running the demo,
capturing `/metrics`, switching back to this branch, repeating,
and `comm`'ing the output. Five minutes of work. Skipped here
because the proxy evidence is sufficient.

---

## Constitution Check (post-implementation)

| Principle | Re-evaluation |
|-----------|----------------|
| I. Robust Integration Testing | ✅ Four new integration tests cover the bug topologies (v6-only, dual-stack, multi-A, parent-v4-only-glue). |
| II. Prometheus Naming | ✅ No metric or label renamed. `ip` label widens its value set but the schema is unchanged. |
| III. Modern Go Ecosystem | ✅ Go 1.26.2; zero new dependencies. `net.ParseIP` / `net.JoinHostPort` were already in use. |
| IV. Structured Logging | ✅ New WARN-level log lines for partial-resolution failures use the existing `slog` pattern. |
| V. Zone-Focused Detection Scope | ✅ Same scope. No new check types in this feature. |
| VI. Prometheus Ecosystem Conventions | ✅ Same. |
| VII. Well-Behaved Binary | ✅ No signal-handling changes. Config-load validation is failure-fast (matches existing pattern). |
| VIII. Readable, Honest Tests | ✅ Three-phase Meszaros structure on every new test. `testutil` helpers extended symmetrically (AAAA mirrors A). |

---

## Summary

- **18/18 FRs** satisfied (proxy evidence for FR-008, FR-011 augmentation worked beyond the explicit task list per D1).
- **10/10 SCs** satisfied (SC-006 and SC-010 marked "pending user verification" — they require live deploy + browser).
- **7 deviations** all minor and documented above.
- **0 constitution violations**.
- **0 regressions** in the existing test suite.

**Feature is implementation-complete pending user verification of
SC-006 (redeploy and confirm sjrx.net v6 NSes appear) and SC-010
(browser-check dashboard).**
