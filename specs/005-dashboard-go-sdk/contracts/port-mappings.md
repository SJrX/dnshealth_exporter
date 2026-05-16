# Contract: Demo Port Mappings

**Date**: 2026-05-15
**Type**: Docker Compose host-port bindings.

The demo's `docker-compose.yml` MUST expose the following host-to-
container port mappings, each overridable via the named environment
variable.

| Service    | Container port | Default host port | Override env var   | Was (before this feature) |
|------------|----------------|-------------------|--------------------|---------------------------|
| Exporter   | 9266           | **9053**          | `EXPORTER_PORT`    | 9266 (changed)            |
| Prometheus | 9090           | 9090              | `PROMETHEUS_PORT`  | 9090 (unchanged)          |
| Grafana    | 3000           | 3000              | `GRAFANA_PORT`     | 3000 (unchanged)          |

**Why exporter changes from 9266 → 9053**: Spec FR-012. Production
`dnshealth_exporter` continues to default to 9266; the *demo*
exporter uses 9053 to be DNS-themed (well-known DNS port `53` in
the prober `9xxx` range) and to avoid colliding with operators
who already run a production exporter on the host.

**Why Grafana and Prometheus stay**: Spec FR-013. The override
mechanism is sufficient for collisions; the well-known defaults are
expected by operators.

## Override syntax

Each port is bound via the existing pattern:

```yaml
ports:
  - "${EXPORTER_PORT:-9053}:9266"
```

Operators override per-invocation:

```bash
EXPORTER_PORT=9999 docker compose up -d
```

## Documentation contract

`demo/README.md` MUST document:

- The new exporter URL: `http://localhost:9053/metrics`.
- The three override env vars and their defaults, in a single
  visible table.
- That the *production* exporter default is unchanged (9266); the
  9053 default applies to the demo only.

## Smoke test contract

`demo/smoke.sh` reads `EXPORTER_PORT` from the environment with a
default. The default literal MUST be updated from `9266` to `9053`
to match the compose default. No assertion changes.
