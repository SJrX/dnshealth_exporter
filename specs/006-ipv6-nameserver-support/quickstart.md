# Quickstart: 006-ipv6-nameserver-support

**Date**: 2026-05-22

Maintainer-facing operational summary: how to verify the IPv6 fix
locally, how to use the override trick to test on an IPv4-only host,
and how to add IPv6 fixtures to a new integration test.

## Verify the fix locally (demo)

```bash
cd demo
docker compose up -d --build
sleep 30   # one probe cycle
curl -s http://localhost:9053/metrics | grep dnshealth_ns_record | grep ':' | head -5
```

You should see at least one series with an IPv6-looking `ip` label
value (the textual giveaway is the `:` separator):

```
dnshealth_ns_record{ip="2001:db8::1",...} 1
```

Tear down:

```bash
docker compose down -v
```

## Verify on the Grafana dashboard

After `docker compose up`, open <http://localhost:3000>. Switch the
`$zone` variable to see the two new IPv6 patterns:

- **`healthy.demo.`** — dual-stack pattern. Every NS appears as
  two rows in the "NS records" tables, one per IP family (IPv4
  dotted-quad and IPv6 colon-separated).
- **`v6-only.demo.`** — IPv6-only pattern. Every NS appears with
  only its IPv6 address in the Glue IP column. Pre-fix this zone
  produced no per-NS metrics at all.

If the IP column is too narrow for v6 strings, file a small
follow-up to widen it — the column-width tweak is mechanical.

## Run the dual-stack integration tests

```bash
go test -tags=integration -count=1 ./prober/... ./testutil/...
```

The new tests added in this feature cover three regression
scenarios. They should all pass:

- IPv6-only NS surfaces in `dnshealth_ns_record`.
- Dual-stack NS produces two series, one per IP.
- Parent referral with IPv4-only glue triggers out-of-band AAAA
  resolution.

## Use the address-override pattern to test IPv6 on a v4-only host

The demo's `demo/exporter/dnshealth.yml` already demonstrates this
pattern after the feature lands. For your own zones:

```yaml
zones:
  - example.com

address_overrides:
  # Map a public-looking v6 address to a v4 destination you can reach.
  "2001:db8::1": "127.0.0.1:53"   # or whatever your local resolver is
```

The exporter sees `2001:db8::1` in its data model and emits metrics
with that v6 address in the `ip` label; the actual queries go to
`127.0.0.1:53`. Useful for local development against zones whose
real NSs have AAAA records you can't reach.

## Add an IPv6 fixture to a new integration test

```go
//go:build integration

package prober_test

import (
    "testing"

    "github.com/sjr/dnshealth_exporter/prober"
    . "github.com/sjr/dnshealth_exporter/testutil"
)

func TestSomeNewProber_IPv6(t *testing.T) {
    env := NewDNSFixture(t).
        ReferralServer("127.240.0.1:"+TestPort,
            SOA("example.test"),
            NS("example.test", "ns1.example.test"),
            A("ns1.example.test", "127.240.0.2"),
            AAAA("ns1.example.test", "2001:db8::2"),  // NEW: AAAA helper
        ).
        Server("127.240.0.2:"+TestPort,
            // Auth server records here.
        ).
        Start(t)
    defer env.Stop()

    metrics := env.Probe(prober.ProbeSomething, "example.test")

    // The same NS now appears as two entries — one per IP.
    AssertGaugeExists(t, metrics, "dnshealth_ns_record",
        WithLabels("ip", "127.240.0.2", "source", "parent"))
    AssertGaugeExists(t, metrics, "dnshealth_ns_record",
        WithLabels("ip", "2001:db8::2", "source", "parent"))
}
```

## Backward-compatibility check

```bash
# Before the feature lands (on main):
curl -s http://localhost:9053/metrics | sort > /tmp/metrics-before.txt

# After deploying the feature (against the same zones):
curl -s http://localhost:9053/metrics | sort > /tmp/metrics-after.txt

# IPv4-only series should be present in both. New v6 series only in after.
comm -23 /tmp/metrics-before.txt /tmp/metrics-after.txt   # removed series — should be empty
comm -13 /tmp/metrics-before.txt /tmp/metrics-after.txt | head -20  # added series — should be all v6
```

`comm -23` must produce **no output** for zones whose NS set is
IPv4-only (FR-008, SC-003, SC-007).
