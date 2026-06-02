package main

import (
	"strings"
	"testing"
)

// TestStatusChecksHaveDetail enforces the rule from the
// `feedback-metric-needs-dashboard` memory note: every status-table
// row MUST ship with detail text. A contributor adding a new
// statusCheck to one of the package-level lists below who forgets
// to fill in the detail field will hit this test, not the user.
//
// The test runs in the default (non-integration) build so it's part
// of the standard `go test ./...` pass — operates entirely on
// package-level data; no DNS / dashboard rendering required.
func TestStatusChecksHaveDetail(t *testing.T) {
	cases := []struct {
		listName string
		checks   []statusCheck
	}{
		{"parentStatusChecks", parentStatusChecks},
		{"nsStatusChecks", nsStatusChecks},
		{"soaStatusChecks", soaStatusChecks},
		{"mxStatusChecks", mxStatusChecks},
		{"emailAuthStatusChecks", emailAuthStatusChecks},
	}

	for _, tc := range cases {
		for i, c := range tc.checks {
			// legendFormat is the human-readable label that
			// appears in the rendered table — use it as the
			// identifier in failure messages so the operator
			// can find the offending row immediately.
			id := c.legendFormat
			if id == "" {
				id = "(row " + itoa(i) + ")"
			}
			if strings.TrimSpace(c.detail) == "" {
				t.Errorf("%s[%d] %q: detail field is empty — every status check must document its metric, why a FAIL matters, and where to look next",
					tc.listName, i, id)
			}
		}
	}
}

// itoa is a stdlib-free int → string for the test's identifier
// fallback; the test runs without other deps and there's no need
// to pull strconv just for the rare missing-legend case.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestComposeStatusExpr guards the four-state composition contract
// (constitution Principle IX):
//
//   - a check with only `expr` compiles to that expr verbatim, so
//     existing PASS/FAIL rows produce byte-identical dashboard JSON;
//   - a check that sets naExpr or warnExpr is rewritten and still
//     references each sub-predicate, so the 0/1/2/3 arithmetic is
//     actually wired up;
//   - every real status check composes to a non-empty expression
//     (an empty target Expr renders a blank, dataless row).
//
// It does NOT evaluate the PromQL against Prometheus (that needs a
// live stack — see issue #46); it guards the string layer only.
func TestComposeStatusExpr(t *testing.T) {
	// A pass/fail-only row must be returned unchanged.
	plain := statusCheck{refId: "A", expr: "dnshealth_x == bool 1"}
	if got := composeStatusExpr(plain); got != plain.expr {
		t.Errorf("plain check: composeStatusExpr altered a pass/fail-only row\n got: %s\nwant: %s", got, plain.expr)
	}

	// A row with naExpr/warnExpr must be rewritten and mention each
	// sub-predicate.
	rich := statusCheck{refId: "B", expr: "HARDP", naExpr: "NAP", warnExpr: "WARNP"}
	got := composeStatusExpr(rich)
	if got == rich.expr {
		t.Fatalf("rich check: composeStatusExpr returned expr unchanged despite na/warn being set")
	}
	for _, sub := range []string{"HARDP", "NAP", "WARNP"} {
		if !strings.Contains(got, sub) {
			t.Errorf("rich check: composed expr is missing sub-predicate %q\n composed: %s", sub, got)
		}
	}

	// Every real status check must compose to a non-empty expression.
	lists := map[string][]statusCheck{
		"parentStatusChecks":    parentStatusChecks,
		"nsStatusChecks":        nsStatusChecks,
		"soaStatusChecks":       soaStatusChecks,
		"mxStatusChecks":        mxStatusChecks,
		"emailAuthStatusChecks": emailAuthStatusChecks,
	}
	for name, checks := range lists {
		for i, c := range checks {
			if strings.TrimSpace(composeStatusExpr(c)) == "" {
				t.Errorf("%s[%d] %q: composed to an empty expression", name, i, c.legendFormat)
			}
		}
	}
}
