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

// demoZones (the list of probed zones) lives in the untagged
// demozones_test.go so the default `go test` build can guard it against the
// exporter config (TestDemoZonesMatchExporterConfig). It is referenced here
// to drive the per-zone predicate evaluation below.

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
		{"spf", spfStatusChecks},
		{"dmarc", dmarcStatusChecks},
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

	// SOA rows (#49): N/A for zones that produced no SOA data this cycle;
	// WARN (not FAIL) when the MNAME is outside the parent-advertised NS
	// set (a legitimate hidden-master pattern); FAIL only for a genuine
	// serial divergence.
	//
	// healthy.demo. — full SOA, MNAME in set, serials agree.
	"soa/A/healthy.demo.": "PASS",
	"soa/B/healthy.demo.": "PASS",
	"soa/C/healthy.demo.": "PASS",
	// lame-nameserver.demo. + missing-glue.demo. — no NS answers SOA, so
	// every SOA row reads N/A rather than a false FAIL (the bug #49 fixes).
	"soa/A/lame-nameserver.demo.": "N/A",
	"soa/B/lame-nameserver.demo.": "N/A",
	"soa/C/lame-nameserver.demo.": "N/A",
	"soa/A/missing-glue.demo.":    "N/A",
	"soa/B/missing-glue.demo.":    "N/A",
	"soa/C/missing-glue.demo.":    "N/A",
	// soa-serial-mismatch.demo. — serials 100 vs 101 → row A genuinely
	// FAILs; MNAME is in set and resolves so B/C PASS.
	"soa/A/soa-serial-mismatch.demo.": "FAIL",
	"soa/B/soa-serial-mismatch.demo.": "PASS",
	// ns-mismatch.demo. — parent advertises ns1 but the SOA MNAME
	// (ns-internal-a) is outside that parent-advertised set → row B WARNs,
	// not FAILs. Single-NS serial is trivially consistent → row A PASS.
	"soa/A/ns-mismatch.demo.": "PASS",
	"soa/B/ns-mismatch.demo.": "WARN",
	"soa/C/ns-mismatch.demo.": "PASS",

	// ns-ip-mismatch.demo. (#37) — the regression zone for NS row H.
	// Parent glues ns1/ns2 to 172.31.0.19; the auth answers their A as
	// 172.31.0.20. Same NS NAMES on both sides (so the name rows D/G
	// PASS), different IP behind them (so row H FAILs). Two NSes meets
	// the RFC 2182 minimum → row A WARNs.
	"ns/H/ns-ip-mismatch.demo.": "FAIL",
	"ns/D/ns-ip-mismatch.demo.": "PASS",
	"ns/G/ns-ip-mismatch.demo.": "PASS",
	"ns/A/ns-ip-mismatch.demo.": "WARN",
	// Row H must NOT mis-fire elsewhere: agreeing zones PASS, a zone the
	// parent provides no glue for reads N/A, and the name/lame-divergent
	// zones are left to rows D/G (row H stays PASS, not double-counted).
	"ns/H/healthy.demo.":           "PASS",
	"ns/H/missing-glue.demo.":      "N/A",
	"ns/H/lame-nameserver.demo.":   "PASS",
	"ns/H/ns-mismatch.demo.":       "PASS",
	"ns/H/ns-names-mismatch.demo.": "PASS",

	// ---- Mail no-data → N/A fix (this change) ----
	// An unreachable zone (no SOA data this cycle — lame NS / missing glue)
	// reads N/A across EVERY mail row, not a cascade of FAILs unrelated to
	// the delegation breakage it actually demonstrates. This is the
	// regression pin for the soaNoData naExpr added to the MX/SPF/DMARC
	// rows; before it, MX rows FAILed and SPF/DMARC row A WARNed.
	"mx/A/lame-nameserver.demo.":    "N/A",
	"mx/B/lame-nameserver.demo.":    "N/A",
	"mx/E/lame-nameserver.demo.":    "N/A",
	"spf/A/lame-nameserver.demo.":   "N/A",
	"spf/B/lame-nameserver.demo.":   "N/A",
	"spf/C/lame-nameserver.demo.":   "N/A",
	"dmarc/A/lame-nameserver.demo.": "N/A",
	"dmarc/B/lame-nameserver.demo.": "N/A",
	"mx/A/missing-glue.demo.":       "N/A",
	"mx/E/missing-glue.demo.":       "N/A",
	"spf/A/missing-glue.demo.":      "N/A",
	"dmarc/A/missing-glue.demo.":    "N/A",

	// ---- Green mail baseline on reachable non-mail zones ----
	// The flagship: selecting healthy.demo shows an all-green Mail section
	// (Null MX + v=spf1 -all + DMARC p=reject), no cross-signal noise.
	"mx/A/healthy.demo.":    "PASS",
	"mx/E/healthy.demo.":    "PASS",
	"spf/A/healthy.demo.":   "PASS",
	"spf/B/healthy.demo.":   "PASS",
	"spf/C/healthy.demo.":   "PASS",
	"dmarc/A/healthy.demo.": "PASS",
	"dmarc/B/healthy.demo.": "PASS",
	// MX-family zones gain a green SPF/DMARC baseline (their signal is MX).
	"spf/A/mx-healthy.demo.":         "PASS",
	"dmarc/A/mx-healthy.demo.":       "PASS",
	"spf/A/mx-broken.demo.":          "PASS",
	"dmarc/A/mx-broken.demo.":        "PASS",
	"spf/A/mx-null.demo.":            "PASS",
	"dmarc/A/mx-null.demo.":          "PASS",
	"spf/A/mx-null-conflict.demo.":   "PASS",
	"dmarc/A/mx-null-conflict.demo.": "PASS",

	// ---- Mail family (specs 009/010): each zone isolates ONE signal ----
	// SPF rows: A = single-valid-record, B = terminal `all` qualifier,
	// C = 10-lookup budget. DMARC rows: A = valid record, B = enforcing
	// policy. Every cell NOT carrying the zone's signal is green.
	//
	// email-healthy — real MX + -all + p=reject → entire Mail section PASS.
	"mx/A/email-healthy.demo.":    "PASS",
	"mx/B/email-healthy.demo.":    "PASS",
	"mx/E/email-healthy.demo.":    "PASS",
	"spf/A/email-healthy.demo.":   "PASS",
	"spf/B/email-healthy.demo.":   "PASS",
	"spf/C/email-healthy.demo.":   "PASS",
	"dmarc/A/email-healthy.demo.": "PASS",
	"dmarc/B/email-healthy.demo.": "PASS",
	// email-nomail — Null MX yet -all + p=reject: all PASS, proving the
	// SPF/DMARC rows are MX-independent (FR-017).
	"mx/A/email-nomail.demo.":    "PASS",
	"mx/B/email-nomail.demo.":    "N/A",
	"mx/E/email-nomail.demo.":    "PASS",
	"spf/A/email-nomail.demo.":   "PASS",
	"spf/B/email-nomail.demo.":   "PASS",
	"spf/C/email-nomail.demo.":   "PASS",
	"dmarc/A/email-nomail.demo.": "PASS",
	"dmarc/B/email-nomail.demo.": "PASS",
	// email-no-auth — Null MX green; SPF and DMARC both absent → present
	// rows WARN, qualifier/policy rows N/A.
	"mx/A/email-no-auth.demo.":    "PASS",
	"spf/A/email-no-auth.demo.":   "WARN",
	"spf/B/email-no-auth.demo.":   "N/A",
	"spf/C/email-no-auth.demo.":   "N/A",
	"dmarc/A/email-no-auth.demo.": "WARN",
	"dmarc/B/email-no-auth.demo.": "N/A",
	// dmarc-absent — SPF + MX green; only DMARC deviates (absent → WARN).
	"mx/A/dmarc-absent.demo.":    "PASS",
	"spf/A/dmarc-absent.demo.":   "PASS",
	"spf/B/dmarc-absent.demo.":   "PASS",
	"spf/C/dmarc-absent.demo.":   "PASS",
	"dmarc/A/dmarc-absent.demo.": "WARN",
	"dmarc/B/dmarc-absent.demo.": "N/A",
	// dmarc-monitoring — SPF + MX green; DMARC valid but p=none → policy WARN.
	"mx/A/dmarc-monitoring.demo.":    "PASS",
	"spf/A/dmarc-monitoring.demo.":   "PASS",
	"dmarc/A/dmarc-monitoring.demo.": "PASS",
	"dmarc/B/dmarc-monitoring.demo.": "WARN",
	// dmarc-malformed — SPF + MX green; DMARC present but no p= → valid FAIL.
	"mx/A/dmarc-malformed.demo.":    "PASS",
	"spf/A/dmarc-malformed.demo.":   "PASS",
	"dmarc/A/dmarc-malformed.demo.": "FAIL",
	"dmarc/B/dmarc-malformed.demo.": "N/A",
	// spf-permissive — DMARC + MX green; SPF +all → qualifier WARN.
	"mx/A/spf-permissive.demo.":    "PASS",
	"spf/A/spf-permissive.demo.":   "PASS",
	"spf/B/spf-permissive.demo.":   "WARN",
	"spf/C/spf-permissive.demo.":   "PASS",
	"dmarc/A/spf-permissive.demo.": "PASS",
	"dmarc/B/spf-permissive.demo.": "PASS",
	// spf-multiple — DMARC + MX green; two SPF records → valid FAIL, the
	// qualifier and budget rows N/A (no single record to read).
	"mx/A/spf-multiple.demo.":    "PASS",
	"spf/A/spf-multiple.demo.":   "FAIL",
	"spf/B/spf-multiple.demo.":   "N/A",
	"spf/C/spf-multiple.demo.":   "N/A",
	"dmarc/A/spf-multiple.demo.": "PASS",
	"dmarc/B/spf-multiple.demo.": "PASS",
	// spf-toomanylookups — DMARC + MX green; chained includes >10 → budget FAIL.
	"mx/A/spf-toomanylookups.demo.":    "PASS",
	"spf/A/spf-toomanylookups.demo.":   "PASS",
	"spf/B/spf-toomanylookups.demo.":   "PASS",
	"spf/C/spf-toomanylookups.demo.":   "FAIL",
	"dmarc/A/spf-toomanylookups.demo.": "PASS",
	// spf-incomplete — DMARC + MX green; unresolvable include → still PASS
	// (no false FAIL on a flaky include; eval_complete=0).
	"mx/A/spf-incomplete.demo.":    "PASS",
	"spf/A/spf-incomplete.demo.":   "PASS",
	"spf/B/spf-incomplete.demo.":   "PASS",
	"spf/C/spf-incomplete.demo.":   "PASS",
	"dmarc/A/spf-incomplete.demo.": "PASS",
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
	defer func() { _ = resp.Body.Close() }()
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
