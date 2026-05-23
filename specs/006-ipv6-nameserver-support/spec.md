# Feature Specification: IPv6 and multi-IP nameserver support

**Feature Branch**: `006-ipv6-nameserver-support`
**Created**: 2026-05-22
**Status**: Draft
**Input**: User description: "fix IPv6 issues identified in GitHub Issue #23"

Issue #23 documents the symptom (an NS with only an IPv6 address is
invisible in metrics; an NS with both A and AAAA is reported under
its first A only; an NS with multiple A records is reported under
its first A only) and the proposed mechanical fix shape. This spec
focuses on the *behaviour change* and the *acceptance bar*, leaving
the implementation specifics to the plan.

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Operator sees IPv6-only nameservers in the metrics (Priority: P1)

An operator monitors a zone whose authoritative nameservers include
at least one host with only an AAAA record (no A). Today, that NS is
silently absent from every per-NS series the exporter emits. The
dashboard's NS records / SOA serial / recursion tables show fewer
rows than the zone actually has, and the operator has no signal that
anything's wrong — the v6-only NS just doesn't exist as far as the
exporter is concerned.

After this change, the operator opens the dashboard and the v6-only
NS appears, with its IPv6 address in the `ip` label, alongside the
IPv4 NSs. SOA serials are reported for it. Recursion-availability is
reported for it. NS-record-from-self is reported for it.

**Why this priority**: This is the original bug from #23 — silent
under-reporting. Until this is fixed, every other check the exporter
runs is wrong by omission for any zone that uses IPv6.

**Independent Test**: Configure the exporter against a zone that has
at least one IPv6-only NS. After one probe cycle, the exporter's
`/metrics` endpoint includes `dnshealth_ns_record{...,ip="<the v6
address>"}` and the per-check series (`query_success{check="soa"}`,
`query_success{check="recursion"}`, `ns_recursion_available`,
`soa_serial`) all include entries with the v6 address.

**Acceptance Scenarios**:

1. **Given** a zone with one IPv4-only NS and one IPv6-only NS,
   **When** the exporter completes a probe cycle, **Then**
   `dnshealth_ns_record` has entries for both NSs (one with the
   v4 address, one with the v6 address) and the per-check series
   each have two entries (one per NS).
2. **Given** an IPv6-only NS that the exporter cannot reach
   (broken v6 path on the exporter host), **When** the exporter
   probes, **Then** the NS appears as a *failure* in
   `query_success` (`= 0`), not as silent absence. The operator
   can distinguish "v6 broken on probe path" from "this NS doesn't
   exist."

---

### User Story 2 — Operator sees dual-stack and multi-address nameservers fully (Priority: P1)

An operator monitors a zone whose NSs have multiple addresses each —
either dual-stack (an A + an AAAA at the same hostname) or
multi-record-of-one-family (two A records, common with anycast). Today
the exporter takes the first A only; every other address at that
hostname is dropped.

After this change, every (NS hostname, IP) pair appears as its own
series in every per-NS metric. The dashboard's tables show one row
per NS-and-IP rather than one row per NS-hostname-only.

**Why this priority**: Same priority as US1 — same underlying gap
(single-string `Nameserver.IP` in the data model, single-result
`ResolveHostname`). Independently testable: a dual-stack NS or a
multi-A NS exhibits the bug even on a zone with no v6-only NSs.

**Independent Test**: Configure the exporter against a zone with
at least one dual-stack NS (A + AAAA at the same hostname). After
one probe cycle, the per-NS metric series include two entries for
that NS — one keyed by `ip=<v4>` and one by `ip=<v6>`. Same test
shape with a multi-A anycast NS produces multiple v4-only entries.

**Acceptance Scenarios**:

1. **Given** a zone with one dual-stack NS (A + AAAA), **When**
   the exporter probes, **Then** every per-NS series for that NS
   has two entries — one per IP — and the IP families are
   correctly distinguishable by the value in the `ip` label.
2. **Given** a zone with one NS whose hostname resolves to two A
   records (anycast), **When** the exporter probes, **Then**
   both addresses appear as distinct series in every per-NS
   metric.
3. **Given** a zone whose parent referral carries IPv4 glue but no
   IPv6 glue (common in real-world delegations — the parent isn't
   authoritative for the IPv6 side), **When** the exporter probes,
   **Then** the exporter resolves the AAAA records out-of-band
   (same way it already resolves A out-of-band when v4 glue is
   missing) and the v6 entries appear in the metrics.

---

### User Story 3 — Operator runs the IPv6-capable demo on an IPv4-only host (Priority: P2)

A maintainer or evaluator wants to bring the demo up locally and
*see* IPv6 nameservers in the dashboard — to validate the work
visually, to demo it to a colleague, to iterate on dashboard panels
that show v6 entries — but their host has IPv6 disabled (common on
laptops behind certain corporate networks, or behind ISPs that
haven't rolled v6, or just by personal preference). The demo's
Docker Compose stack stays IPv4-only (avoiding the complexity of
Docker bridge IPv6 enablement), so any v6 NS entries in demo zone
files would normally be unreachable.

After this change, the existing address-override mechanism
(`address_overrides` in the exporter config, today used to map
delegation IPs to the demo's in-container endpoints) accepts IPv6
addresses as map keys. The demo's zone files declare some NSs with
AAAA records pointing at v6 addresses; the demo's exporter config
maps those v6 addresses to the same in-container IPv4 endpoints the
v4 NSes already use. The exporter sees v6 IPs in its data model
throughout, runs all v6 code paths, and emits metrics with v6 values
in the `ip` label — while the actual outbound DNS queries are sent
to v4 destinations the host can reach.

**Why this priority**: P2 because it's the visible validation path
for the v4/v6 work — without it, the user can't easily confirm the
fix works on a v4-only host. Also makes the dashboard's IPv6
behaviour reviewable by anyone running the demo, which is
operationally important for an exporter whose value proposition
includes IPv6-aware DNS monitoring.

**Independent Test**: Bring up the demo with default config on a
host with IPv6 disabled. The Grafana dashboard's NS-records and
per-NS metric tables show at least one row whose `ip` value is an
IPv6 address (e.g., `2001:db8::1`), populated with real probe data
even though the underlying DNS queries went to v4 addresses.

**Acceptance Scenarios**:

1. **Given** the address-override config carries a mix of v4 and
   v6 keys, **When** the exporter resolves an address that matches
   a v6 key, **Then** the override is applied (the outbound query
   goes to the mapped v4 destination) and the original v6 address
   is preserved in the `ip` label of all emitted metrics.
2. **Given** the demo's bundled zone files include an NS with an
   AAAA record, **When** the demo stack is brought up on a v4-only
   host, **Then** the Grafana dashboard's NS-records table shows
   that NS with its v6 address visible in the `ip` column.
3. **Given** the operator writes an override key in an unusual but
   valid IPv6 textual form (e.g., omitted leading zeros, mixed
   case), **When** the exporter loads the config, **Then** the
   override matches regardless of the form the prober later
   produces for the same address (i.e., IP string normalisation
   happens at lookup, not at config-load — both sides agree on the
   same canonical form).

---

### User Story 4 — Test infrastructure supports dual-stack scenarios so future probes work both families (Priority: P2)

A developer adding a new probe type (TCP support, AXFR availability,
version disclosure, etc. per the proposals doc) should be able to
write integration tests that exercise IPv6 paths from day one,
without re-inventing AAAA fixtures or IPv6 referral glue. Today's
`testutil` only provides an `A()` record helper and only attaches A
records as referral glue; AAAA is unsupported in the test
infrastructure.

After this change, `testutil` ships AAAA record helpers, dual-stack
fixture topologies, and IPv6 referral-glue attachment so new probes
are written and tested against both families from the start.

**Why this priority**: P2, same priority as US3 — both are
enabling work that validates US1/US2 in their respective contexts
(visual dashboard for US3, automated CI for US4). Without US4,
every E1/E2/E3/E4 probe in the proposals doc gets shipped v4-only
and needs IPv6 retrofitted later. Same retrofit risk that motivated
promoting #23 to E0.

**Independent Test**: A new integration test can stand up a fixture
with at least one IPv6 nameserver, query it through the existing
prober APIs, and assert on the resulting metrics — all using
`testutil` helpers, no inline IPv6 plumbing.

**Acceptance Scenarios**:

1. **Given** `testutil` is in use by an integration test, **When**
   the test calls a helper to declare an AAAA record, **Then** the
   helper signature mirrors the existing `A()` helper and the
   resulting record is served correctly by the test DNS servers.
2. **Given** a test sets up a `ReferralServer` with both A and
   AAAA records for the same NS hostname, **When** the test
   exercises the prober, **Then** the referral response includes
   both v4 and v6 glue in the Additional section (matching real-
   world parent referral behaviour).
3. **Given** an integration test sets up a dual-stack topology
   (one NS with both A and AAAA), **When** the test queries
   metrics, **Then** the existing `AssertGauge*` helpers can
   assert on entries with IPv6 values in the `ip` label without
   needing test-specific workarounds.

---

### Edge Cases

- **NS hostname with no resolvable IP at all** (no A, no AAAA, or
  both queries time out). The NS appears nowhere — same as today.
  The empty `nameservers` slice → no probe series. Existing
  "no nameservers" log line covers it.
- **NS hostname with A success and AAAA timeout** (broken v6
  resolution path through the exporter's resolver, but the v4
  side works). The v4 series appears as normal; the v6 series
  appears as a failure (`query_success = 0`) rather than silent
  absence. Operator can distinguish broken-v6-probe from
  no-v6-configured.
- **NS hostname with A success and AAAA NODATA** (the hostname
  legitimately has no AAAA — the most common case, billions of
  zones). The v4 series appears; nothing is emitted for v6
  (NODATA is not a failure). Crucially, this MUST NOT generate a
  failure series — that would create noise on the vast majority
  of zones.
- **Same IP address appearing under multiple NS hostnames** (rare
  but legal — e.g., a single anycast IP advertised under several
  NS names). Each (hostname, IP) pair is its own series; the
  duplication is information, not a bug.
- **Parent referral that includes IPv6 glue inline** (large modern
  TLDs increasingly do this). The exporter MUST consume the IPv6
  glue from the Additional section the same way it consumes IPv4
  glue today, without an extra round trip.
- **Parent referral that lacks any glue at all** (already-handled
  case fixed in #14, but must continue to work for both families).
  The exporter resolves both A and AAAA out-of-band; the prober's
  self-side queries (post-#14 fix) iterate the resolved
  nameservers slice that now includes v6 entries.
- **Exporter host with no IPv6 connectivity** (operator's machine
  is v4-only despite the zone having v6 NSs). Every AAAA query
  fails; the failure must be visible in metrics, not silenced. The
  operator decides whether that's a probe-host config problem or
  a zone problem.

## Requirements *(mandatory)*

### Functional Requirements

#### Resolution and IP fan-out

- **FR-001**: The exporter MUST query both A and AAAA records for
  every nameserver hostname it tries to resolve, whether the
  resolution happens via the parent referral's Additional section
  or via out-of-band lookup when glue is missing.
- **FR-002**: The exporter MUST treat every resolved IP address as
  its own probe target. If a single NS hostname resolves to N IPs
  (any mix of v4 and v6, any count), the exporter MUST run every
  per-NS probe against all N IPs and emit N series per metric, one
  per IP.
- **FR-003**: An IPv6-only NS hostname (AAAA only, no A) MUST NOT
  be silently dropped from probing or metrics. It MUST be treated
  identically to an IPv4-only NS hostname, with the v6 address
  carried in the `ip` label.
- **FR-004**: The exporter MUST emit one and only one series per
  (zone, nameserver-hostname, IP) tuple in every per-NS metric
  family. No double-counting; no de-duplication that hides
  legitimate multi-IP NSs.

#### Glue prober internals

- **FR-005**: The glue prober's self-side query loop (the
  authoritative-NS-asked-what-its-own-NSes-are query) MUST issue
  both A and AAAA queries when looking up the IPs of NSs the auth
  servers report. NSs that the auth reports with only AAAA records
  MUST surface in the `source="self"` series.
- **FR-006**: When iterating the resolved nameservers list to
  self-query each NS, the glue prober MUST handle entries whose IP
  is a v6 address — connecting to them over IPv6 — and emit the
  same `source="self"` series shape as for v4 entries.

#### Metric and label compatibility

- **FR-007**: Every existing per-NS metric series that today carries
  an `ip` label MUST continue to use the same label name and the
  same metric name. IPv6 entries appear as additional series in
  those same metric families, distinguished by the value in the
  `ip` label (a textual IPv6 address vs a textual IPv4 address).
- **FR-008**: Existing IPv4-only zones (every metric series the
  exporter emits today for a zone with no IPv6 NSs) MUST be
  byte-identical before and after this change. No new labels added
  to v4 series; no v4 series removed or renamed.
- **FR-009**: Once the exporter has obtained an IPv6 IP for a
  nameserver (either from parent glue or out-of-band resolution),
  any subsequent probe failure against that IP — connection
  refused, query timeout, malformed response, SERVFAIL — MUST be
  reported through the same `query_success = 0` mechanism the
  exporter already uses for IPv4 failures. The exporter MUST NOT
  silently elide such probe-time failures.

  Resolution-time failures (failing to *find* AAAA records in the
  first place) are a separate concern handled per research.md R-3:
  NODATA on AAAA is silent (no metric series — the hostname
  legitimately has no v6 address), protocol failure on AAAA is
  logged at WARN but does not produce a metric series either
  (because there's no IP to label it with).

#### Parent-side handling

- **FR-010**: When a parent referral includes IPv6 glue in its
  Additional section (AAAA records), the exporter MUST consume that
  glue without an extra resolution round trip — the same way it
  consumes IPv4 glue today.
- **FR-011**: When a parent referral lacks IPv6 glue for an NS that
  has only AAAA (or has both A and AAAA but parent only attached
  the A glue), the exporter MUST resolve the missing AAAA out-of-
  band by walking the delegation chain — symmetric with how it
  already resolves missing A glue out-of-band post-#14.

#### Address-override and demo wiring

- **FR-012**: The exporter's address-override mechanism
  (`config.AddressOverrides`, today a map of IP-string → host:port)
  MUST accept IPv6 addresses as map keys with the same semantics as
  IPv4 keys. A v6 key MAY map to a v4 destination, enabling
  IPv6-capable monitoring on hosts that cannot make outbound IPv6
  connections.
- **FR-013**: When the prober looks up an IP in the override map,
  the lookup MUST succeed regardless of the textual form of the IP
  (e.g., `2001:db8::1` vs `2001:0db8:0000:0000:0000:0000:0000:0001`
  vs `2001:DB8::1`). Both sides — the operator-provided config and
  the prober's runtime IP string — MUST be normalised to a single
  canonical form before comparison.
- **FR-014**: When an override is applied, the IP address that
  appears in metric labels (`ip="..."`) MUST remain the original
  IP from the delegation data — NOT the override destination. The
  override only redirects the network connection; it does not
  rewrite the data the exporter reports.
- **FR-015**: The demo MUST include two distinct IPv6 patterns,
  each rendering visibly differently on the dashboard so an
  evaluator can see what each pattern looks like:
  - **(a) Dual-stack NSes** — the existing `healthy.demo.` zone
    gains AAAA records on its existing NSes, so each NS shows up
    as two series (one per IP family). Pattern: the common modern
    case where an NS is reachable via both v4 and v6.
  - **(b) IPv6-only NSes** — a new demo zone (e.g.,
    `v6-only.demo.`) whose NSes have only AAAA records (no A).
    Pattern: the original #23 failure mode — pre-fix this zone
    would have produced no per-NS metrics at all; post-fix every
    series appears with the v6 address in the `ip` label.

  The demo's bundled exporter config (`demo/exporter/dnshealth.yml`)
  MUST include address overrides that map the v6 addresses from
  both patterns to in-container IPv4 endpoints — enabling the
  visible v6 entries on an IPv4-only host. The new `v6-only.demo.`
  zone MUST be added to the demo's configured zone list and
  served by a dedicated (or shared) CoreDNS container matching the
  existing demo topology.

  *Out of scope for this feature*: a third demo zone exercising
  the FR-011 path (parent referral with A glue but no AAAA glue,
  triggering out-of-band AAAA resolution). The integration tests
  cover that path; if visual demo coverage of it becomes useful,
  it lands as a follow-up.

#### Test infrastructure

- **FR-016**: The project's test-fixture package (`testutil`) MUST
  provide a helper for declaring AAAA records, mirroring the
  signature and behaviour of the existing `A()` helper.
- **FR-017**: The test-fixture referral server MUST attach AAAA
  records as glue in the Additional section of referral responses,
  using the same hostname-matching logic it uses for A glue today.
- **FR-018**: The test-fixture authoritative server MUST answer
  AAAA queries against its record set (the existing query-handling
  logic should already do this for any record type the test
  declares; this requirement is a guard against regressions).

### Key Entities

- **Nameserver record (resolved)**: A (hostname, IP) pair where
  IP is a single textual address (v4 or v6). Today modelled as
  `prober.Nameserver{Hostname, IP string}`. After this change the
  same struct is reused, but the caller emits multiple instances
  per hostname instead of one.
- **Resolved-address set**: All addresses (A + AAAA) found for a
  given hostname via parent glue and/or out-of-band resolution.
  Internal to the resolution step; flattens into one
  `Nameserver` entry per address before reaching the probers.
- **Per-(NS, IP) probe result**: Each existing prober result is now
  keyed by both nameserver hostname AND IP. The `ip` label already
  carries this; no metric schema change.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For every NS hostname in the configured zones that
  has at least one AAAA record at probe time, at least one metric
  series with that v6 address in the `ip` label exists in the
  `/metrics` output within one probe cycle. Verifiable by grep'ing
  for the v6 address in `/metrics`.
- **SC-002**: For every NS hostname that resolves to multiple IP
  addresses (any combination of v4 and v6), the per-NS metric
  series count for that hostname equals the count of resolved IPs.
  Verifiable per-zone by counting series in `/metrics`.
- **SC-003**: For an IPv4-only zone (zone whose configured NSs
  return only A records), the set of metric series emitted is
  byte-identical before and after this change. Verifiable by
  capturing `/metrics` against the demo's IPv4-only zones before
  and after, comparing.
- **SC-004**: The existing demo smoke test (`demo/smoke.sh`)
  passes without modification to its assertions.
- **SC-005**: New integration tests cover three scenarios that
  fail today and pass after this change: (a) IPv6-only NS, (b)
  dual-stack NS (A + AAAA at the same hostname), (c) parent
  referral with v4-only glue requiring out-of-band v6 resolution.
- **SC-006**: When the user redeploys the exporter against their
  live zones (the deployment referenced in #23), the previously-
  invisible IPv6 NS addresses appear in `/metrics`. Verified by
  spot-checking the deployed instance after deploy.
- **SC-007**: No metric series that was present in `/metrics`
  before this change is absent after. Verifiable by series-set
  diff against a captured baseline.
- **SC-008**: A new integration test using only `testutil`
  helpers can declare an AAAA record, stand up a dual-stack
  referral, and assert on a metric with an IPv6 value in the
  `ip` label — no inline IPv6 plumbing, no test-specific glue
  attachment. Verifiable by reading the resulting test code:
  symmetric in shape with existing IPv4-only tests.
- **SC-009**: Bringing up the demo (`cd demo && docker compose up
  -d --build`) on a host with IPv6 disabled produces, within one
  probe cycle, at least one `dnshealth_ns_record` series whose
  `ip` label is an IPv6 address. Verifiable by `curl
  http://localhost:9053/metrics | grep dnshealth_ns_record | grep ':'`
  (the colon is unique to IPv6 textual form among IP families
  the demo would emit).
- **SC-010**: The demo Grafana dashboard, on a fresh demo bring-
  up, distinguishably shows the two FR-015 patterns:
  - With `$zone=healthy.demo.` selected, every NS in the
    "NS records" tables appears as two rows (one per IP family) —
    the dual-stack pattern.
  - With `$zone=v6-only.demo.` selected, every NS appears with
    only an IPv6 value in the IP column — the v6-only pattern.
  Together these two views let an evaluator see both shapes side
  by side without external setup.

## Assumptions

- The exporter host has functional IPv6 connectivity in
  deployments that care about IPv6 NSs. When it doesn't, the
  AAAA-side probes fail visibly (FR-009), surfacing the host-
  config problem rather than the exporter silently giving up. We
  are not adding a "disable IPv6 path" config toggle in this work
  — operators in IPv4-only environments will see noisy v6
  failures, and that's their signal to either fix v6 or file a
  follow-up for an opt-out.
- The metric schema does NOT gain a new `ip_family` label in
  this work. The `ip` label already disambiguates families by its
  textual value (IPv6 addresses contain colons; IPv4 don't), and
  the dashboard can derive `ip_family` in PromQL if needed. If
  a future ticket establishes that a derived label is worth the
  cardinality cost, it lands as its own change.
- The demo Docker Compose stack stays IPv4-only at the network
  layer. Adding native IPv6 to the Compose bridge is a larger
  piece of work (Docker bridge networks need explicit IPv6
  enablement, the demo's static IP plan needs a separate IPv6
  range, etc.). Instead, demo zone files declare some NSs with
  AAAA records pointing at "public-looking" IPv6 addresses, and
  the demo's exporter config maps those v6 addresses through the
  same address-override mechanism the demo already uses for v4
  addresses — outbound queries land on the in-container IPv4
  endpoints. The exporter sees v6 in its data model and metrics
  regardless of the host's IPv6 connectivity. This is the
  approach US3 / FR-012-FR-015 codify.
- The existing delegation cache (`cache.DelegationCache`) is
  unaffected — its inputs are zones, its outputs are
  `DelegationResult` containing `NSRecords` slices. The slices
  may now contain v6 entries, but the cache contract doesn't
  change.
- The `prober.RootServers` list (already configurable per config
  via `root_servers`) is unchanged. Operators who run against an
  IPv6-only root via that config will benefit from this work
  automatically; operators using the IPv4 defaults will too.
- No backward-incompatible change is made to the
  `prober.Nameserver` exported type. The struct stays
  `{Hostname, IP string}`; the call sites that previously emitted
  one instance per hostname now emit one per IP. Code outside
  the exporter that imports this package (none today, but in
  principle) sees the same type.
- This work is the prerequisite for E0 in the proposals doc; once
  it lands, the rest of E1-E5 can be designed against a
  dual-stack baseline without retrofit risk.
