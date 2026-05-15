#!/usr/bin/env bash
# demo/smoke.sh — end-to-end validation of the demo deployment.
#
# Brings the stack up, waits for the first probe cycle, asserts the
# expected metric series for healthy and broken zones, then tears
# down. Implements the contract documented at
# specs/004-demo-deployment/contracts/smoke-test.md.
#
# Exit codes:
#   0  all assertions passed; teardown clean
#   1  one or more assertions failed
#   2  stack failed to come up within timeout
#   3  exporter container did not exit with code 0 in response to SIGTERM
#
# Usage:
#   cd demo && ./smoke.sh

set -euo pipefail

cd "$(dirname "$0")"

EXPORTER_PORT="${EXPORTER_PORT:-9266}"
METRICS_URL="http://localhost:${EXPORTER_PORT}/metrics"
METRICS_FILE="$(mktemp -t dnshealth-smoke.metrics.XXXXXX)"
trap 'rm -f "${METRICS_FILE}"' EXIT

step() { printf '\n=== %s ===\n' "$*"; }
fail() { printf '\nASSERTION FAILED: %s\n' "$*" >&2; printf '\n--- last 50 lines of compose logs ---\n' >&2; docker compose logs --tail 50 >&2 || true; docker compose down -v >/dev/null 2>&1 || true; exit 1; }

step "Step 1: bring up the stack"
docker compose up -d --build

step "Step 2: wait for /metrics to respond (90s timeout)"
DEADLINE=$(( $(date +%s) + 90 ))
until curl -fsS "${METRICS_URL}" >/dev/null 2>&1; do
    if [ "$(date +%s)" -gt "${DEADLINE}" ]; then
        printf 'ERROR: /metrics did not respond within 90s\n' >&2
        docker compose logs --tail 50 >&2 || true
        docker compose down -v >/dev/null 2>&1 || true
        exit 2
    fi
    sleep 2
done
echo "metrics endpoint ready"

step "Step 3: wait one full probe cycle (25s)"
sleep 25

step "Step 4: capture metrics"
curl -fsS "${METRICS_URL}" > "${METRICS_FILE}"
echo "captured $(wc -l < "${METRICS_FILE}") metric lines"

step "Step 5: run assertions"

# Label matching uses piped greps because Prometheus emits labels in
# alphabetical order, which would otherwise force the regex to know
# the order. Each `grep -F` matches one fragment regardless of position.

echo "A1: healthy zone reports success across all checks"
grep -E '^dnshealth_query_success\{.*\} 1$' "${METRICS_FILE}" | grep -F 'zone="healthy.demo."' | grep -F 'check="soa"' >/dev/null \
    || fail "healthy.demo. SOA check not reporting success"
grep -E '^dnshealth_query_success\{.*\} 1$' "${METRICS_FILE}" | grep -F 'zone="healthy.demo."' | grep -F 'check="recursion"' >/dev/null \
    || fail "healthy.demo. recursion check not reporting success"

echo "A2: soa-serial-mismatch zone surfaces both serials (100 and 101)"
grep -E '^dnshealth_soa_serial\{.*\} 100$' "${METRICS_FILE}" | grep -F 'zone="soa-serial-mismatch.demo."' >/dev/null \
    || fail "soa-serial-mismatch.demo. SOA serial 100 not present"
grep -E '^dnshealth_soa_serial\{.*\} 101$' "${METRICS_FILE}" | grep -F 'zone="soa-serial-mismatch.demo."' >/dev/null \
    || fail "soa-serial-mismatch.demo. SOA serial 101 not present"

echo "A3: lame-nameserver zone surfaces SOA failure (forwarder limitation; see contract A3 note)"
grep -E '^dnshealth_query_success\{.*\} 0$' "${METRICS_FILE}" | grep -F 'zone="lame-nameserver.demo."' | grep -F 'check="soa"' >/dev/null \
    || fail "lame-nameserver.demo. SOA check not reporting failure"

echo "A3b: ns-mismatch zone surfaces parent vs self NS record divergence"
PARENT_COUNT=$(grep -E '^dnshealth_ns_record\{.*\} 1$' "${METRICS_FILE}" | grep -F 'zone="ns-mismatch.demo."' | grep -F 'source="parent"' | wc -l)
SELF_COUNT=$(grep -E '^dnshealth_ns_record\{.*\} 1$' "${METRICS_FILE}" | grep -F 'zone="ns-mismatch.demo."' | grep -F 'source="self"' | wc -l)
[ "${PARENT_COUNT}" -eq 1 ] && [ "${SELF_COUNT}" -ge 2 ] \
    || fail "ns-mismatch.demo.: expected parent=1 self>=2 NS records, got parent=${PARENT_COUNT} self=${SELF_COUNT}"

echo "A4: probe cycle ran and produced query counts"
grep -E '^dnshealth_probe_cycle_duration_seconds [0-9]' "${METRICS_FILE}" >/dev/null \
    || fail "dnshealth_probe_cycle_duration_seconds not present"
grep -E '^dnshealth_dns_queries_total\{[^}]+\} [1-9][0-9]*$' "${METRICS_FILE}" >/dev/null \
    || fail "no per-server query count above zero"

echo "A5: build info present"
grep -F 'dnshealth_build_info' "${METRICS_FILE}" >/dev/null \
    || fail "dnshealth_build_info not present"

echo "A6: clean shutdown — exporter exits 0 in response to SIGTERM"
EXPORTER_CID=$(docker compose ps -q exporter)
docker compose stop --timeout 10
EXIT_CODE=$(docker inspect --format='{{.State.ExitCode}}' "${EXPORTER_CID}" 2>/dev/null || echo "")
docker compose down -v
if [ "${EXIT_CODE}" != "0" ]; then
    printf 'ERROR: exporter exited with code %q on SIGTERM (expected 0)\n' "${EXIT_CODE}" >&2
    exit 3
fi

step "All assertions passed"
exit 0
