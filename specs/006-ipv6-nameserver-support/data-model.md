# Data Model: 006-ipv6-nameserver-support

**Date**: 2026-05-22
**Status**: Complete

This feature does not introduce persisted data, new metrics, or new
config schemas. The "data model" describes the **runtime entities**
the change affects — the contracts between layers that need to be
coherent after the v6 work lands.

---

## Entity 1: `prober.Nameserver` (existing — unchanged shape)

```go
type Nameserver struct {
    Hostname string  // e.g. "ns1.example.com."
    IP       string  // e.g. "203.0.113.1" or "2001:db8::1"
}
```

**Semantic change** (not a struct change): callers now emit **one
instance per resolved IP address**, not one per hostname. A
hostname that resolves to two A records produces two entries; a
hostname with one A + one AAAA produces two entries; a hostname
with three AAAA records produces three entries.

**Validation rules**:
- `Hostname` MUST be FQDN with trailing dot (existing convention).
- `IP` MUST be a parseable IP string in any family. After this
  feature, the field carries either an IPv4 dotted-quad or an
  IPv6 colon-separated address.
- The `(Hostname, IP)` tuple MUST be unique within a single
  nameservers slice — no duplicate entries from the resolver.

**Relationships**: consumed by every prober as `[]Nameserver`. After
this change, the slice may be longer than the count of distinct
NS hostnames.

---

## Entity 2: `prober.DelegationResult.NSRecords` (existing — semantics unchanged)

```go
type DelegationResult struct {
    NSRecords []Nameserver
    Glue      []Nameserver
    // ...
}
```

The `NSRecords` slice still represents what the parent advertised
in its delegation response. Each entry's `IP` field comes from
parent-supplied glue and may be empty when the parent omitted glue
(the standard pre-existing case fixed for the self-side loop by
#14). For NSs whose parent supplied AAAA glue inline (FR-010),
those entries' `IP` field is now an IPv6 string.

**Important**: this entity is *parent-side*. It is the input to the
out-of-band resolution step, not the post-resolution
`[]Nameserver` slice that probers see. The two slices have
different lengths after this feature.

---

## Entity 3: Resolved-addresses set (internal, transient)

The intermediate value produced by `ResolveHostnames(hostname)`
before being flattened into `[]Nameserver` entries.

```go
// Conceptual — actual return type is just []string + error.
type resolvedAddresses struct {
    A    []string  // all IPv4 addresses found
    AAAA []string  // all IPv6 addresses found
    // Failures collapse into a top-level error; per-family
    // failures are visible in caller logs.
}
```

Implementation may flatten directly to `[]string` (returning all
addresses concatenated, family-agnostic) — the caller doesn't need
to distinguish at the API boundary because every address becomes
one `Nameserver` entry regardless of family. Return shape is a
plan-level decision.

**State transitions**:
- `(A success, AAAA success)` → both families returned, fan-out
  produces one entry per address.
- `(A success, AAAA NODATA)` → only A addresses returned. NODATA
  is silent; no series emitted for v6 family of this hostname.
- `(A success, AAAA failure)` → A addresses returned. AAAA failure
  is logged but does not become a top-level error (so v4 series
  still appear).
- `(A NODATA, AAAA success)` → only AAAA addresses returned.
- `(A NODATA, AAAA NODATA)` → empty result, NO error (legitimately
  unresolved hostname). Caller drops the NS from probing —
  identical to today's "ResolveHostname returns no answer" path.
- `(A failure, AAAA failure)` → top-level error; caller logs and
  skips this NS — same as today's behaviour.

---

## Entity 4: `config.AddressOverrides` (existing — accepted-key semantics widened)

```go
type Config struct {
    AddressOverrides map[string]string `yaml:"address_overrides"`
    // ...
}
```

**Today**: keys are IPv4 dotted-quads; values are `host:port`
strings pointing at the in-container destination.

**After this feature**:
- Keys MAY be IPv6 textual addresses in addition to IPv4.
- Both keys (at config-load) and lookup IPs (at prober runtime) are
  normalised via `net.ParseIP(s).String()` before comparison. So
  `"2001:db8::1"`, `"2001:0db8:0000:0000:0000:0000:0000:0001"`, and
  `"2001:DB8::1"` are all equivalent keys.
- Values are unchanged — same `host:port` strings, typically v4
  destinations even when the key is v6 (the whole point of US3).
- Config-load REJECTS keys that don't parse as valid IPs (new
  validation; today unparseable keys would silently never match).

---

## Entity 5: Per-NS probe result series (existing — semantics widened)

Every per-NS metric series that exists today and carries an `ip`
label:

- `dnshealth_query_success{zone, nameserver, ip, check}`
- `dnshealth_ns_record{zone, nameserver, ip, source}`
- `dnshealth_ns_recursion_available{zone, nameserver, ip}`
- `dnshealth_soa_serial{zone, nameserver, ip}`
- `dnshealth_soa_refresh_seconds{zone, nameserver, ip}` (and
  the other SOA timer metrics)
- `dnshealth_query_duration_seconds{zone, nameserver, ip, check}`
- `dnshealth_ns_glue{zone, nameserver, ip, source}` (if present)

**Semantic change** (no metric / label rename):

- The `ip` label value MAY be an IPv6 textual address.
- Per (zone, nameserver-hostname) there MAY now be multiple
  series, one per (hostname, IP) tuple, distinguished by the `ip`
  label's value.
- Per-family disambiguation is by inspecting the `ip` value
  (colons = v6, dots = v4) — no new `ip_family` label.
- For zones whose NSs all resolve to IPv4 only, the series set
  is byte-identical before and after this feature (FR-008).

---

## Relationships

```text
config.AddressOverrides ─normalised at load──► canonical key form
                                                      │
                                                      ▼
        ┌── ResolveAddress(ip) ──► override lookup (canonical) ──► host:port
        │                                                                 │
prober  ┤                                                                 ▼
        │                                                          DNS query target
        └── ResolveHostnames(host) ──► []string (A + AAAA)
                                              │
                                              ▼
                       cycle.runner / testutil.Probe
                                              │
                                              ▼
                  []Nameserver  (one entry per IP, any family)
                                              │
                                              ▼
                              every prober: per-NS-IP loop
                                              │
                                              ▼
                  Per-NS metric series (ip label carries either family)
```

The only structural change in this graph vs today's: the
`ResolveHostname` node returning a single string is replaced by
`ResolveHostnames` returning a slice, and the downstream fan-out
loop produces one `Nameserver` per element.

---

## State transitions

The probe cycle itself is unchanged — same scheduler, same cache,
same per-zone flow. The only state-bearing entity in this feature
is the address-override map at config-load time:

```
[YAML config] ──load──► AddressOverrides map ──normalise keys──► canonical map
                                                                       │
                                                                       │ retained until next reload
                                                                       ▼
                                              prober.ResolveAddress (function variable)
```

On SIGHUP reload (existing path, `applyReloadedConfig` in main.go):
the entire map is replaced unconditionally; canonicalization runs on
the new keys; old override semantics are dropped. This matches the
existing reload contract.
