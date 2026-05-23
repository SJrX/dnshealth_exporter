# Quickstart: NS Classification (Spec 007)

Operator-facing walkthrough for reading the new metrics, alerting on them, and verifying the feature in the demo deployment.

---

## What the feature gives you

Two new metric families plus one new dashboard row:

- `dnshealth_ns_classification{zone, nameserver, ip, classification}` — info gauge per (zone, NS hostname), with `classification` one of `parent-only`, `self-only`, `both`.
- `dnshealth_ns_classification_count{zone, classification}` — per-zone count gauge.
- New row `G` in the NS — status dashboard panel: "No stealth NSes (parent and self agree on NS set)".

---

## Reading the metrics

**List every "stealth" NS across all zones**:

```promql
dnshealth_ns_classification{classification="self-only"}
```

Each returned series identifies one (zone, NS hostname) where the auth-side lists an NS the parent doesn't advertise. The label set tells you which zone and which NS — no further query needed.

**List asymmetric NSes for one zone**:

```promql
dnshealth_ns_classification{zone="example.com.",classification!="both"}
```

Returns both `parent-only` and `self-only` for the named zone, so you see the full picture at a glance.

**Alert: any zone has new stealth NS appearing**:

```promql
changes(dnshealth_ns_classification_count{classification="self-only"}[1h]) > 0
```

Fires when the per-zone self-only count moved during the past hour — typically because a new asymmetric NS appeared.

**Alert: persistent asymmetry that warrants action**:

```promql
dnshealth_ns_classification_count{classification!="both"} > 0
```

Persistent FAIL on the dashboard row. Tune based on whether you legitimately run hidden-master setups; if you do, exclude those zones from the rule.

---

## Distinguishing legitimate hidden master from leaked / forgotten NS

The classifier emits a dedicated reachability gauge for every self-only stealth NS it detects — it actively resolves the NS hostname out-of-band and probes for SOA, since the standard SOA prober only queries parent-listed NSes and wouldn't otherwise reach a stealth NS:

```promql
# Does the stealth NS actually answer authoritatively when probed directly?
dnshealth_ns_stealth_reachable{zone="example.com.",nameserver="hidden-primary.example.com."}
```

- `= 1` → the NS resolves AND returned an authoritative SOA for the zone. Most likely a **working hidden master** (intentional NOTIFY-driven primary not in the public NS set).
- `= 0` → the NS hostname doesn't resolve, OR resolved but returned no authoritative SOA. Most likely a **leaked / forgotten listing** — operational dirt to clean up.
- **Series absent** → no self-only stealth NS detected for this zone this cycle (nothing to disambiguate).

The dashboard's row detail-text (visible via the "i" icon on the NS — status panel) summarizes this guidance.

---

## Limitations to remember

- **Not RFC-strict stealth detection.** Per the spec's definition section and the row's detail-text, this check surfaces NS-set divergence between sources we can query. A server that responds authoritatively for a zone but is unknown to BOTH parent and every reachable auth is invisible to this check — and to any single-vantage-point exporter.
- **Per-NS classification, not per-IP.** IP-level disagreement between parent glue and auth-side IPs for the same NS hostname is a separate concern; see issue #37.
- **Per-cycle freshness.** The classification reflects the most recent probe cycle. NSes that come and go between cycles will flicker — use `avg_over_time` or `max_over_time` to smooth.

---

## Verifying the feature in the demo

After bringing up the demo (`cd demo && docker compose up -d --build`), the new `hidden-master.demo.` zone exercises the self-only case:

```bash
curl -s http://localhost:9053/metrics | grep 'dnshealth_ns_classification'
```

Expected output includes:

```text
dnshealth_ns_classification{...,nameserver="ns1.hidden-master.demo.",...,classification="both"} 1
dnshealth_ns_classification{...,nameserver="ns2.hidden-master.demo.",...,classification="both"} 1
dnshealth_ns_classification{...,nameserver="hidden-primary.hidden-master.demo.",...,classification="self-only"} 1
dnshealth_ns_classification_count{zone="hidden-master.demo.",classification="self-only"} 1
dnshealth_ns_classification_count{zone="hidden-master.demo.",classification="parent-only"} 0
dnshealth_ns_classification_count{zone="hidden-master.demo.",classification="both"} 2
dnshealth_ns_stealth_reachable{zone="hidden-master.demo.",nameserver="hidden-primary.hidden-master.demo."} 0
```

The reachability gauge reads `0` because the demo's `hidden-primary.hidden-master.demo.` hostname has no A record anywhere reachable — it models the "leaked listing" pattern. A working-hidden-master demo would read `1`.

For healthy.demo. (no asymmetry), all counts read 0 except `both`:

```text
dnshealth_ns_classification_count{zone="healthy.demo.",classification="self-only"} 0
dnshealth_ns_classification_count{zone="healthy.demo.",classification="parent-only"} 0
dnshealth_ns_classification_count{zone="healthy.demo.",classification="both"} 2
```

The smoke test (`demo/smoke.sh`) asserts the `hidden-master.demo.` case at step A3d.

---

## Looking at the Grafana dashboard

Open Grafana (`http://localhost:3000`), pick a zone in the `$zone` variable selector. Scroll to the "NS — status" panel (middle of the top status row). The new row reads:

- **PASS** for zones with no asymmetric NSes (e.g., `healthy.demo.`)
- **FAIL** for `hidden-master.demo.` (one self-only NS present)

Hover the "i" icon on the panel header for the full per-row detail block, including the RFC-stealth scope disclaimer and the investigation pointer.
