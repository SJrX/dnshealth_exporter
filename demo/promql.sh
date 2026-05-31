#!/usr/bin/env bash
# demo/promql.sh — ad-hoc PromQL inspection against the running demo
# Prometheus. A thin, dependency-light wrapper so interactive "what does
# this query / dashboard row evaluate to right now?" checks are a single
# reusable command instead of bespoke `curl ... | python3 -c ...`
# one-liners. The committed promql_live go test (demo/dashboard/
# promql_live_test.go) remains the authoritative regression gate — this
# is for eyeballing live state while iterating.
#
# Requires: the demo stack up (cd demo && docker compose up -d --build),
# curl, and python3 (stdlib only, for URL-encoding + JSON parsing).
#
# Prometheus URL: $PROM_URL, else http://localhost:${PROMETHEUS_PORT:-9090}.
#
# Usage:
#   demo/promql.sh query '<promql>'
#       Run an instant query; print each result series as "<labels>  <value>".
#
#   demo/promql.sh state '<predicate-with-$zone>' [zone ...]
#       Evaluate a four-state dashboard predicate (the kind composeStatusExpr
#       emits, value in {0,1,2,3}) per zone, printing FAIL/PASS/N/A/WARN.
#       Substitutes $zone in the predicate. With no zones, uses every zone
#       in demo/exporter/dnshealth.yml.
#
#   demo/promql.sh zones
#       List the configured demo zones.
#
# Examples:
#   demo/promql.sh query 'dnshealth_mx_count'
#   demo/promql.sh query 'dnshealth_soa_serial{zone="healthy.demo."}'
#   demo/promql.sh state 'absent(dnshealth_soa_serial{zone="$zone"})' lame-nameserver.demo.

set -euo pipefail

cd "$(dirname "$0")"

PROM_URL="${PROM_URL:-http://localhost:${PROMETHEUS_PORT:-9090}}"
ZONES_FILE="exporter/dnshealth.yml"

die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# urlencode <string> — percent-encode via python3 stdlib.
urlencode() { python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.argv[1]))' "$1"; }

# zones — the configured demo zones, parsed from the YAML list.
list_zones() {
    awk '
        /^zones:/ { in_zones = 1; next }
        in_zones && /^[[:space:]]*-[[:space:]]/ { sub(/^[[:space:]]*-[[:space:]]*/, ""); print; next }
        in_zones && /^[^[:space:]#]/ { exit }
    ' "${ZONES_FILE}"
}

# api_query <promql> — POST-style instant query, returns the raw JSON body.
api_query() {
    curl -fsS "${PROM_URL}/api/v1/query" --data-urlencode "query=$1"
}

# cmd_query <promql> — print one line per result series: "<sorted labels>  <value>".
cmd_query() {
    [ $# -ge 1 ] || die "query needs a PromQL argument"
    api_query "$1" | python3 -c '
import json, sys
d = json.load(sys.stdin)
if d.get("status") != "success":
    print("query failed:", d.get("error", d.get("status")), file=sys.stderr); sys.exit(1)
res = d["data"]["result"]
if not res:
    print("(no series)"); sys.exit(0)
for s in sorted(res, key=lambda x: sorted(x["metric"].items())):
    m = s["metric"]
    name = m.pop("__name__", "")
    labels = ",".join("%s=%s" % (k, v) for k, v in sorted(m.items()))
    val = s["value"][1]
    print("%s{%s}  %s" % (name, labels, val))
'
}

# cmd_state <predicate> [zone ...] — four-state interpretation per zone.
cmd_state() {
    [ $# -ge 1 ] || die "state needs a predicate argument (use \$zone as the placeholder)"
    local predicate="$1"; shift
    local zones=("$@")
    if [ ${#zones[@]} -eq 0 ]; then
        # shellcheck disable=SC2207
        zones=($(list_zones))
    fi
    local z q
    for z in "${zones[@]}"; do
        q="${predicate//\$zone/$z}"
        api_query "$q" | ZONE="$z" python3 -c '
import json, os, sys
d = json.load(sys.stdin)
z = os.environ["ZONE"]
NAMES = {0: "FAIL", 1: "PASS", 2: "N/A", 3: "WARN"}
if d.get("status") != "success":
    print("%-30s ERROR %s" % (z, d.get("error", d.get("status")))); sys.exit(0)
res = d["data"]["result"]
if len(res) != 1:
    print("%-30s %s (expected 1 sample, got %d)" % (z, "EMPTY" if not res else "MULTI", len(res))); sys.exit(0)
v = float(res[0]["value"][1])
iv = round(v)
label = NAMES.get(iv, "?%s" % v) if abs(v - iv) < 1e-9 else "?%s" % v
print("%-30s %s" % (z, label))
'
    done
}

[ $# -ge 1 ] || die "usage: promql.sh {query|state|zones} ... (see header)"
sub="$1"; shift
case "$sub" in
    query) cmd_query "$@" ;;
    state) cmd_state "$@" ;;
    zones) list_zones ;;
    *)     die "unknown subcommand '${sub}' (want query|state|zones)" ;;
esac
