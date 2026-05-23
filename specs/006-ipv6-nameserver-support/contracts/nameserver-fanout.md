# Contract: Nameserver fan-out and per-IP probing

**Date**: 2026-05-22
**Type**: Internal contract between resolver and probers.

This contract codifies how nameserver hostnames are expanded to IPs
and how probers iterate over them. It governs the invariants every
prober (existing and future) can rely on.

## Resolution contract

`prober.ResolveHostnames(ctx, hostname, client, logger) ([]string, error)`

- Queries both A and AAAA for the hostname.
- Returns all addresses found across both families, in resolution
  order — A addresses first, AAAA addresses second.
- Empty slice + nil error = hostname has neither A nor AAAA
  records (both queries returned NODATA). Caller drops the hostname.
- Non-empty slice + nil error = at least one family resolved
  successfully. Caller iterates each address.
- Non-nil error = both families failed at the protocol level
  (timeout, SERVFAIL, connection error). Caller logs and skips.

A partial-success case (A succeeds, AAAA times out, or vice versa)
returns the successful family's addresses with a nil error. The
failed family's failure is logged at WARN level; the partial result
is preferable to dropping the entire hostname.

## Fan-out contract

Production caller (`cycle.runner.probeZone`) and test caller
(`testutil.DNSFixture.Probe`) both follow this pattern:

```go
var nameservers []prober.Nameserver
for _, ns := range delegation.NSRecords {
    if ns.IP != "" {
        // Parent supplied glue inline — use directly.
        nameservers = append(nameservers, ns)
        continue
    }
    ips, err := prober.ResolveHostnames(ctx, ns.Hostname, client, logger)
    if err != nil {
        logger.Warn("could not resolve NS hostname",
            "zone", zone, "ns", ns.Hostname, "err", err)
        continue
    }
    for _, ip := range ips {
        nameservers = append(nameservers, prober.Nameserver{
            Hostname: ns.Hostname,
            IP:       ip,
        })
    }
}
```

**Invariant**: every entry in `nameservers` has a non-empty `IP`
field. Probers can rely on this and drop the empty-IP guard that
existed pre-#14.

**Invariant**: a single NS hostname may appear in multiple entries,
one per resolved IP. Probers that want to dedupe by hostname MUST
do so explicitly; the default loop iterates each (hostname, IP)
pair.

## Per-prober contract

Every prober receives `nameservers []Nameserver` and iterates per
entry. Each iteration produces one or more `ProbeResult` entries
keyed by the entry's `(Nameserver.Hostname, Nameserver.IP)` pair.

**Per-IP query target**: probers query
`prober.ResolveAddress(ns.IP)` to get the network destination.
That function handles override lookup (with normalised keys per
[`address-override.md`](./address-override.md)) and defaults to
`net.JoinHostPort(ns.IP, "53")` — correct for both v4 and v6.

**Metric label population**: the `ip` label in emitted metrics
ALWAYS carries `ns.IP` directly — the address from the resolver,
never the override destination. See `address-override.md` for the
rationale.

## Glue prober self-side contract

The glue prober's self-side loop (`querySelfForNSAndA` in
`prober/glue.go`) MUST mirror the same resolution shape: when it
asks an authoritative NS for its own NS records and then resolves
each NS hostname's address, it MUST query both A and AAAA and
produce one `source="self"` series per (hostname, IP) pair.

A self-reported NS that only has AAAA MUST surface in
`dnshealth_ns_record{source="self"}` exactly as it would if it had
A records — symmetric handling, no v4 bias.

## Test-fixture contract

`testutil` provides the symmetric helpers needed to exercise this
shape without inline plumbing:

- **`AAAA(name, ipv6 string) dns.RR`** — analogous to the existing
  `A()` helper. Returns a properly-formed `*dns.AAAA` record.
- **Referral-mode glue attachment** — the `ReferralServer` handler
  attaches both `*dns.A` and `*dns.AAAA` records as glue in the
  Additional section when their `Header().Name` matches an NS in
  the Authority section.
- **Authoritative-mode AAAA answers** — already works (the existing
  `qtype == rr.Header().Rrtype` matching in fixture.go handles any
  RR type); guarded by a regression test.

## Compatibility guarantees

- IPv4-only zones produce a byte-identical set of metric series
  before and after (FR-008).
- No existing metric or label is renamed.
- No new label is added to the `ip`-bearing series in this feature.
  (A future `ip_family` derived label is left to a follow-up
  ticket if it becomes useful.)
- The `prober.Nameserver` exported type retains its
  `{Hostname, IP string}` shape; only the cardinality of caller-
  emitted instances per hostname changes.
