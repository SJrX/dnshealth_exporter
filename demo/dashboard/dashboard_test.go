//go:build integration

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDashboardJSONMatchesGenerator is the drift test specified in
// research.md R-7 and contracts/dashboard-output.md. It runs the same
// builder + marshal pipeline as main.go and compares the bytes against
// the committed JSON files. A mismatch means someone edited the JSON
// by hand, or edited the typed source without re-running `make
// dashboards`. Either way the fix is the same: regenerate.
//
// Built only under -tags=integration to keep it out of the default
// unit test pass (matches the project convention; see CLAUDE.md).
func TestDashboardJSONMatchesGenerator(t *testing.T) {
	// Fixture Setup — locate repo root via the test file's own path,
	// then enumerate the (variant, committedPath) cases. The drift
	// test deliberately mirrors main.go's variants slice rather than
	// importing it — if main.go and the test agree on the cases but
	// disagree on the output, the disagreement is meaningful (drift),
	// not just shared-state coincidence.
	repoRoot := repoRootFromTestFile(t)
	cases := []struct {
		uid             string
		title           string
		path            string
		includeInfoText bool
	}{
		{"dnshealth-overview", "DNS Health Overview",
			"demo/grafana/dashboards/dnshealth-overview.json", false},
		{"dnshealth-overview-demo", "DNS Health Overview (demo)",
			"demo/grafana/dashboards/dnshealth-overview-demo.json", true},
	}

	for _, tc := range cases {
		t.Run(tc.uid, func(t *testing.T) {
			// Exercise SUT — build and marshal exactly like main.go.
			d, err := buildOverview(tc.uid, tc.title, tc.includeInfoText)
			if err != nil {
				t.Fatalf("buildOverview(%q): %v", tc.uid, err)
			}
			generated, err := marshalDashboard(d)
			if err != nil {
				t.Fatalf("marshalDashboard(%q): %v", tc.uid, err)
			}

			fullPath := filepath.Join(repoRoot, tc.path)
			committed, err := os.ReadFile(fullPath)
			if err != nil {
				t.Fatalf("read committed JSON %s: %v", fullPath, err)
			}

			// Verification — bytes must match exactly. Use t.Errorf
			// (not Fatalf) so both variants are checked in one run.
			if !bytes.Equal(generated, committed) {
				t.Errorf("dashboard JSON drifted from generator source for %s\n"+
					"  committed: %s\n"+
					"  hint: run `make dashboards` from repo root to regenerate, "+
					"then commit the resulting diff",
					tc.uid, fullPath)
			}
		})
	}
}

// repoRootFromTestFile walks up from this test file's location to the
// repo root (verified by the presence of go.mod there). Test runs from
// arbitrary cwd (go test sets cwd to the package dir), so we resolve
// committed-file paths relative to the test file rather than relying
// on $PWD. If the test file is ever moved, the go.mod check fails
// loudly so we can fix the path here.
func repoRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// thisFile is .../demo/dashboard/dashboard_test.go — repo root is two levels up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repoRootFromTestFile: expected go.mod at %s but: %v — has the test file moved?", root, err)
	}
	return root
}
