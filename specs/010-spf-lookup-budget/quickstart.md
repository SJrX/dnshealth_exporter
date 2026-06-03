# Quickstart: Reading the SPF Lookup-Budget Signals

Completes the SPF checks from spec 009. The dashboard's **"Email auth — status"** panel gains an **"SPF within the 10-lookup budget"** row; the PromQL below is for ad-hoc queries and alerting (which live in your Prometheus/Alertmanager, not the exporter — Principle V).

> Why it matters: RFC 7208 §4.6.4 caps an SPF record at **10 DNS lookups** when evaluated, counting recursively through every `include:`/`redirect=`. Exceed it and receiving mail servers return **PermError** — SPF silently stops working, even though the record looks perfect. The offending lookups are almost always hidden inside third-party `include:` records you don't control.

## PromQL

```promql
# Zones whose SPF is over the 10-lookup budget (PermError at receivers)
dnshealth_spf_lookup_budget_exceeded == 1

# How many lookups each zone's SPF currently needs (11 = "≥11")
dnshealth_spf_lookup_count

# Zones close to the limit — worth trimming before they tip over
dnshealth_spf_lookup_count >= 8 and dnshealth_spf_lookup_budget_exceeded == 0

# Caveat: an include was unreachable this cycle, so the count is a lower
# bound — a FAIL is NOT raised on these (no transient-include false alarm)
dnshealth_spf_lookup_eval_complete == 0
```

## What the row tells you

- **PASS** — the record resolves to ≤10 lookups. (A zone with `v=spf1 -all` and no mechanisms is 0 lookups → PASS.)
- **FAIL** — the record genuinely needs ≥11 lookups; trim `include:`s. Receivers are PermError-ing your SPF right now.
- **N/A** — the zone has no single valid SPF record (none, multiple, or malformed) — see the SPF rows above; there's nothing to budget-check.

The row only FAILs on a **resolved** over-budget count. If a third-party include is briefly unreachable, the row stays PASS and `dnshealth_spf_lookup_eval_complete` reads 0 — so a flaky include never cries wolf.

## Try it in the demo

```bash
cd demo && docker compose up -d --build
# after the first probe cycle:
./promql.sh query 'dnshealth_spf_lookup_count'
./promql.sh state '<the budget-row predicate>' email-toomanylookups.demo. email-healthy.demo.
```

`email-toomanylookups.demo.` chains `include:` to in-zone sub-records summing past 10 → its row reads **FAIL** with a count of 11; `email-healthy.demo.` reads **PASS** at 0.
