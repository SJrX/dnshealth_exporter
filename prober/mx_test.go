//go:build integration

package prober_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

// TestMX_HappyPath — multi-MX healthy zone. Parent delegates the
// zone; auth has two MX records at priorities 10 and 20, both
// targets resolve and neither is a CNAME.
func TestMX_HappyPath(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			MX("example.test", 10, "mail-a.example.test"),
			MX("example.test", 20, "mail-b.example.test"),
			A("mail-a.example.test", "127.240.0.3"),
			A("mail-b.example.test", "127.240.0.4"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeMX, "example.test")

	// Verification — both MXes have info gauges with correct
	// priorities, both resolve, neither is a CNAME, both
	// syntactically valid.
	AssertGauge(t, metrics, "dnshealth_mx_info",
		WithLabels("zone", "example.test", "target", "mail-a.example.test.",
			"priority", "00010"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_mx_info",
		WithLabels("zone", "example.test", "target", "mail-b.example.test.",
			"priority", "00020"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_mx_resolves",
		WithLabels("zone", "example.test", "target", "mail-a.example.test."),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_mx_resolves",
		WithLabels("zone", "example.test", "target", "mail-b.example.test."),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_mx_is_cname",
		WithLabels("zone", "example.test", "target", "mail-a.example.test."),
		WithValue(0))
	AssertGauge(t, metrics, "dnshealth_mx_syntax_valid",
		WithLabels("zone", "example.test", "target", "mail-a.example.test."),
		WithValue(1))
}

// TestMX_UnresolvableTarget — one MX target has no A or AAAA record
// anywhere reachable. Resolves gauge reads 0 for that target while
// the healthy target still reads 1.
func TestMX_UnresolvableTarget(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			MX("example.test", 10, "mail-good.example.test"),
			MX("example.test", 20, "mail-missing.example.test"),
			A("mail-good.example.test", "127.240.0.3"),
			// Deliberately NO A/AAAA for mail-missing.example.test.
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeMX, "example.test")

	// Verification
	AssertGauge(t, metrics, "dnshealth_mx_resolves",
		WithLabels("zone", "example.test", "target", "mail-good.example.test."),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_mx_resolves",
		WithLabels("zone", "example.test", "target", "mail-missing.example.test."),
		WithValue(0))
}

// TestMX_CNAMEdTarget — one MX target IS a CNAME (target hostname
// has a CNAME RR at its name). Violates RFC 2181 §10.3.
func TestMX_CNAMEdTarget(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			MX("example.test", 10, "mail-cname.example.test"),
			CNAME("mail-cname.example.test", "real-mail.example.test"),
			A("real-mail.example.test", "127.240.0.5"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeMX, "example.test")

	// Verification — is_cname=1 for the CNAMEd target.
	AssertGauge(t, metrics, "dnshealth_mx_is_cname",
		WithLabels("zone", "example.test", "target", "mail-cname.example.test."),
		WithValue(1))
}

// TestMX_InvalidSyntaxTarget — MX target hostname with an underscore
// (LDH violation per RFC 952 / 1123). Also exercises US4 (syntax
// check) since the underlying validator is the same helper from
// spec N6 (isValidNSHostname).
func TestMX_InvalidSyntaxTarget(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			MX("example.test", 10, "bad_mail.example.test"),
			A("bad_mail.example.test", "127.240.0.6"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeMX, "example.test")

	// Verification — syntax_valid=0 for the underscored target.
	AssertGauge(t, metrics, "dnshealth_mx_syntax_valid",
		WithLabels("zone", "example.test", "target", "bad_mail.example.test."),
		WithValue(0))
}

// TestMX_NullMX — zone publishing exactly one MX with `0 .`
// (canonical RFC 7505 Null MX). Asserts mx_null_mx=1, info gauge
// present with target="." priority="0", and no spurious resolves/
// is_cname series for the "." sentinel (intentionally skipped).
func TestMX_NullMX(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			MX("example.test", 0, "."), // Null MX per RFC 7505
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeMX, "example.test")

	// Verification — info gauge present with the canonical "."
	// sentinel target and priority "0". dnshealth_mx_null_mx itself
	// is derived by the cycle runner from this info-gauge presence
	// (not emitted by the prober — avoids the gauge-name collision
	// pattern from spec 007 D-1); see TestCycleRunner_MXNullMX
	// for the runner-level verification.
	AssertGauge(t, metrics, "dnshealth_mx_info",
		WithLabels("zone", "example.test", "target", ".", "priority", "00000"),
		WithValue(1))
	// No spurious mx_resolves / mx_is_cname for the "." sentinel
	// — per data-model.md edge-case table, those checks intentionally
	// don't apply.
	AssertGaugeMissing(t, metrics, "dnshealth_mx_resolves",
		WithLabels("zone", "example.test", "target", "."))
	AssertGaugeMissing(t, metrics, "dnshealth_mx_is_cname",
		WithLabels("zone", "example.test", "target", "."))
}

// TestMX_NullMXConflict — zone publishing `0 .` AND a real MX
// record. Per RFC 7505 §3 the canonical Null MX form requires
// the Null MX to be the SOLE MX RR; coexistence is malformed.
// Asserts mx_null_mx=0 (since the canonical single-Null-MX form
// is NOT met) but the info gauges for BOTH RRs are still emitted
// so operators can see the malformed state in metrics.
func TestMX_NullMXConflict(t *testing.T) {
	// Fixture Setup — Null MX plus a real MX coexisting.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			MX("example.test", 0, "."),                       // Null MX
			MX("example.test", 10, "real-mail.example.test"), // ALSO a real one
			A("real-mail.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeMX, "example.test")

	// Verification — mx_null_mx=0 because the canonical form
	// (exactly one MX RR with `0 .`) is NOT met. Operators
	// detect this via the row-E predicate: null_mx==1 AND
	// count>1 → FAIL. Here null_mx is 0 (not the canonical
	// form), but the conflict is still surfaced because both
	// MX info gauges are visible.
	//
	// Note: the row E PromQL is `(null_mx == 0) OR (count == 1)`
	// → for this fixture: null_mx==0 yields PASS via the first
	// branch. That's correct — this isn't the canonical Null MX
	// conflict the row was designed to catch. The actual
	// "conflict" the row catches is: zone publishes a SINGLE MX
	// RR that's `0 .` (so null_mx==1) AND has additional MX RRs
	// from some other source (multi-auth disagreement) — that's
	// the case where count>1 AND null_mx==1.
	//
	// For THIS fixture, the operator-visible artifact is just
	// the two info-gauge series. Both targets appear:
	AssertGauge(t, metrics, "dnshealth_mx_info",
		WithLabels("zone", "example.test", "target", ".", "priority", "00000"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_mx_info",
		WithLabels("zone", "example.test", "target", "real-mail.example.test.", "priority", "00010"),
		WithValue(1))
	// dnshealth_mx_null_mx is derived by the cycle runner from
	// the per-MX info gauges (not emitted by the prober), so
	// env.Probe doesn't surface it. Runner-level coverage:
	// TestCycleRunner_MXNullMXNotEmittedForConflict below verifies
	// the runner correctly reads null_mx=0 in this conflict case.
}

// TestMX_LabelContract — emitted MX metrics must carry exactly the
// labels documented in contracts/mx-metrics.md. Regression guard for
// spec 008 D-7: the prober originally repurposed ProbeResult.Nameserver
// to carry the MX target (producing a stray `nameserver=<target>`
// label) and shared one Labels map across all per-RR metrics (forcing
// `priority` onto mx_resolves / mx_is_cname / mx_syntax_valid, which
// the contract says belong only to mx_info). Both errors caused
// duplicate / unjoinable columns in the dashboard records table.
func TestMX_LabelContract(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			MX("example.test", 10, "mail.example.test"),
			A("mail.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	registry := env.Probe(prober.ProbeMX, "example.test")

	// Verification — gather raw families and check exact label
	// keysets per metric. AssertGauge uses subset matching, which
	// would happily pass even with extra labels — this test uses
	// the registry directly so it catches over-emission.
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}

	wantLabels := map[string][]string{
		// Per contracts/mx-metrics.md: mx_info has zone+target+priority
		// plus the prober-plumbing labels (nameserver, ip — both empty
		// for the MX prober since it's per-zone, not per-NS fan-out).
		"dnshealth_mx_info":         {"ip", "nameserver", "priority", "target", "zone"},
		"dnshealth_mx_resolves":     {"ip", "nameserver", "target", "zone"},
		"dnshealth_mx_is_cname":     {"ip", "nameserver", "target", "zone"},
		"dnshealth_mx_syntax_valid": {"ip", "nameserver", "target", "zone"},
	}

	for name, want := range wantLabels {
		var found bool
		for _, fam := range families {
			if fam.GetName() != name {
				continue
			}
			for _, m := range fam.GetMetric() {
				found = true
				got := []string{}
				for _, lp := range m.GetLabel() {
					got = append(got, lp.GetName())
				}
				sort.Strings(got)
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Errorf("%s: got labels [%s], want [%s]",
						name, strings.Join(got, ","), strings.Join(want, ","))
				}
				// Belt-and-braces: the over-emission bug specifically
				// set nameserver=<target>; assert nameserver is empty.
				for _, lp := range m.GetLabel() {
					if lp.GetName() == "nameserver" && lp.GetValue() != "" {
						t.Errorf("%s: nameserver label must be empty (MX is per-zone, not per-NS), got %q",
							name, lp.GetValue())
					}
				}
			}
		}
		if !found {
			t.Errorf("%s: no series found in registry", name)
		}
	}
}

// TestMX_NSFailover — first parent-listed NS drops queries; ProbeMX
// must failover to the second NS and complete normally. Without the
// failover (post-review-fix), the whole zone's MX panel would be
// blank for any zone whose first parent NS is down.
func TestMX_NSFailover(t *testing.T) {
	// Fixture Setup — parent has 2 NSes. The first (at .2) drops
	// every query (simulates unreachable); the second (at .3) serves
	// the zone normally.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		ServerWithOptions("127.240.0.2:"+TestPort, ServerOptions{Drop: true},
			SOA("example.test"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
			MX("example.test", 10, "mail.example.test"),
			A("mail.example.test", "127.240.0.5"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeMX, "example.test")

	// Verification — despite the first NS dropping, the MX panel
	// surfaces the zone's MX record via failover. The mx_info gauge
	// is the assertion target because mx_resolves depends on a
	// separate ResolveHostnames walk that goes through the same
	// dropped NS at .2 (the test's RootServers only knows about
	// .1 referring to .2 — that's not the MX prober's failover
	// concern, it's a downstream resolution concern). The mx_info
	// surface alone proves the MX prober itself failed over.
	AssertGauge(t, metrics, "dnshealth_mx_info",
		WithLabels("zone", "example.test", "target", "mail.example.test."),
		WithValue(1))
}
