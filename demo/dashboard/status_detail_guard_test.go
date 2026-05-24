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
