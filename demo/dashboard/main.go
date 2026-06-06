package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Demo dashboard generator. Emits two Grafana dashboard JSON files from
// one shared typed source (see dashboard.go::buildOverview). Run from
// the repo root: `make dashboards` (or `go run ./demo/dashboard`).
//
// Output paths are fixed; the program takes no flags. Errors panic
// because this is a code generator — silent partial output would be
// worse than a stack trace. See specs/005-dashboard-go-sdk/contracts/
// generator-cli.md.

type variant struct {
	uid             string
	title           string
	path            string
	includeInfoText bool
	defaultZone     string // initially-selected $zone; "" = let Grafana pick
}

var variants = []variant{
	{
		// Default variant — no demo-specific markdown header. This is
		// what any real deployment should import (and what's published
		// to grafana.com). defaultZone is empty so Grafana selects the
		// importing user's first real zone, not a demo name.
		uid:             "dnshealth-overview",
		title:           "DNS Health Overview",
		path:            "demo/grafana/dashboards/dnshealth-overview.json",
		includeInfoText: false,
		defaultZone:     "",
	},
	{
		// Demo variant — adds a markdown header describing the demo
		// zones (healthy.demo., soa-serial-mismatch.demo., etc.) and
		// what each row of the dashboard means in the demo context.
		// Only meaningful inside the bundled demo stack, so it opens on
		// the known-good demo zone.
		uid:             "dnshealth-overview-demo",
		title:           "DNS Health Overview (demo)",
		path:            "demo/grafana/dashboards/dnshealth-overview-demo.json",
		includeInfoText: true,
		defaultZone:     "healthy.demo.",
	},
}

func main() {
	// Sanity check: every output path is repo-root-relative, so we must
	// be invoked from there. The Makefile target enforces this; a raw
	// `go run` from elsewhere would silently write files to the wrong
	// place. Fail loudly with a clear hint instead.
	if _, err := os.Stat(filepath.Dir(variants[0].path)); err != nil {
		panic(fmt.Errorf("output directory %q not found relative to cwd — run from repo root (e.g., `make dashboards`): %w",
			filepath.Dir(variants[0].path), err))
	}

	for _, v := range variants {
		d, err := buildOverview(v.uid, v.title, v.includeInfoText, v.defaultZone)
		if err != nil {
			panic(fmt.Errorf("build %s: %w", v.uid, err))
		}
		b, err := marshalDashboard(d)
		if err != nil {
			panic(fmt.Errorf("marshal %s: %w", v.uid, err))
		}
		if err := os.WriteFile(v.path, b, 0644); err != nil {
			panic(fmt.Errorf("write %s: %w", v.path, err))
		}
		fmt.Printf("wrote %s (%d bytes)\n", v.path, len(b))
	}
}
