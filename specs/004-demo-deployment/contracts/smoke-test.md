# Smoke Test Contract

`demo/smoke.sh` is a shell script that validates the demo deployment
end-to-end. This file documents what the script does, what it asserts,
and what exit codes mean. The script's behavior MUST match this
contract; if either changes, both update together.

## Steps

1. **Bring up the stack** with `docker compose -f demo/docker-compose.yml up -d --build`.
2. **Wait for `/metrics`** — poll `http://localhost:${EXPORTER_PORT:-9266}/metrics` every 2 seconds, up to 90 seconds, for an HTTP 200.
3. **Wait one probe cycle** — sleep 25 seconds (1.5 × the demo `probe_interval` of 15s) so at least one cycle has produced metrics.
4. **Capture metrics** — `curl -fsS http://localhost:${EXPORTER_PORT:-9266}/metrics > /tmp/dnshealth-smoke.metrics`.
5. **Run assertions** (below).
6. **Tear down** — `docker compose -f demo/docker-compose.yml down -v`.
7. **Print outcome** — overall pass/fail summary.

If any step fails, the script prints the failing assertion, dumps the
last 50 lines of `docker compose logs`, and exits non-zero.

## Assertions

Each assertion is a `grep -F` (or `grep -E` where noted) against the
captured `/metrics` output. The check passes when grep matches at least
one line.

**Important — label ordering**: Prometheus emits labels in
alphabetical order. Earlier drafts of this contract used regexes that
required `zone="..."` to appear before `check="..."` in the line —
that breaks because alphabetical order is `check, ip, nameserver,
zone`. The assertions below use piped greps so the order doesn't
matter.

### A1. Healthy zone reports success

```
grep -E '^dnshealth_query_success\{.*\} 1$' /tmp/dnshealth-smoke.metrics \
    | grep -F 'zone="healthy.demo."' | grep -F 'check="soa"'
grep -E '^dnshealth_query_success\{.*\} 1$' /tmp/dnshealth-smoke.metrics \
    | grep -F 'zone="healthy.demo."' | grep -F 'check="recursion"'
```

### A2. soa-serial-mismatch zone surfaces both serials

```
grep -E '^dnshealth_soa_serial\{.*\} 100$' /tmp/dnshealth-smoke.metrics \
    | grep -F 'zone="soa-serial-mismatch.demo."'
grep -E '^dnshealth_soa_serial\{.*\} 101$' /tmp/dnshealth-smoke.metrics \
    | grep -F 'zone="soa-serial-mismatch.demo."'
```

(Two distinct serial values for the same zone.)

### A3. lame-nameserver zone surfaces SOA failure

The originally-spec'd "RA=1" assertion is not feasible with a pure
CoreDNS demo (CoreDNS's `forward` plugin does not set the RA flag on
referral responses without a real recursive upstream like `unbound`).
The zone (renamed from `recursive.demo.` to `lame-nameserver.demo.`
in Phase 8) instead demonstrates a different real failure: the SOA
query through a server delegated by the parent but not actually
authoritative for the zone returns no useful answer.

```
grep -E '^dnshealth_query_success\{.*\} 0$' /tmp/dnshealth-smoke.metrics \
    | grep -F 'zone="lame-nameserver.demo."' | grep -F 'check="soa"'
```

### A3b. ns-mismatch zone surfaces parent vs self NS divergence

The parent advertises ONE NS (`ns1.ns-mismatch.demo.`) but the auth
server's zone file lists TWO different NSs (`ns-internal-a` and
`ns-internal-b`). The exporter's glue prober emits
`dnshealth_ns_record` with `source="parent"` (1 row) and
`source="self"` (2 rows).

```
PARENT_COUNT=$(grep -E '^dnshealth_ns_record\{.*\} 1$' /tmp/dnshealth-smoke.metrics \
    | grep -F 'zone="ns-mismatch.demo."' | grep -F 'source="parent"' | wc -l)
SELF_COUNT=$(grep -E '^dnshealth_ns_record\{.*\} 1$' /tmp/dnshealth-smoke.metrics \
    | grep -F 'zone="ns-mismatch.demo."' | grep -F 'source="self"' | wc -l)
[ "${PARENT_COUNT}" -eq 1 ] && [ "${SELF_COUNT}" -ge 2 ]
```

### A4. Probe cycle ran

```
grep -E '^dnshealth_probe_cycle_duration_seconds [0-9]' /tmp/dnshealth-smoke.metrics
grep -E '^dnshealth_dns_queries_total\{[^}]+\} [1-9][0-9]*$' /tmp/dnshealth-smoke.metrics
```

(Cycle duration recorded; at least one server has non-zero query count.)

### A5. Build info present

```
grep -F 'dnshealth_build_info' /tmp/dnshealth-smoke.metrics
```

### A6. Clean shutdown

After `docker compose down -v`, the exporter container's last reported
exit code (`docker inspect` before removal, or `docker events` watch)
MUST be 0.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | All assertions passed; teardown clean |
| 1 | One or more assertions failed |
| 2 | Stack failed to come up within timeout |
| 3 | Teardown reported non-zero exit from any service |

## What the smoke test does NOT cover

- Visual rendering of the Grafana dashboard. Validated implicitly by
  asserting the underlying metrics exist; dashboard JSON correctness is
  reviewed manually.
- Behavior under signal handling beyond the SIGTERM that `compose down`
  sends. Production signal-handling tests live in the exporter's own
  test suite, not the demo.
- Performance benchmarking. The demo's measurable outcomes (SC-001,
  SC-003) are time-bounded but not asserted by the smoke script — they
  are validated manually during development.
