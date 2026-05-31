//go:build promql_live

// Package main's promql_live test evaluates every status-table row's
// PromQL predicate against a LIVE Prometheus and asserts the rendered
// state (FAIL/PASS/N/A/WARN) per demo zone. It is gated behind the
// `promql_live` build tag because it needs the full demo stack running
// (Prometheus scraping the exporter scraping CoreDNS) — smoke.sh invokes
// it after bring-up; it is excluded from `go test ./...` and from
// `go test -tags=integration ./...`.
//
// Why this exists (issue #46): dashboard predicates are generated Go
// strings that no other test evaluates. The drift test only checks the
// committed JSON matches the generator; the unit tests only check string
// composition. Bugs where a predicate returns the wrong value, an empty
// vector (blank row), or short-circuits (spec 008 audit D-4 / D-9) are
// invisible to `go test` and were historically caught only by a human
// reading Grafana. This test closes that gap by querying
// /api/v1/query with the EXACT predicate the dashboard ships (it imports
// composeStatusExpr + the check slices directly; the drift test
// guarantees those equal the committed JSON).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// promURL is where Prometheus' HTTP API lives. smoke.sh runs the stack
// with port 9090 published to the host; override via env for other setups.
func promURL() string {
	if u := os.Getenv("PROMQL_CHECK_URL"); u != "" {
		return u
	}
	return "http://localhost:9090"
}

// demoZones is the set of zones the demo deployment probes (mirrors
// demo/exporter/dnshealth.yml). Hardcoded rather than read from the
// config so that adding a zone is a conscious, reviewed change to the
// expectations below — a new zone that renders a garbage state should
// trip the universal invariant, not be silently skipped.
var demoZones = []string{
	"healthy.demo.",
	"soa-serial-mismatch.demo.",
	"lame-nameserver.demo.",
	"ns-mismatch.demo.",
	"ns-names-mismatch.demo.",
	"v6-only.demo.",
	"dup-glue.demo.",
	"hidden-master.demo.",
	"mx-healthy.demo.",
	"mx-broken.demo.",
	"mx-null.demo.",
	"mx-null-conflict.demo.",
	"missing-glue.demo.",
}

// stateName maps the four-state numeric value to its label.
var stateName = map[int]string{0: "FAIL", 1: "PASS", 2: "N/A", 3: "WARN"}

// panelChecks pairs each status panel with its check slice so the test
// can address rows as (panel, refId).
func panelChecks() []struct {
	name   string
	checks []statusCheck
} {
	return []struct {
		name   string
		checks []statusCheck
	}{
		{"parent", parentStatusChecks},
		{"ns", nsStatusChecks},
		{"soa", soaStatusChecks},
		{"mx", mxStatusChecks},
	}
}

// expectations pins the meaningful cells — the bug-table states from
// issue #46 plus the WARN/N/A states added in #48. Key is
// "panel/refId/zone". Cells NOT listed here are still covered by the
// universal invariant (must render a valid 0/1/2/3 state); only cells
// whose exact value carries regression meaning are pinned.
var expectations = map[string]string{
	// MX happy path — everything green.
	"mx/A/mx-healthy.demo.": "PASS",
	"mx/B/mx-healthy.demo.": "PASS",
	"mx/C/mx-healthy.demo.": "PASS",
	"mx/D/mx-healthy.demo.": "PASS",
	"mx/E/mx-healthy.demo.": "PASS",

	// Null MX — presence PASSes via the Null-MX branch; the per-target
	// rows read N/A (no real targets to resolve / CNAME-check / validate);
	// no conflict.
	"mx/A/mx-null.demo.": "PASS",
	"mx/B/mx-null.demo.": "N/A",
	"mx/C/mx-null.demo.": "N/A",
	"mx/D/mx-null.demo.": "N/A",
	"mx/E/mx-null.demo.": "PASS",

	// Broken zone — a target fails to resolve and a target is a CNAME
	// (bug #3's no-false-FAIL is implicitly covered by the no-MX zones'
	// N/A cells above; here the genuine failures must FAIL).
	"mx/B/mx-broken.demo.": "FAIL",
	"mx/C/mx-broken.demo.": "FAIL",
	"mx/E/mx-broken.demo.": "PASS",

	// Null-MX-conflict zone — the regression cell for spec 008 bug #4:
	// a Null MX RR coexisting with a real MX MUST make row E FAIL. Before
	// the fix (audit D-5/D-9) this predicate was a tautology / short-
	// circuited and read PASS.
	"mx/E/mx-null-conflict.demo.": "FAIL",

	// WARN states from #48: a zone with exactly two nameservers meets the
	// RFC 2182 minimum but warrants a soft warning.
	"ns/A/healthy.demo.": "WARN",
}

// TestDashboardPromQLPredicates is the live PromQL evaluation gate.
func TestDashboardPromQLPredicates(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Fail fast with a clear message if Prometheus isn't reachable —
	// this test is smoke-only and assumes the stack is up.
	if _, err := queryScalar(client, "vector(1)"); err != nil {
		t.Fatalf("Prometheus not reachable at %s: %v\n"+
			"(this test is smoke-only; run via demo/smoke.sh with the stack up, "+
			"or set PROMQL_CHECK_URL)", promURL(), err)
	}

	checked := 0
	for _, p := range panelChecks() {
		for _, c := range p.checks {
			expr := composeStatusExpr(c)
			for _, zone := range demoZones {
				q := strings.ReplaceAll(expr, "$zone", zone)
				key := fmt.Sprintf("%s/%s/%s", p.name, c.refId, zone)

				val, err := queryScalar(client, q)
				if err != nil {
					t.Errorf("%s (%q): query error: %v", key, c.legendFormat, err)
					continue
				}

				// Universal invariant: every row, every zone, must
				// render exactly one valid four-state value. This alone
				// catches empty-vector blank rows and out-of-range
				// arithmetic across the whole dashboard.
				state, ok := toState(val)
				if !ok {
					t.Errorf("%s (%q): rendered invalid state value %v — "+
						"expected one of 0/1/2/3; predicate:\n  %s",
						key, c.legendFormat, val, q)
					continue
				}

				// Pinned cells must match their documented state.
				if want, pinned := expectations[key]; pinned && state != want {
					t.Errorf("%s (%q): got %s, want %s; predicate:\n  %s",
						key, c.legendFormat, state, want, q)
				}
				checked++
			}
		}
	}

	// Guard against a silent no-op (e.g. all slices empty or zones
	// list emptied by a bad refactor).
	if checked == 0 {
		t.Fatal("no predicates were evaluated — check slices or demoZones is empty")
	}
	t.Logf("evaluated %d (row × zone) predicates across %d zones", checked, len(demoZones))

	// Belt-and-braces: every pinned expectation must correspond to a
	// real (panel, refId) so a typo in the table can't pass vacuously.
	assertExpectationKeysExist(t)
}

// toState converts a query result value to a four-state label, rejecting
// anything outside {0,1,2,3} or non-integral.
func toState(v float64) (string, bool) {
	if math.Abs(v-math.Round(v)) > 1e-9 {
		return "", false
	}
	name, ok := stateName[int(math.Round(v))]
	return name, ok
}

// queryScalar runs an instant query and returns the single sample's
// value. It errors if the result is not exactly one sample (a status
// predicate must always reduce to one label-less value).
func queryScalar(client *http.Client, promql string) (float64, error) {
	endpoint := promURL() + "/api/v1/query?" + url.Values{"query": {promql}}.Encode()
	resp, err := client.Get(endpoint)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value [2]any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, fmt.Errorf("decode response: %v", err)
	}
	if parsed.Status != "success" {
		return 0, fmt.Errorf("query status %q", parsed.Status)
	}
	if len(parsed.Data.Result) != 1 {
		return 0, fmt.Errorf("expected exactly 1 sample, got %d (predicate returned an empty or multi-series result — likely a blank dashboard cell)", len(parsed.Data.Result))
	}
	raw, ok := parsed.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected value type %T", parsed.Data.Result[0].Value[1])
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse value %q: %v", raw, err)
	}
	return f, nil
}

// assertExpectationKeysExist verifies every pinned expectation key names
// a real (panel, refId) pair — a guard against table typos that would
// otherwise pass vacuously (the key would just never be looked up).
func assertExpectationKeysExist(t *testing.T) {
	t.Helper()
	valid := map[string]bool{}
	for _, p := range panelChecks() {
		for _, c := range p.checks {
			valid[p.name+"/"+c.refId] = true
		}
	}
	for key := range expectations {
		// key is panel/refId/zone — strip the zone (last /-segment).
		idx := strings.LastIndex(key, "/")
		prefix := key[:idx]
		zone := key[idx+1:]
		if !valid[prefix] {
			t.Errorf("expectation key %q references unknown panel/refId %q", key, prefix)
		}
		found := false
		for _, z := range demoZones {
			if z == zone {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expectation key %q references unknown zone %q", key, zone)
		}
	}
}
