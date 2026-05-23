# Research: IPv6 and multi-IP nameserver support

**Date**: 2026-05-22
**Feature**: 006-ipv6-nameserver-support
**Status**: Complete

The fix shape is well-scoped by issue #23 and the spec — no large
external dependencies, no library decisions, no protocol questions.
Phase 0 research is therefore short, focused on the few decisions
that need to be settled before code.

---

## R-1: A/AAAA query strategy — sequential vs parallel

**Decision**: Sequential per hostname (A first, then AAAA). Run probes
across hostnames in the existing per-zone order; do not introduce
goroutine fan-out at the resolution layer.

**Rationale**:
- The existing `ResolveHostname` already issues delegation-walk queries
  serially against `RootServers[0]`. Adding parallelism at the
  resolution layer would be inconsistent with the rest of the code.
- For a typical zone with 2-6 NSes, sequential adds at most one extra
  RTT per missing IP family (~30-100ms). Probe cycles already
  tolerate seconds of latency.
- Parallel resolution would multiply the load on the upstream root
  servers per cycle by 2x. Not a concern at our scale but bad form.
- Worth revisiting if/when zone counts grow into the hundreds —
  but that's a separate performance ticket.

**Alternatives considered**: Concurrent A+AAAA via goroutines per
hostname (would halve resolution latency for hostnames without glue,
at the cost of doubling concurrent root-server queries). Rejected as
premature optimization.

---

## R-2: IP string normalization for override map lookup

**Decision**: Use `net.ParseIP(s).String()` for canonicalization on
**both** sides — at config-load time (when populating
`AddressOverrides`) **and** at lookup time (when the prober asks
`ResolveAddress` for an IP). Reject unparseable IPs at config-load
with a clear error.

**Canonical form (RFC 5952)**:
- Drop leading zeros in each 16-bit group (`0db8` → `db8`)
- Lowercase hex digits (`2001:DB8::1` → `2001:db8::1`)
- Compress the **longest** run of consecutive all-zero groups
  with `::`
- When two or more runs tie for longest, compress the **first**
- Do NOT compress a single zero group (`2001:db8:0:1:...` stays
  as-is; `::` is for runs of two or more — §4.2.2)
- IPv4-mapped form `::ffff:a.b.c.d` is the dotted-quad embedded
  in the standard mapped prefix

**Rationale**:
- Go's `net.ParseIP(s).String()` is fully RFC 5952 compliant in
  the pinned Go 1.26.x. Verified empirically on 10 cases including
  the two edge rules (first-of-ties, single-zero-not-compressed)
  during planning. Example outputs:
  - `2001:0db8:0000:0000:0000:0000:0000:0001` → `2001:db8::1`
  - `2001:db8:0:0:1:0:0:1` → `2001:db8::1:0:0:1`  *(first run wins)*
  - `2001:db8:0:1:1:1:1:1` → `2001:db8:0:1:1:1:1:1`  *(single zero stays)*
  - `2001:0:0:0:1:0:0:1` → `2001::1:0:0:1`  *(longest of two runs wins)*
- Canonicalizing on **both** sides means the map keys and the
  prober's lookup keys agree regardless of which textual form was
  used in either place. This is FR-013.
- Doing it once at config-load (for keys) avoids per-lookup parse
  overhead. The lookup side still normalises explicitly because
  the upstream source (`miekg/dns` types) already happens to
  produce canonical strings, but relying on that implicitly would
  be a hidden coupling — better to be explicit and cheap.

**Alternatives considered**:
- Roll our own RFC 5952 implementation — pointless, the stdlib
  is correct and well-tested. Rejected.
- String-based normalization without parsing (lowercasing,
  stripping leading zeros by hand) — fragile, error-prone for v6
  edge cases like `::` vs `0:0:0:0:0:0:0:0`, and gets the
  longest-run / single-zero rules wrong on naive implementations.
  Rejected.
- Only normalise on one side — leaves a footgun where an operator
  writes a non-canonical key and silently gets no match. Rejected.
- Use a third-party RFC 5952 library — no need; stdlib handles it.
  Rejected.

---

## R-3: AAAA NODATA vs AAAA failure semantics

**Decision**: A `NODATA` response (RCODE=NOERROR, empty Answer) is
**not** a failure — the hostname legitimately has no AAAA, the most
common case for v4-only zones. The exporter emits no series for the
v6 family of that hostname. A timeout / SERVFAIL / connection
refused IS a failure and produces `dnshealth_query_success{check=...,
ip="<the v6 IP we tried to use>"} = 0`.

**Rationale**: Most zones in the wild have A but no AAAA. Treating
AAAA absence as a failure would generate noise on the vast majority
of monitored zones — directly inverting the spec's intent. The
distinction matters operationally: `NODATA` is information about the
zone's DNS configuration; timeout is information about the network
path or the NS being unreachable over v6.

**Test coverage**: integration tests cover both branches —
`testutil` already supports defining a zone with only A records (no
AAAA → NODATA); a separate test fixture covers the failure branch by
configuring an unreachable v6 IP.

**Alternatives considered**: Emit a `dnshealth_aaaa_present{...}`
gauge per hostname (1 if AAAA exists, 0 if NODATA). Rejected as out
of scope — useful, but a separate ticket about "AAAA presence as a
health signal" rather than this ticket's "make existing checks
IPv6-aware" framing.

---

## R-4: miekg/dns IPv6 query handling

**Decision**: No special handling needed at the query layer.
`miekg/dns` accepts a `host:port` string for the server address;
`net.JoinHostPort("2001:db8::1", "53")` returns the correctly-bracketed
form `[2001:db8::1]:53` which `miekg/dns` parses correctly. The
existing `prober.ResolveAddress` function variable already uses
`net.JoinHostPort` as its default (so IPv6 outbound is already
handled — the missing piece is just that `ResolveHostname` never
returns v6 in the first place).

**Verified**: Read of `prober/prober.go:36-38` confirms current
ResolveAddress default is `net.JoinHostPort(ip, "53")`. Read of
`miekg/dns` documentation confirms `Exchange(msg, "[v6]:port")` is
the expected form.

**Implication**: No changes needed in `ResolveAddress`, only in
the resolution path that *feeds* it.

---

## R-5: Demo Docker network — IPv6 enablement not required

**Decision**: Keep the demo Docker bridge IPv4-only. Add AAAA
records to the demo's zone files using "public-looking" IPv6
addresses (e.g., `2001:db8::1` per RFC 3849 documentation prefix).
Add corresponding entries in `demo/exporter/dnshealth.yml`'s
`address_overrides` map keyed by those v6 addresses, mapping to the
existing in-container IPv4 endpoints (e.g., `coredns-healthy:53`).

**Rationale**:
- Docker bridge IPv6 enablement requires `enable_ipv6: true` on the
  network definition, a manually-allocated v6 subnet, and per-OS
  Docker daemon configuration (the daemon needs `ipv6: true` set
  in `/etc/docker/daemon.json` on Linux). Each step is a
  per-environment friction point.
- The exporter's data model (post-fix) and metric labels carry the
  v6 addresses regardless of whether the wire is v6 — the override
  mechanism (FR-014) preserves the original v6 IP in `ip="..."`.
  So the demo gives operators visible v6 entries on the dashboard
  even though no v6 packets transit the Compose network.
- RFC 3849 reserves `2001:db8::/32` for documentation use; no
  collision risk with real internet allocations.

**Alternatives considered**:
- Full IPv6 dual-stack Docker bridge — rejected as scope creep with
  high friction. Filed mentally as a follow-up if anyone needs to
  test actual wire-level v6 behaviour against the demo.
- Skip demo updates entirely; cover IPv6 only via integration tests
  — rejected because the user explicitly asked for visible demo
  verification (US3, SC-009, SC-010).

---

## R-6: `ResolveHostname` → rename or keep signature?

**Decision**: Rename to `ResolveHostnames` (plural), change return
type from `(string, error)` to `([]string, error)`. Empty slice +
nil error is the "no addresses found" case; non-nil error is the
"resolution failed at the protocol level" case (callers can
distinguish "no AAAA exists" from "AAAA query failed" via the
error).

**Rationale**: Single-module project, no external consumers — clean
rename has zero compatibility cost and matches the new semantics.
Keeping the old name would mislead readers about the return type.

**Internal callers to update** (verified via grep):
- `cycle/runner.go:180` — single caller for production fan-out
- `testutil/fixture.go:145` — single caller for test fan-out
- (No other consumers in the tree.)

**Alternatives considered**: Add a parallel `ResolveHostnames` and
deprecate `ResolveHostname`. Rejected; just rename.

---

## R-7: testutil glue attachment — symmetric A and AAAA

**Decision**: Extend `testutil/fixture.go`'s referral-mode handler
(currently at lines 212-226) to attach both A and AAAA records as
glue when the NS hostname matches. Same logic, just additionally
match `*mdns.AAAA` records.

**Rationale**: One-line addition mirroring the existing A-attachment
loop. The test fixture's existing query-handler in non-referral mode
already serves AAAA records (because matching is by `qtype ==
rr.Header().Rrtype` — line 207 — which works for any RR type the
test declares). The only gap is glue attachment on referrals.

**Verified**: Re-read of fixture.go confirms the gap is exactly one
loop in the `referral && qtype == TypeNS` branch.

---

## R-8: Where do AAAA records live in the demo

**Decision**: Two IPv6 patterns, two demo placements:

1. **Dual-stack pattern**: Add AAAA records alongside the existing
   A records for the NSes of `healthy.demo.` in the existing
   `demo/coredns/root/zones/demo.zone` and in each healthy auth
   server's own zone file. No new CoreDNS container — just
   additional records in existing zones.

2. **IPv6-only pattern**: Add a new demo zone `v6-only.demo.`
   served by a new CoreDNS container, where every NS has only
   AAAA records (no A). New entries in `docker-compose.yml`,
   new zone files, new delegation entry in the root zone file.

**Rationale**:
- CoreDNS's `file` plugin reads standard zone-file syntax for
  both A and AAAA, no config changes.
- Putting dual-stack on `healthy.demo.` means the dashboard's
  default `$zone=healthy.demo.` view shows the common-case
  pattern with no user action — best demo UX for the typical
  modern zone.
- Putting v6-only into its own dedicated zone (rather than
  mixing v6-only NSes into healthy) keeps each pattern
  visually distinct on the dashboard. An evaluator switches
  `$zone` to see each pattern cleanly rather than parsing a
  heterogeneous table.

**Implication for `docker-compose.yml`**:
- One new CoreDNS service (e.g., `coredns-v6-only`) on a new
  static IPv4 (consistent with the existing
  `172.31.0.0/24` plan).
- The new service mounts a new zone file with the
  `v6-only.demo.` SOA/NS/A records (the NSes still need A
  records on the CoreDNS service itself so the demo containers
  can reach it; the v6-only-ness lives in the *delegation* — the
  root zone delegates with AAAA glue only).

**Implication for `demo/coredns/root/zones/demo.zone`**:
- Existing delegation for `healthy.demo.` gains AAAA glue lines
  for its NSes (in addition to the existing A glue).
- New delegation for `v6-only.demo.` with NS records pointing at
  hostnames whose only published address is AAAA glue (no A glue).
- All new AAAA addresses use RFC 3849 documentation prefix
  (`2001:db8::/32`).

**Implication for `demo/exporter/dnshealth.yml`**:
- New `address_overrides` entries mapping each AAAA glue address
  to the corresponding CoreDNS container's `host:port` (v4
  destination, since the Compose network is v4-only).
- `v6-only.demo.` added to the `zones:` list.

**Implication for the dashboard / smoke test**:
- Smoke test should add a small assertion that `v6-only.demo.`
  produces per-NS metric series with v6 IPs (concrete proof of
  the fix, integrated with existing smoke).
- Dashboard auto-discovers the new zone via the existing `$zone`
  templating variable; no panel edits required.

---

## Honest unknowns / risks remaining

- **Multiple A records for one hostname** (the "anycast" framing in
  the spec's US2): the spec acceptance scenarios cover this, but
  it's a less-tested code path. Confirming the existing
  `ResolveHostname` truly returns the first A only (not somehow
  iterating) requires the test — and the test exists in US2's
  acceptance criteria. Low risk; behaviour is well-defined.
- **Reload behaviour** for address overrides with v6 keys: the
  existing reload path (`applyReloadedConfig` in `main.go`)
  rebinds `prober.ResolveAddress` unconditionally on every reload.
  v6 keys should pass through without special handling. Will verify
  in implementation; integration test for reload-with-v6-override
  is a nice-to-have but not load-bearing — the existing reload tests
  cover the mechanism, only the inputs change.
- **Demo dashboard panel column widths**: adding v6 strings (~30
  chars) to the IP column may visually crowd the table. Worth a
  quick check during the demo verification step (SC-010); a column-
  width tweak is a trivial follow-up if needed.
