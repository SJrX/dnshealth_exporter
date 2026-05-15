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
| `services` | `docker-compose.yml` | exporter, prometheus, grafana, coredns-root, coredns-healthy, coredns-broken-soa-a, coredns-broken-soa-b, coredns-recursive |
| `network` | `docker-compose.yml` (default network) | A user-defined bridge network with a fixed IPv4 subnet so static IPs can be assigned to coredns containers |
| `host_ports` | `.env` (with `.env.example` template) | `GRAFANA_PORT`, `PROMETHEUS_PORT`, `EXPORTER_PORT` |

### DemoZone

A DNS zone served inside the stack, referenced by both the exporter
config and the relevant CoreDNS instance(s).

| Zone | Intended state | Served by | Configured in exporter | Notes |
|---|---|---|---|---|
| `healthy.demo.` | healthy across all checks | `coredns-healthy` (advertised as `ns1.demo.` and `ns2.demo.`) | yes | Control case for the dashboard |
| `broken-soa.demo.` | divergent SOA serial across nameservers | `coredns-broken-soa-a` (serial=100), `coredns-broken-soa-b` (serial=101) | yes | SOA prober flags this |
| `missing-glue.demo.` | parent NS without A glue, child NS hostname unresolvable | parent (`coredns-root`) only — no auth container | yes | Glue prober / delegation walk fails for this zone |
| `recursive.demo.` | RA flag returned (recursion offered by an "authoritative") | `coredns-recursive` | yes | Recursion prober flags RA=1 |

State transitions: none. The intended state is fixed across runs (per
SC-004).

### ExporterConfig (demo instance)

The `dnshealth.yml` shipped with the demo.

| Field | Value | Why |
|---|---|---|
| `zones` | `["healthy.demo.", "broken-soa.demo.", "missing-glue.demo.", "recursive.demo."]` | The four DemoZone entries above |
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

A single panel in the demo dashboard. The minimum panel set for v1:

| Panel title | Query (PromQL) | Visualization | Why |
|---|---|---|---|
| Zone health overview | `dnshealth_query_success` | Stat with `Last *` reducer, grouped by `zone`, color thresholds (1=green, 0=red) | One-glance "which zones are healthy" |
| SOA serials per zone | `dnshealth_soa_serial` | Time series, legend `{{zone}} / {{nameserver}}` | Surfaces the divergent-serial broken zone visually |
| Recursion availability | `dnshealth_recursion_available` | Stat per zone, threshold (0=green, 1=red) | Flags the recursive demo zone |
| Query duration | `dnshealth_query_duration_seconds` | Time series by `zone`, `check` | Useful for noticing slow queries |
| Probe cycle duration | `dnshealth_probe_cycle_duration_seconds` | Time series | Operator visibility into the exporter itself |
| Cycle queries / cache hit ratio | `rate(dnshealth_dns_queries_total[1m])` and `rate(dnshealth_delegation_cache_hits_total[5m]) / (rate(dnshealth_delegation_cache_hits_total[5m]) + rate(dnshealth_delegation_cache_misses_total[5m]))` | Time series | Shows the cache + traffic shape; useful when iterating on cache changes |

The dashboard MUST also include a markdown panel at the top with: a one-
line description, a link to the demo README, and the list of zones with
their intended states. This makes the dashboard self-explanatory for a
new viewer.

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
