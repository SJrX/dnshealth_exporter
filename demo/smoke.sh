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

EXPORTER_PORT="${EXPORTER_PORT:-9053}"
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

echo "A4b: v6-only.demo. surfaces per-NS metric series with v6 IPs (spec 006 SC-009)"
# Match an `ip` label whose value contains a colon — discriminator for
# IPv6 textual form. Pre-spec-006 this zone produced no per-NS series.
grep -E '^dnshealth_ns_record\{[^}]*\} 1$' "${METRICS_FILE}" | grep -F 'zone="v6-only.demo."' | grep -E 'ip="[0-9a-f:]+:[0-9a-f:]+"' >/dev/null \
    || fail "v6-only.demo. has no dnshealth_ns_record series with an IPv6 ip label"
# Also confirm the healthy zone now has v6 entries (dual-stack pattern).
grep -E '^dnshealth_ns_record\{[^}]*\} 1$' "${METRICS_FILE}" | grep -F 'zone="healthy.demo."' | grep -E 'ip="[0-9a-f:]+:[0-9a-f:]+"' >/dev/null \
    || fail "healthy.demo. dual-stack: no dnshealth_ns_record series with an IPv6 ip label"

echo "A4c: dnshealth_parent_delegation surfaces successful delegations"
# Every demo zone configured in dnshealth.yml has at least an NS entry
# in the parent zone file (demo/coredns/root/zones/demo.zone), so
# WalkDelegation succeeds for all of them and the gauge reads 1.
# missing-glue.demo. is the closest to a "broken" case in the demo,
# but its parent referral DOES contain NS records (just no glue), so
# WalkDelegation still returns a valid DelegationResult — the failure
# is downstream (no resolvable IPs → "no nameservers found" warning).
# The 0-value path of this gauge fires when the parent has no NS RR
# set for the zone at all (an entirely undelegated zone in the config).
# No demo zone currently exercises that path; the gauge's failure
# branch is structurally correct but visually unverified here.
grep -E '^dnshealth_parent_delegation\{zone="healthy\.demo\."\} 1$' "${METRICS_FILE}" >/dev/null \
    || fail "dnshealth_parent_delegation for healthy.demo. is not 1"
grep -E '^dnshealth_parent_delegation\{zone="v6-only\.demo\."\} 1$' "${METRICS_FILE}" >/dev/null \
    || fail "dnshealth_parent_delegation for v6-only.demo. is not 1"

echo "A4d: dnshealth_soa_mname metrics surface MNAME validity"
# Happy path — healthy.demo.'s SOA MNAME is ns1.healthy.demo., which
# IS in the NS RR set and resolves (A + AAAA glue both present).
grep -E '^dnshealth_soa_mname\{[^}]+\} 1$' "${METRICS_FILE}" \
    | grep -F 'zone="healthy.demo."' \
    | grep -F 'mname="ns1.healthy.demo."' >/dev/null \
    || fail "dnshealth_soa_mname for healthy.demo. missing or mname label wrong"
grep -E '^dnshealth_soa_mname_in_ns_set\{[^}]+\} 1$' "${METRICS_FILE}" \
    | grep -F 'zone="healthy.demo."' >/dev/null \
    || fail "dnshealth_soa_mname_in_ns_set for healthy.demo. is not 1"
grep -E '^dnshealth_soa_mname_resolves\{[^}]+\} 1$' "${METRICS_FILE}" \
    | grep -F 'zone="healthy.demo."' >/dev/null \
    || fail "dnshealth_soa_mname_resolves for healthy.demo. is not 1"

# Failure path — ns-mismatch.demo.'s SOA MNAME is
# ns-internal-a.ns-mismatch.demo., which is NOT in the parent's NS RR
# set (parent advertises only ns1.ns-mismatch.demo.). The hostname
# itself resolves through the auth server, so _resolves is 1 but
# _in_ns_set is 0 — exercises the "MNAME mismatch" alert path.
grep -E '^dnshealth_soa_mname_in_ns_set\{[^}]+\} 0$' "${METRICS_FILE}" \
    | grep -F 'zone="ns-mismatch.demo."' >/dev/null \
    || fail "dnshealth_soa_mname_in_ns_set for ns-mismatch.demo. is not 0 (failure path)"

echo "A4e: dnshealth_ns_hostname_* surfaces per-NS hostname validity"
# healthy.demo.'s NSs (ns1.healthy.demo., ns2.healthy.demo.) are
# valid LDH and not CNAMEs, so the happy path is all-1 / all-0.
# The 0-side of each metric (invalid syntax, is-a-CNAME) has no
# corresponding demo zone — exercised by the integration tests
# instead, since adding malformed NSs to coredns is finicky.
grep -E '^dnshealth_ns_hostname_syntax_valid\{[^}]+\} 1$' "${METRICS_FILE}" \
    | grep -F 'zone="healthy.demo."' | grep -F 'nameserver="ns1.healthy.demo."' >/dev/null \
    || fail "dnshealth_ns_hostname_syntax_valid for healthy.demo. / ns1 is not 1"
grep -E '^dnshealth_ns_hostname_is_cname\{[^}]+\} 0$' "${METRICS_FILE}" \
    | grep -F 'zone="healthy.demo."' | grep -F 'nameserver="ns1.healthy.demo."' >/dev/null \
    || fail "dnshealth_ns_hostname_is_cname for healthy.demo. / ns1 is not 0"

echo "A4f: dup-glue.demo. probes normally despite duplicated parent glue (issue #26)"
# Parent referral for dup-glue.demo. lists the same A glue record
# twice for ns1.dup-glue.demo. (deliberate, per
# demo/coredns/root/zones/demo.zone). Pre-fix this inflated
# dnshealth_dns_queries_total{server="172.31.0.17"} via duplicate
# ProbeResults flowing out of extractDelegation; post-fix the
# slices.Contains gate keeps the glueMap honest. Smoke asserts the
# zone is being probed normally — exact counter-magnitude is the
# integration test's job (cycle-timing makes a tight smoke
# assertion racy), but a single parent NS-record series for the
# (host, IP) tuple confirms the dedup is in effect.
grep -E '^dnshealth_ns_record\{[^}]+\} 1$' "${METRICS_FILE}" \
    | grep -F 'zone="dup-glue.demo."' \
    | grep -F 'nameserver="ns1.dup-glue.demo."' \
    | grep -F 'ip="172.31.0.17"' \
    | grep -F 'source="parent"' >/dev/null \
    || fail "dup-glue.demo. parent-side ns_record series missing"
# Counter exists for the dup-glue IP (proves the zone was probed).
grep -E '^dnshealth_dns_queries_total\{server="172\.31\.0\.17"\} [1-9]' "${METRICS_FILE}" >/dev/null \
    || fail "dnshealth_dns_queries_total for 172.31.0.17 (dup-glue) is missing or zero"

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
