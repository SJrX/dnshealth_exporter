package main

import (
	"fmt"
	"os"
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
}

var variants = []variant{
	{
		uid:             "dnshealth-overview",
		title:           "DNS Health Overview",
		path:            "demo/grafana/dashboards/dnshealth-overview.json",
		includeInfoText: true,
	},
	{
		uid:             "dnshealth-overview-clean",
		title:           "DNS Health Overview (clean)",
		path:            "demo/grafana/dashboards/dnshealth-overview-clean.json",
		includeInfoText: false,
	},
}

func main() {
	for _, v := range variants {
		d, err := buildOverview(v.uid, v.title, v.includeInfoText)
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
