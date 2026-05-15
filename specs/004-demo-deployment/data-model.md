# Phase 1 Data Model: Demo Deployment

This feature has no runtime data model in the conventional sense — the
demo is a deployment artifact, not an application with persistent
entities. What follows describes the static configuration topology
between artifacts: which file points at which container, which zone is
served by which CoreDNS instance, and which metric labels the dashboard
expects to see. Each "entity" below is a configuration object, not a
record in a store.

---

## Entities

### DemoStack

The set of services launched by `demo/docker-compose.yml`.

| Field | Source of truth | Description |
|---|---|---|
| `services` | `docker-compose.yml` | exporter, prometheus, grafana, coredns-root, coredns-healthy, coredns-soa-serial-mismatch-a, coredns-soa-serial-mismatch-b, coredns-lame-nameserver, coredns-ns-mismatch |
| `network` | `docker-compose.yml` (default network) | A user-defined bridge network with a fixed IPv4 subnet so static IPs can be assigned to coredns containers |
| `host_ports` | `.env` (with `.env.example` template) | `GRAFANA_PORT`, `PROMETHEUS_PORT`, `EXPORTER_PORT` |

### DemoZone

A DNS zone served inside the stack, referenced by both the exporter
config and the relevant CoreDNS instance(s).

| Zone | Intended state | Served by | Configured in exporter | Notes |
|---|---|---|---|---|
| `healthy.demo.` | healthy across all checks | `coredns-healthy` (advertised as `ns1.healthy.demo.` and `ns2.healthy.demo.`, both in-bailiwick with explicit A glue) | yes | Control case for the dashboard. In-bailiwick NSs are required so the glue prober's self-side A lookup succeeds (see Phase 8 / T061). |
| `soa-serial-mismatch.demo.` | divergent SOA serial across nameservers | `coredns-soa-serial-mismatch-a` (serial=100), `coredns-soa-serial-mismatch-b` (serial=101) | yes | SOA prober's "All NSs report same SOA serial" status check flags this |
| `missing-glue.demo.` | parent NS without A glue, child NS hostname unresolvable | parent (`coredns-root`) only — no auth container | yes | Delegation walk fails for this zone; no metrics emitted at all, so the zone does NOT appear in the `$zone` selector in Grafana — the absence is itself the failure signal |
| `lame-nameserver.demo.` | parent delegates to a server that does not authoritatively serve the zone (renamed from the original `recursive.demo.` in Phase 8 — CoreDNS's `forward` plugin doesn't reliably set RA on referrals, so the actual demonstrable failure is "auth NS that returns no SOA" rather than "RA=1 advertised") | `coredns-lame-nameserver` (a CoreDNS forwarder, not authoritative for the zone) | yes | SOA check fails: `dnshealth_query_success{check="soa",...}=0` |
| `ns-mismatch.demo.` | parent advertises one NS hostname (`ns1.ns-mismatch.demo.`); the auth server's zone file lists two different NS hostnames (`ns-internal-a.ns-mismatch.demo.`, `ns-internal-b.ns-mismatch.demo.`) | `coredns-ns-mismatch` (single container that handles both the parent-advertised NS and the self-reported NSs at the same IP) | yes | Glue prober emits `dnshealth_ns_record{source="parent"}` (1 row) and `dnshealth_ns_record{source="self"}` (2 rows). Dashboard's "Parent and self report same NS records" status check flags the divergence; Records tables show the differing hostnames side-by-side. |

State transitions: none. The intended state is fixed across runs (per
SC-004).

### ExporterConfig (demo instance)

The `dnshealth.yml` shipped with the demo.

| Field | Value | Why |
|---|---|---|
| `zones` | `["healthy.demo.", "soa-serial-mismatch.demo.", "missing-glue.demo.", "lame-nameserver.demo.", "ns-mismatch.demo."]` | The five DemoZone entries above |
| `probe_interval` | `15s` | Tighter than 60s production default to satisfy SC-003 |
| `query_timeout` | `2s` | Shorter than 5s default — local network is fast; faster failure surfaces unhealthy cases sooner |
| `zone_deadline` | `15s` | Matches probe interval — no point letting a zone deadline overrun the next cycle |
| `root_servers` | `["coredns-root:53"]` | Points the delegation walk at the in-stack fake root (R-1) |
| `address_overrides` | `{}` | Not needed when nameservers are reachable on port 53 inside the network |

### PrometheusConfig (demo instance)

| Field | Value |
|---|---|
| `global.scrape_interval` | `5s` |
| `scrape_configs[0].job_name` | `dnshealth_exporter` |
| `scrape_configs[0].static_configs[0].targets` | `["exporter:9266"]` |

### GrafanaProvisioning

| File | Purpose |
|---|---|
| `provisioning/datasources/prometheus.yml` | Defines a Prometheus data source named `Prometheus` at `http://prometheus:9090`, marked as default |
| `provisioning/dashboards/dashboards.yml` | Loader: provider name `dnshealth`, points at `/var/lib/grafana/dashboards`, `updateIntervalSeconds: 10` so file edits picked up live |
| `dashboards/dnshealth-overview.json` | The demo dashboard — see DashboardPanel below |

### DashboardPanel

The dashboard is laid out as a per-zone intodns-style report card with
a `$zone` Grafana templating variable populated from
`label_values(dnshealth_query_success, zone)`. The current panel set
(after Phase 7 / 8 / 9 iterations) is:

**Top row — markdown header** with description, demo zone catalogue, link
to `demo/README.md`.

**Status row** (3 boolean tables, value mappings 0→FAIL/red, 1→PASS/green):

| Panel | Status checks |
|---|---|
| Parent — status | "Parent has NS records for the zone" |
| NS — status | "Multiple authoritative nameservers (≥2)", "All NSs answered SOA authoritatively", "No NS advertises recursion (RA=0)", "Parent and self report same NS records" |
| SOA — status | "All NSs report same SOA serial" (guarded against false PASS when no SOA data exists) |

**Records row** (3 raw-data tables, no PASS/FAIL colouring on data columns):

| Panel | Source | Columns |
|---|---|---|
| NS records — from parent | `dnshealth_ns_record{source="parent",zone="$zone"}` | Nameserver, Glue IP (empty rendered as "(not provided)") |
| NS records — from the zone | outer-join of `dnshealth_ns_record{source="self"}`, `dnshealth_query_success{check="soa"}`, `dnshealth_ns_recursion_available`, filtered to rows where the self-side ns_record is present | Nameserver, IP, Responded (yes/no), Recursion (no/RA=1). For NSs only present on the self side (e.g. `ns-mismatch.demo.`'s `ns-internal-a/b`), Responded and Recursion are empty because the exporter only probes parent-listed NSs. |
| SOA — per-nameserver values | 5 instant queries (`dnshealth_soa_serial`, `_refresh_seconds`, `_retry_seconds`, `_expire_seconds`, `_minimum_seconds`) joined by `nameserver` | Nameserver, IP, Serial, Refresh (s), Retry (s), Expire (s), Min TTL (s) |

**Operator / debug row** — Grafana `row` panel with `collapsed: true`,
containing 4 time-series panels. SOA-serials and Query-duration panels
filter by `${zone}` so they follow the Zone selector; Probe-cycle and
Query-rate panels are global (no zone label):

| Panel | Query | Notes |
|---|---|---|
| Probe cycle duration | `dnshealth_probe_cycle_duration_seconds` | Global |
| Query rate and delegation cache hit ratio | `rate(dnshealth_dns_queries_total[1m])` + `rate(cache_hits)/clamp_min((rate(hits)+rate(misses)),1e-9)` | Global; cache ratio rendered as `percentunit` on right axis |
| SOA serials per nameserver over time — `${zone}` | `dnshealth_soa_serial{zone="$zone"}` | Per-zone |
| Query duration (per check / nameserver) — `${zone}` | `dnshealth_query_duration_seconds{zone="$zone"}` | Per-zone |

---

## Relationships

```text
DemoStack
  ├── exporter ──── reads ───→ ExporterConfig ── targets ──→ DemoZone[]
  │                                                  │
  │                                                  └── served by ──→ CoreDNS containers
  │
  ├── prometheus ── reads ───→ PrometheusConfig ── scrapes ──→ exporter
  │
  └── grafana ──── reads ───→ GrafanaProvisioning ── queries ──→ prometheus
                                                       │
                                                       └── via ──→ DashboardPanel queries
```

The dashboard depends on metric series produced by the exporter; the
contract is captured separately in
`contracts/dashboard-metrics.md` so that any future change to exporter
metric names or labels has a single place to look up "will this break
the demo dashboard?".
