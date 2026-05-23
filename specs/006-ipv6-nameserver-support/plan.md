# Implementation Plan: 006-ipv6-nameserver-support

**Branch**: `006-ipv6-nameserver-support` | **Date**: 2026-05-22 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/006-ipv6-nameserver-support/spec.md`

## Summary

Close the IPv6 gap documented in [issue #23](https://github.com/SJrX/dnshealth_exporter/issues/23): the
exporter's resolver queries `TypeA` only and returns a single
string, the `prober.Nameserver` data model carries one IP per
hostname, and the glue prober's self-side loop has the same
single-family limitation. Result: nameservers with only AAAA records
are silently dropped; nameservers with multiple A records or a mix
of A+AAAA report only their first A.

The fix is mechanical but cuts across the resolver
(`prober/prober.go`), the cycle runner (`cycle/runner.go`), the
glue prober's self-side loop (`prober/glue.go`), the test fixture
(`testutil/fixture.go`, `testutil/records.go`), and the demo
deployment (`demo/coredns/root/zones/demo.zone`,
`demo/exporter/dnshealth.yml`).

**Technical approach** (from research):

- Rename `ResolveHostname` → `ResolveHostnames`, plural return
  `([]string, error)`. Queries both A and AAAA sequentially. NODATA
  on either family is silent (not a failure); a partial success
  (one family OK, other failed) returns the successful family.
- Callers (`cycle/runner.go`, `testutil/fixture.go`) flatten the
  returned slice into one `prober.Nameserver` entry per IP. No
  struct change — same `{Hostname, IP string}` shape, just emitted
  N times per hostname instead of once.
- Glue prober's `querySelfForNSAndA` issues both A and AAAA for
  each self-reported NS hostname, emits one `source="self"` series
  per (hostname, IP) tuple.
- Address-override map (`config.AddressOverrides`) gains canonical
  IP normalisation on both load and lookup (via
  `net.ParseIP(s).String()`) so operators can write v6 keys in any
  legal textual form. Unparseable keys rejected at config-load.
- Test fixture (`testutil`) gains `AAAA(name, ipv6)` mirroring the
  existing `A()`; referral handler attaches AAAA records as glue.
- Demo gets two IPv6 patterns, deliberately distinguishable:
  (a) AAAA records added to `healthy.demo.`'s existing NSes
  (dual-stack), and (b) a new `v6-only.demo.` zone whose NSes
  have only AAAA glue (the original #23 failure mode, visibly
  fixed). Demo exporter config maps all new v6 addresses through
  the existing override mechanism to in-container IPv4 endpoints.

## Technical Context

**Language/Version**: Go 1.26.2 (matches `go.mod`).

**Primary Dependencies**: None added. Existing
`github.com/miekg/dns` already handles AAAA queries and IPv6
`host:port` strings; existing `net` (stdlib) provides
`ParseIP` and `JoinHostPort`.

**Storage**: N/A.

**Testing**: `go test -tags=integration ./...` — new tests added
to `prober/glue_ipv6_test.go`, `prober/glue_dualstack_test.go`,
`prober/glue_parent_v4_only_glue_test.go`, plus a `testutil`
regression test for the new AAAA helper and glue attachment.

**Target Platform**: Linux/macOS dev machines (existing).
Production runs anywhere the Go binary runs (existing). The
exporter does not require IPv6 host connectivity to function on
IPv4-only zones; IPv6-bearing zones require either IPv6
connectivity or address overrides per FR-014/015.

**Project Type**: Same as today — long-running Prometheus exporter,
single Go module, no project-type change.

**Performance Goals**:
- Worst-case extra resolution latency per zone per cycle: ~100ms
  (one extra AAAA query per NS hostname when glue is missing on
  both families). Tolerated within existing probe-cycle SLA.
- No additional concurrent queries — A and AAAA serially per
  hostname.

**Constraints**:
- FR-008: byte-identical metric series for IPv4-only zones (no
  schema change, no label drift).
- FR-009: v6 probe failures MUST surface as failure series, not
  silent absence.
- FR-014: address-override MUST preserve the original IP in metric
  labels (override redirects network, not data).
- Test infrastructure additions MUST follow the existing testutil
  defaults-with-override pattern (constitution Principle VIII).

**Scale/Scope**:
- ~300-500 LoC change across the exporter source tree (estimate
  from research).
- 4-5 new integration tests + 1 testutil unit test.
- Demo gains: 1 new CoreDNS container + 1 new zone file +
  1 new Corefile + edits to root zone file + existing healthy
  zone file + exporter config + smoke test. ~80-150 lines of
  YAML / zone-file content.
- 0 new public types, 1 rename (`ResolveHostname` →
  `ResolveHostnames`).

## Constitution Check

Evaluated against `.specify/memory/constitution.md` v1.1.1.

| Principle | Relevance | Status |
|-----------|-----------|--------|
| I. Robust Integration Testing | Three new integration tests cover v6-only, dual-stack, and parent-v4-glue-only paths (each was silently broken pre-fix). Existing `testutil` fixtures extended. | PASS |
| II. Prometheus Naming Conventions | No new metric names; no new labels on existing series; existing `ip` label widens its value set to include v6 textual addresses. FR-007 / FR-008 lock this in. | PASS |
| III. Modern Go Ecosystem | Go 1.26.2; no new deps; `net.ParseIP` / `net.JoinHostPort` / `miekg/dns` all already in use. | PASS |
| IV. Structured Logging | New WARN-level log lines for AAAA-only and resolution-partial-failure cases. Same `promslog` discipline as existing code. | PASS |
| V. Zone-Focused Detection Scope | Same scope; no new check types in this feature. Just makes existing checks family-aware. | PASS |
| VI. Prometheus Ecosystem Conventions | Same. Address-override mechanism gains v6 key support — schema-compatible with existing v4 entries. | PASS |
| VII. Well-Behaved Binary | No signal-handling changes, no startup changes other than config-load validation (which is failure-fast, matching the existing pattern). | PASS |
| VIII. Readable, Honest Tests | Three-phase Meszaros structure for every new test. `testutil/records.go` AAAA helper mirrors A helper exactly. No mocks; real `miekg/dns` servers. | PASS |

**Gate verdict**: PASS. No principle violations; Complexity Tracking
section empty.

**Post-design re-check** (after Phase 1): PASS. The data-model and
contracts artifacts introduce no new public types, no new metrics,
no new dependencies, no new external interfaces beyond the widened
acceptance of the existing `address_overrides` YAML schema. The
constitution stays satisfied.

## Project Structure

### Documentation (this feature)

```text
specs/006-ipv6-nameserver-support/
├── plan.md                          # This file
├── research.md                      # Phase 0: 8 decisions (R-1..R-8)
├── data-model.md                    # Phase 1: 5 runtime entities
├── quickstart.md                    # Phase 1: maintainer/operator ops
├── contracts/
│   ├── address-override.md          # YAML schema + normalisation contract
│   └── nameserver-fanout.md         # Resolver→prober internal contract
├── checklists/
│   └── requirements.md              # Spec quality checklist
└── tasks.md                         # Phase 2 (NOT created here — `/speckit.tasks`)
```

### Source Code (repository root)

```text
.
├── go.mod                           # unchanged
├── go.sum                           # unchanged
├── main.go                          # unchanged
├── prober/
│   ├── prober.go                    # ResolveHostname → ResolveHostnames
│   ├── glue.go                      # querySelfForNSAndA: A + AAAA
│   ├── glue_ipv6_test.go            # NEW: v6-only NS regression test
│   ├── glue_dualstack_test.go       # NEW: A+AAAA same hostname test
│   └── glue_parent_v4_only_glue_test.go  # NEW: out-of-band AAAA test
├── cycle/
│   └── runner.go                    # ResolveHostname → ResolveHostnames + fan-out
├── config/
│   └── config.go                    # ResolveAddress: normalise lookup key
├── testutil/
│   ├── fixture.go                   # ReferralServer: attach AAAA glue
│   ├── records.go                   # NEW helper: AAAA(name, ipv6)
│   └── records_test.go              # NEW unit test for AAAA helper
└── demo/
    ├── docker-compose.yml           # NEW: coredns-v6-only service
    ├── coredns/
    │   ├── root/zones/demo.zone     # NEW: AAAA glue for healthy.demo NSes + delegation for v6-only.demo
    │   ├── healthy/zones/healthy.demo.zone  # NEW: AAAA records for healthy NSes
    │   └── v6-only/                 # NEW: dedicated CoreDNS for v6-only.demo
    │       ├── Corefile
    │       └── zones/v6-only.demo.zone
    ├── exporter/dnshealth.yml       # NEW: address_overrides for AAAA addresses; v6-only.demo added to zones
    └── smoke.sh                     # NEW: assert v6-only.demo produces per-NS series with v6 IPs
```

**Structure Decision**: Single-module layout (unchanged). All
changes live in the existing package layout. No new packages
introduced; no new directories created (the
`specs/006-ipv6-nameserver-support/` tree is the only new directory).

## Complexity Tracking

> No constitution violations to justify. Section intentionally empty.
