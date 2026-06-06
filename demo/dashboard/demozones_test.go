package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/sjr/dnshealth_exporter/config"
)

// demoZones is the set of zones the demo deployment probes (mirrors
// demo/exporter/dnshealth.yml). Hardcoded rather than read from the config
// so that adding a zone is a conscious, reviewed change to the expectations
// in promql_live_test.go — a new zone that renders a garbage state should
// trip the universal invariant, not be silently skipped.
//
// It lives in this UNTAGGED file (not promql_live_test.go) so the default
// `go test` build can see it — TestDemoZonesMatchExporterConfig below keeps
// it in lockstep with the exporter config without needing the live stack.
var demoZones = []string{
	"healthy.demo.",
	"soa-serial-mismatch.demo.",
	"lame-nameserver.demo.",
	"ns-mismatch.demo.",
	"ns-names-mismatch.demo.",
	"ns-ip-mismatch.demo.",
	"v6-only.demo.",
	"dup-glue.demo.",
	"hidden-master.demo.",
	"missing-glue.demo.",
	"mx-healthy.demo.",
	"mx-broken.demo.",
	"mx-null.demo.",
	"mx-null-conflict.demo.",
	// Mail family — each isolates one MX/SPF/DMARC signal, named for it.
	"email-healthy.demo.",
	"email-nomail.demo.",
	"email-no-auth.demo.",
	"dmarc-absent.demo.",
	"dmarc-monitoring.demo.",
	"dmarc-malformed.demo.",
	"spf-permissive.demo.",
	"spf-multiple.demo.",
	"spf-toomanylookups.demo.",
	"spf-incomplete.demo.",
}

// TestDemoZonesMatchExporterConfig guards against the drift that let
// mx-null-conflict be pinned in the promql_live expectations (and delegated
// in CoreDNS) while being absent from the exporter config — so it was never
// actually probed and its pin evaluated against empty series. demoZones and
// demo/exporter/dnshealth.yml MUST list exactly the same zones: a zone in
// demoZones but not the config is never probed (promql_live then fails
// opaquely with "got 0 samples" only when the live stack is up); a zone in
// the config but not demoZones is probed but never state-checked. This test
// runs in the default build, so the drift is caught at `go test` time —
// long before the live smoke run.
func TestDemoZonesMatchExporterConfig(t *testing.T) {
	cfg, err := config.Load("../exporter/dnshealth.yml")
	if err != nil {
		t.Fatalf("loading demo exporter config: %v", err)
	}

	inConfig := map[string]bool{}
	for _, z := range cfg.Zones {
		inConfig[canonZone(z)] = true
	}
	inDemo := map[string]bool{}
	for _, z := range demoZones {
		inDemo[canonZone(z)] = true
	}

	var missingFromConfig, missingFromDemo []string
	for z := range inDemo {
		if !inConfig[z] {
			missingFromConfig = append(missingFromConfig, z)
		}
	}
	for z := range inConfig {
		if !inDemo[z] {
			missingFromDemo = append(missingFromDemo, z)
		}
	}
	sort.Strings(missingFromConfig)
	sort.Strings(missingFromDemo)

	if len(missingFromConfig) > 0 {
		t.Errorf("zones in demoZones but NOT in demo/exporter/dnshealth.yml (pinned/checked but never probed): %v", missingFromConfig)
	}
	if len(missingFromDemo) > 0 {
		t.Errorf("zones in demo/exporter/dnshealth.yml but NOT in demoZones (probed but never state-checked): %v", missingFromDemo)
	}
}

// canonZone normalises a zone name for set comparison: the config and the
// test both use trailing-dot FQDNs, but trimming the trailing dot makes the
// comparison robust to either form.
func canonZone(z string) string {
	return strings.TrimSuffix(strings.TrimSpace(z), ".")
}
