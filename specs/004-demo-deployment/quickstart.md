# Quickstart: Demo Deployment

This document is the developer-facing workflow for the demo. Once the
feature is implemented, the same content (with minor edits) becomes
`demo/README.md`. While the spec is in flight, this file lives under
`specs/004-demo-deployment/` for review.

---

## Prerequisites

- Docker Engine (>= 24.x recommended) and the Docker Compose plugin
  (Compose v2). Test with `docker compose version`.
- About 1 GB of disk space for the images (Grafana, Prometheus,
  CoreDNS, distroless).
- A local checkout of this repository.

The demo does **not** require Go to be installed — the exporter is
built inside a container.

The demo does **not** require port 53 to be free on the host —
CoreDNS containers are reachable only over the internal Docker
network.

## Start the demo

```sh
cd demo
docker compose up -d --build
```

First run pulls images (Grafana, Prometheus, CoreDNS) — allow a few
minutes. Subsequent runs start in well under 60 seconds because
images are cached.

## Open the dashboard

| URL | What |
|---|---|
| http://localhost:3000 | Grafana — the demo dashboard "DNS Health Overview" loads with anonymous Editor access (no login). |
| http://localhost:9090 | Prometheus — useful for poking metric queries directly. |
| http://localhost:9266/metrics | The exporter's `/metrics` endpoint — useful for grepping series. |

To override any of these ports (e.g., if the host already uses 3000),
copy `.env.example` to `.env` and edit:

```sh
cp .env.example .env
$EDITOR .env
docker compose up -d
```

## What you'll see

The dashboard pre-loads five demo zones served by in-stack CoreDNS, picked via the **Zone** templating selector at the top of the dashboard:

| Zone | Intended state | What the dashboard shows |
|---|---|---|
| `healthy.demo.` | healthy | All status checks green; both NS records tables show identical hostnames; single SOA serial in the per-NS table |
| `soa-serial-mismatch.demo.` | divergent SOA serials between primaries (100 vs 101) | SOA status row "All NSs report same SOA serial" = FAIL; SOA per-NS table shows two distinct serial values; Operator-section "SOA serials over time" line splits |
| `missing-glue.demo.` | parent NS without A glue, hostname unresolvable | Delegation walk fails entirely; zone does NOT appear in the `$zone` selector (no `dnshealth_query_success` series exists for it — the absence is the signal) |
| `lame-nameserver.demo.` | parent delegates to a CoreDNS forwarder that is not authoritative for the zone (originally intended as the "RA=1 advertised" demo, but CoreDNS's `forward` plugin doesn't reliably set RA — the visible failure is "auth NS returns no SOA") | NS status row "All NSs answered SOA authoritatively" = FAIL; "NS records — from the zone" panel is empty (no self response) |
| `ns-mismatch.demo.` | parent advertises 1 NS hostname; the auth server reports 2 different hostnames internally | NS status row "Parent and self report same NS records" = FAIL; "NS records — from parent" shows `ns1.ns-mismatch.demo.`; "NS records — from the zone" shows `ns-internal-a` and `ns-internal-b` with empty Responded/Recursion (the exporter only probes parent-listed NSs) |

After starting, allow one probe cycle (15 seconds) for data to appear.

## Iterate on exporter code

```sh
# Edit Go source in the parent directory, then:
cd demo
docker compose up -d --build exporter
```

This rebuilds and restarts only the exporter container; Prometheus,
Grafana, and the CoreDNS containers keep running. New metrics appear
within ~20 seconds (probe cycle + scrape interval).

## Iterate on the dashboard

1. Edit panels interactively in the Grafana UI (anonymous Editor mode
   — no login required).
2. **Share → Export → Save to file** to download the modified
   dashboard JSON.
3. Replace `demo/grafana/dashboards/dnshealth-overview.json` with the
   downloaded file.
4. Commit. The next `docker compose up` will load your version.

In-UI changes are intentionally **not persisted** across restarts —
the JSON file under version control is the source of truth.

## Iterate on demo zones

CoreDNS zone files live under `demo/coredns/<container>/zones/`.
After editing a zone file, restart only the affected CoreDNS
container:

```sh
docker compose restart coredns-healthy
```

(Substitute the appropriate container name.)

## Stop and tear down

```sh
docker compose down -v
```

The `-v` flag removes any anonymous volumes Compose created.
All demo state is gone; the next `up` produces an identical demo.

## Validate the demo end-to-end

A scripted smoke test is provided:

```sh
./smoke.sh
```

It brings the stack up, waits for the first probe cycle, asserts the
expected metric series for both healthy and broken zones, and tears
down. Exits 0 on success. See
`../specs/004-demo-deployment/contracts/smoke-test.md` for the full
list of assertions.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `bind: address already in use` on `up` | Host port conflict | Copy `.env.example` to `.env`, change the conflicting port, retry |
| Dashboard panels say "No data" | Less than one probe cycle has elapsed | Wait 15–20 seconds and refresh |
| Dashboard still says "No data" after a minute | Exporter container failed; check logs | `docker compose logs exporter` |
| Metrics for a zone are entirely missing | Delegation walk failed (expected for `missing-glue.demo.`); for other zones check `coredns-root` logs | `docker compose logs coredns-root` |

## Not for production

This deployment is for evaluation and development only. It uses
unauthenticated Grafana, in-stack CoreDNS without DNSSEC, no
persistent storage, and no TLS. Do not use the configuration in this
directory as the basis for a production deployment.
