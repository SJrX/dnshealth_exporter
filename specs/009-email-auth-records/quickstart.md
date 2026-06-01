# Quickstart: Reading the Email-Auth Metrics

Operator-facing guide to the SPF + DMARC signals this feature adds. The dashboard's **"Email auth — status"** panel summarizes these per zone; the PromQL below is for ad-hoc queries and alerting rules (which live in *your* Prometheus/Alertmanager, not the exporter — Principle V).

> Mental model: **MX is where you receive mail; SPF/DMARC stop spammers forging your domain as a sender.** They are independent — every domain (even one that sends no mail) benefits from `v=spf1 -all` + DMARC `p=reject`. So these checks apply to every monitored zone regardless of MX.

## SPF

```promql
# Zones with no SPF record at all (spoofing exposure — WARN on the dashboard)
dnshealth_spf_present == 0

# Zones with a broken SPF record: multiple records (PermError) ...
dnshealth_spf_record_count > 1
# ... or present-but-malformed
dnshealth_spf_present == 1 and dnshealth_spf_valid == 0

# Permissive terminal qualifier — +all authorizes the whole internet
dnshealth_spf_terminal_all{qualifier="pass"}
# Soft/neutral or no terminal all (weak)
dnshealth_spf_terminal_all{qualifier=~"neutral|none"}
```

> The RFC 7208 10-lookup-budget check (`dnshealth_spf_lookup_*`) is **not in v1** — it ships with [#58](https://github.com/SJrX/dnshealth_exporter/issues/58).

## DMARC

```promql
# Zones with no DMARC policy
dnshealth_dmarc_present == 0

# DMARC present but malformed (no valid p= tag)
dnshealth_dmarc_present == 1 and dnshealth_dmarc_valid == 0

# Policy distribution across monitored zones
dnshealth_dmarc_policy                      # info gauge; the `policy` label is the value
count by (policy) (dnshealth_dmarc_policy)  # how many zones at none/quarantine/reject

# Zones only monitoring (p=none), not enforcing
dnshealth_dmarc_policy{policy="none"}

# Zones not collecting aggregate reports
dnshealth_dmarc_present == 1 and on(zone) dnshealth_dmarc_rua_present == 0
```

## What "good" looks like

A fully healthy zone:
- `dnshealth_spf_present 1`, `dnshealth_spf_record_count 1`, `dnshealth_spf_valid 1`
- `dnshealth_spf_terminal_all{qualifier="fail"} 1` (i.e. `-all`)  — `softfail` (`~all`) is also PASS
- `dnshealth_dmarc_present 1`, `dnshealth_dmarc_valid 1`, `dnshealth_dmarc_policy{policy="reject"} 1` (or `quarantine`)

## Try it in the demo

```bash
cd demo && docker compose up -d --build
# after the first probe cycle:
./promql.sh query 'dnshealth_spf_terminal_all'
./promql.sh query 'dnshealth_dmarc_policy'
./promql.sh state '<the row-A predicate>' email-broken.demo. email-healthy.demo.
```

Demo zones to look at: `email-healthy` (all green), `email-spf-only` (DMARC WARN), `email-none` (SPF+DMARC WARN), `email-permissive` (`+all`/`p=none` WARN), `email-broken` (FAIL), `email-nomail` (Null MX yet all green — proves MX-independence).
