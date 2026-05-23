# Contract: Address-override config

**Date**: 2026-05-22
**Type**: Operator-facing YAML config schema.

The `address_overrides` map in the exporter config (today documented
implicitly by examples in `demo/exporter/dnshealth.yml`) is widened
in this feature to accept IPv6 keys. This document is the contract
operators rely on when writing such configs.

## YAML shape

```yaml
address_overrides:
  # IPv4 keys — existing behaviour, unchanged:
  10.0.0.1: "coredns-healthy:53"
  192.0.2.5: "coredns-soa-mismatch-a:53"

  # IPv6 keys — new in this feature. Both forms work; both
  # normalise to the same canonical string at config-load.
  "2001:db8::1": "coredns-healthy:53"
  "2001:DB8:0:0:0:0:0:2": "coredns-healthy:53"
  "[2001:db8::3]": "coredns-healthy:53"  # bracket form also accepted
```

**Note**: YAML treats unquoted strings starting with `[` or
containing `:` specially. IPv6 keys SHOULD be quoted in YAML to
avoid parsing surprises. The exporter's config loader does NOT
require quoting (it parses whatever YAML produces) but the
convention is unambiguous.

## Semantics

### Lookup

When the prober resolves an NS to an IP and asks
`prober.ResolveAddress(ip)` for the address to query:

1. The IP is canonicalised via `net.ParseIP(ip).String()`.
2. The override map (also canonicalised at config-load) is
   consulted with the canonical key.
3. If found → the mapped `host:port` is returned (typically
   pointing at an in-container destination).
4. If not found → `net.JoinHostPort(ip, "53")` returns the default
   target (correctly bracketed for v6).

### Family-mixing is supported

A v6 key MAY map to a v4 destination. This is the supported
configuration for "test IPv6 paths on an IPv4-only host" — the
exporter's data model and metrics carry the v6 IP throughout, but
the actual outbound socket goes to the v4 destination the host can
reach.

### Metric labels are NOT rewritten

The `ip` label in every per-NS metric series ALWAYS carries the
original IP the exporter resolved — never the override destination.
An override redirects the network connection; it does not rewrite
the data being reported.

For example, given:

```yaml
address_overrides:
  "2001:db8::1": "coredns-healthy:53"
```

If the zone delegates to an NS that resolves to `2001:db8::1`, the
exporter emits:

```
dnshealth_ns_record{ip="2001:db8::1", ...}     # the original v6 IP
dnshealth_query_success{ip="2001:db8::1", ...}  # same
```

…while the actual DNS queries go to `coredns-healthy:53`. The
dashboard shows v6 entries; the network sees v4 packets.

### Config-load validation

Keys that do not parse as IPs (via `net.ParseIP`) MUST be rejected
at config-load with a clear error identifying the offending key.
Today such keys would silently never match. Hardening this is part
of this feature.

Values are not validated beyond YAML string parsing — typos in the
`host:port` destination surface as DNS query failures (which become
`query_success = 0` series) rather than load-time errors.

### Reload behaviour

On SIGHUP reload (existing exporter behaviour), the entire
`address_overrides` map is replaced unconditionally — the
canonicalised v6/v4 mix of the new config is what takes effect; the
old map is discarded. Empty / missing `address_overrides` in the new
config means the prober's default behaviour (`net.JoinHostPort(ip,
"53")`) is restored.

## Out of scope

- DNS-over-TLS / DoH override destinations — keep the `host:port`
  shape only.
- Per-zone override granularity — overrides remain global to the
  exporter.
- Reverse mapping (`host:port` → IP) — not needed.
