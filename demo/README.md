# dnshealth_exporter — demo deployment

A self-contained Docker Compose stack that runs `dnshealth_exporter`
against an in-stack DNS hierarchy with deliberately healthy and
(soon) deliberately broken zones, scraped by Prometheus and surfaced
in a pre-imported Grafana dashboard.

> **Not for production.** This deployment uses unauthenticated
> Grafana, in-stack CoreDNS without DNSSEC, no persistent storage,
> and no TLS. Do not use the configuration here as a basis for a
> production deployment.

## Prerequisites

- Docker Engine and the Docker Compose plugin (Compose v2). Test with
  `docker compose version`.
- About 1 GB of disk space for the Grafana, Prometheus, and CoreDNS
  images.
- A local checkout of this repository.

You do **not** need Go installed — the exporter is built inside a
container. You do **not** need port 53 free on your host — CoreDNS
containers are only reachable on the internal Docker network.

## Start

```sh
cd demo
docker compose up -d --build
```

First run pulls images (Grafana, Prometheus, CoreDNS) and builds the
exporter — allow a few minutes. Subsequent runs start in well under
60 seconds because images are cached.

## What you'll see

| URL | What |
| --- | --- |
| <http://localhost:3000> | Grafana — the **DNS Health Overview** dashboard loads automatically with anonymous Editor access (no login). |
| <http://localhost:9090> | Prometheus — useful for poking metric queries directly. |
| <http://localhost:9266/metrics> | The exporter's `/metrics` endpoint — useful for grepping series. |

After the stack is up, allow one probe cycle (~15 seconds) for data
to appear.

### Demo zones

| Zone | Intended state | What the dashboard shows |
| --- | --- | --- |
| `healthy.demo.` | Healthy across all checks | All checks green; one SOA serial line; recursion=0 |
| `soa-serial-mismatch.demo.` | Two primaries with divergent SOA serials (100 vs 101) | "All NSs report same SOA serial" = FAIL; SOA per-NS table shows two distinct serial values |
| `missing-glue.demo.` | Parent NS without A glue, hostname unresolvable | No metrics for this zone — delegation walk fails entirely. Zone won't appear in the dashboard's `$zone` selector (the absence is the signal). |
| `lame-nameserver.demo.` | "Authoritative" server is actually a misconfigured forwarder with no real authoritative chain | SOA check fails (`query_success{check="soa"}=0`) for this zone. (CoreDNS's `forward` plugin does not set RA on referral responses, so the recursion-available metric reads 0 — the dashboard's recursion panel is still useful for real-world deployments where the exporter is pointed at actual recursive resolvers.) |
| `ns-mismatch.demo.` | Parent advertises 1 NS; the auth server reports 2 different NSs internally | "Parent and self report same NS records" = FAIL. The Parent records table shows the parent's view (1 NS); the per-NS SOA table populates from the self view (2 NSs). |

## Override host ports

Copy `.env.example` to `.env` and edit any value that conflicts with
something already running on your host:

```sh
cp .env.example .env
$EDITOR .env
docker compose up -d
```

The `${VAR:-default}` syntax in `docker-compose.yml` falls back to the
default when the variable is unset, so leaving any line commented out
is safe.

## Stop and tear down

```sh
docker compose down -v
```

The `-v` flag removes any anonymous volumes Compose created. All demo
state is gone; the next `up` produces an identical demo.

## Iterate on exporter code

Edit Go source in the parent directory, then:

```sh
cd demo
docker compose up -d --build exporter
```

Only the `exporter` container is rebuilt and restarted. Prometheus,
Grafana, and the CoreDNS containers keep running. New metrics appear
within ~20 seconds (one probe cycle plus a scrape interval).

## Iterate on the dashboard

The dashboard JSON at `demo/grafana/dashboards/dnshealth-overview.json`
is the source of truth — it is mounted read-only into Grafana. UI
edits are reverted on restart. To persist a change:

1. Open the dashboard in Grafana, click the gear icon, edit panels.
2. **Share → Export → Save to file**.
3. Replace `demo/grafana/dashboards/dnshealth-overview.json` with the
   downloaded file.
4. Commit.

Grafana re-reads the JSON every 10 seconds, so a fresh `cp` over the
file shows up without a restart.

## Iterate on demo zones

CoreDNS zone files live under `demo/coredns/<container>/zones/`.
After editing a zone file, restart only the affected container:

```sh
docker compose restart coredns-healthy
```

(Substitute the appropriate container name.)

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `bind: address already in use` on `up` | Host port conflict on 3000/9090/9266 | Copy `.env.example` to `.env`, change the port, retry |
| Dashboard panels say "No data" | Less than one probe cycle (~15s) has elapsed | Wait and refresh |
| Still "No data" after a minute | Exporter container failed | `docker compose logs exporter` |
| Metrics for a zone are missing entirely | Delegation walk failed for that zone | `docker compose logs coredns-root` |
