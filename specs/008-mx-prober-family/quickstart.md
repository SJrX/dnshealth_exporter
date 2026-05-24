# Quickstart: MX Prober Family (Spec 008)

Operator-facing walkthrough for reading the new metrics, alerting on them, and verifying the feature in the demo deployment.

---

## What the feature gives you

Per-zone DNS-level email-health checks. The exporter probes each configured zone for MX records and, for every MX target, validates resolution, RFC-2181 CNAME correctness, and LDH syntax. It also recognizes RFC 7505 Null MX as the intentional "no email" opt-out.

New metric families (see `contracts/mx-metrics.md` for the full contract):

- `dnshealth_mx_info{zone, target, priority}` — info gauge per MX record
- `dnshealth_mx_resolves{zone, target}` — boolean per target
- `dnshealth_mx_is_cname{zone, target}` — boolean per target
- `dnshealth_mx_syntax_valid{zone, target}` — boolean per target
- `dnshealth_mx_is_primary{zone, target}` — boolean per target
- `dnshealth_mx_null_mx{zone}` — per-zone boolean (Null MX detected)
- `dnshealth_mx_count{zone}`, `_resolved_count{zone}`, `_cname_count{zone}` — per-zone aggregates

Plus a new "MX — status" dashboard panel (5 rows) and a per-MX records table.

---

## Reading the metrics

**List every MX record across all zones, sorted by priority**:

```promql
sort_by_label(dnshealth_mx_info, "zone", "priority")
```

Each series identifies one MX record; the labels carry zone, target, and priority.

**Find every CNAMEd MX target (RFC 2181 §10.3 violations)**:

```promql
dnshealth_mx_is_cname == 1
```

Returns one series per offending (zone, target). Operator gets the offending hostname directly from labels.

**Find zones with no MX records and no Null MX (silent inbound-mail failure)**:

```promql
(dnshealth_mx_count == 0) and (dnshealth_mx_null_mx == 0)
```

Returns one series per affected zone.

**Find zones where the primary MX changed in the past hour (failover detection)**:

```promql
changes(dnshealth_mx_is_primary == 1[1h]) > 0
```

Fires when the set of `is_primary=1` MXes changes — typically caused by priority reordering.

**Alert: any zone has an MX target that doesn't resolve**:

```promql
dnshealth_mx_resolves == 0
```

Fires per offending (zone, target). Caveat: a transient DNS error mid-cycle can produce a single spurious 0. Use `min_over_time(...[5m])` for stable alerting.

---

## Distinguishing intentional Null MX from misconfiguration

The exporter recognizes the RFC 7505 Null MX form (`0 .`) as an explicit "this domain doesn't receive email" declaration. The MX-presence row PASSes for these zones automatically — no per-zone alerting noise.

If you DO see `dnshealth_mx_null_mx == 1` for a zone you didn't intentionally configure that way, check the zone's authoritative records — someone may have published Null MX accidentally (it's a common copy-paste error from RFC examples).

The "No conflict between Null MX and real MX records" row catches the configuration error where both Null MX AND regular MXes coexist. RFC 7505 requires Null MX to be the SOLE MX record; coexistence is undefined behavior and MTAs may interpret it differently.

---

## Limitations to remember

- **DNS-level checks only.** The exporter does NOT verify SMTP connectivity, TLS certs, or actual mail delivery. If you need those, deploy `blackbox_exporter` with an SMTP prober config — that's the right tool for transport-layer checks.
- **No email-authentication checks.** SPF / DMARC / DKIM / MTA-STS / TLS-RPT / BIMI / DANE are deferred to a follow-up feature (tracked in #44). This spec is MX-specific.
- **Per-cycle freshness.** Each cycle re-probes; a transient DNS failure produces a single bad data point. Smooth with `min_over_time` / `max_over_time` for alerting.
- **No private-IP detection.** An MX target resolving to an RFC-1918 address (`10.x`, `192.168.x`, etc.) is treated as "resolves" — the exporter doesn't know whether your MTAs are reachable from the public internet.

---

## Verifying the feature in the demo

After bringing up the demo (`cd demo && docker compose up -d --build`):

```bash
curl -s http://localhost:9053/metrics | grep 'dnshealth_mx_'
```

For the demo's `mx-healthy.demo.` zone (two healthy MXes at priorities 10 + 20):

```text
dnshealth_mx_info{zone="mx-healthy.demo.",target="mail-a.mx-healthy.demo.",priority="00010"} 1
dnshealth_mx_info{zone="mx-healthy.demo.",target="mail-b.mx-healthy.demo.",priority="00020"} 1
dnshealth_mx_resolves{zone="mx-healthy.demo.",target="mail-a.mx-healthy.demo."} 1
dnshealth_mx_resolves{zone="mx-healthy.demo.",target="mail-b.mx-healthy.demo."} 1
dnshealth_mx_is_primary{zone="mx-healthy.demo.",target="mail-a.mx-healthy.demo."} 1
dnshealth_mx_is_primary{zone="mx-healthy.demo.",target="mail-b.mx-healthy.demo."} 0
dnshealth_mx_null_mx{zone="mx-healthy.demo."} 0
dnshealth_mx_count{zone="mx-healthy.demo."} 2
dnshealth_mx_resolved_count{zone="mx-healthy.demo."} 2
dnshealth_mx_cname_count{zone="mx-healthy.demo."} 0
```

For `mx-null.demo.` (Null MX):

```text
dnshealth_mx_info{zone="mx-null.demo.",target=".",priority="00000"} 1
dnshealth_mx_null_mx{zone="mx-null.demo."} 1
dnshealth_mx_count{zone="mx-null.demo."} 1
```

For `mx-broken.demo.` (one CNAMEd target + one unresolvable target):

```text
dnshealth_mx_is_cname{zone="mx-broken.demo.",target="cname-mail.mx-broken.demo."} 1
dnshealth_mx_resolves{zone="mx-broken.demo.",target="missing-mail.mx-broken.demo."} 0
dnshealth_mx_cname_count{zone="mx-broken.demo."} 1
```

Smoke assertions (A4g / A4h / A4i, added in T___) verify each case end-to-end.

---

## Looking at the Grafana dashboard

Open Grafana (`http://localhost:3000`), pick a zone in the `$zone` variable. The new **"MX — status"** panel shows 5 rows:

- Zone has MX records (or Null MX) — PASS for healthy + Null MX zones; FAIL if both absent.
- All MX targets resolve — PASS for `mx-healthy.demo.`; FAIL for `mx-broken.demo.`.
- No MX target is a CNAME — PASS for healthy; FAIL for broken.
- All MX target hostnames syntactically valid — PASS for all demo zones.
- No conflict between Null MX and real MX records — PASS everywhere in demo (no zone configured with both).

The **per-MX table** below the records row lists each zone's MX records ordered by priority, with columns showing resolves / is-CNAME / syntax-valid / role (primary/backup).
