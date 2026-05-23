//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

// TestNSClassification_HappyPath — clean state, no asymmetry.
// Parent advertises [ns1, ns2] and both auth servers report the
// same [ns1, ns2] set. Each NS gets classification="both"; no
// self-only or parent-only series should exist.
func TestNSClassification_HappyPath(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeNSClassification, "example.test")

	// Verification — every NS in `both`, no other classifications present.
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.",
			"classification", "both"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.",
			"classification", "both"),
		WithValue(1))
	AssertGaugeMissing(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "classification", "self-only"))
	AssertGaugeMissing(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "classification", "parent-only"))
}

// TestNSClassification_SelfOnlyStealth — the canonical hidden-master
// case. Parent advertises [ns1, ns2]; the auth's self-side NS RR
// set is [ns1, ns2, hidden-primary]. `hidden-primary` should classify
// as self-only; the other two as both.
func TestNSClassification_SelfOnlyStealth(t *testing.T) {
	// Fixture Setup — parent has 2 NSes; auth reports 3 (including
	// the hidden-primary stealth NS).
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			NS("example.test", "hidden-primary.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			NS("example.test", "hidden-primary.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeNSClassification, "example.test")

	// Verification — hidden-primary classified self-only; the
	// publicly-advertised NSes classified both.
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.",
			"classification", "both"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.",
			"classification", "both"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "hidden-primary.example.test.",
			"classification", "self-only"),
		WithValue(1))
}

// TestNSClassification_StealthReachable_LeakedListing covers spec 007
// FR-010 / R-9: a self-only stealth NS whose hostname doesn't resolve
// anywhere reachable. The active reachability probe MUST emit
// dnshealth_ns_stealth_reachable = 0 — modeling the leaked-listing
// pattern (vs. a working hidden master, see next test).
func TestNSClassification_StealthReachable_LeakedListing(t *testing.T) {
	// Fixture Setup — parent has 2 NSes; auth's NS RR set adds a
	// third (leaked) name. No A record for the leaked NS anywhere,
	// so ResolveHostnames returns empty → reachable = 0.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "leaked.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			// Deliberately NO A for leaked.example.test.
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeNSClassification, "example.test")

	// Verification — leaked.example.test classified self-only AND
	// reachability reads 0.
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "leaked.example.test.",
			"classification", "self-only"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_stealth_reachable",
		WithLabels("zone", "example.test", "nameserver", "leaked.example.test."),
		WithValue(0))
}

// TestNSClassification_StealthReachable_WorkingHiddenMaster covers
// the other side of FR-010 / R-9: a self-only NS that DOES resolve
// and DOES respond authoritatively. The reachability probe MUST emit
// 1 — the working-hidden-master pattern (vs. leaked listing above).
func TestNSClassification_StealthReachable_WorkingHiddenMaster(t *testing.T) {
	// Fixture Setup — parent has 1 NS at .2; auth reports a hidden
	// master at .9. The hidden master itself runs at .9 and serves
	// example.test authoritatively, even though the parent doesn't
	// publicly advertise it. This is the canonical legitimate
	// hidden-master pattern.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			// Glue for hidden-primary's hostname so the resolver can
			// find it during the active reachability probe. (In real
			// DNS, this would be reachable via the hidden-primary's
			// own zone delegation, but since hidden-primary is
			// in-bailiwick here, the parent ships glue.)
			A("hidden-primary.example.test", "127.240.0.9"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "hidden-primary.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("hidden-primary.example.test", "127.240.0.9"),
		).
		Server("127.240.0.9:"+TestPort,
			// The "hidden master" — answers authoritatively for
			// the zone, returns a SOA. Not in the parent's NS set
			// but still functional.
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "hidden-primary.example.test"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeNSClassification, "example.test")

	// Verification — hidden-primary classified self-only AND the
	// reachability probe finds it authoritative (= 1).
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "hidden-primary.example.test.",
			"classification", "self-only"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_stealth_reachable",
		WithLabels("zone", "example.test", "nameserver", "hidden-primary.example.test."),
		WithValue(1))
}

// TestNSClassification_ParentOnly covers US3 (the symmetric
// divergence): an NS hostname appears in the parent's NS RR set but
// none of the auths report it. Classifies as parent-only.
func TestNSClassification_ParentOnly(t *testing.T) {
	// Fixture Setup — parent advertises [ns1, ns2, abandoned]; both
	// auths only report [ns1, ns2]. The `abandoned` NS is the
	// parent-only case (registrar still publishes a stale NS that
	// the zone owner removed from their own config).
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			NS("example.test", "abandoned.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
			A("abandoned.example.test", "127.240.0.4"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeNSClassification, "example.test")

	// Verification — abandoned classified parent-only; the others
	// classified both. parent-only NSes get NO stealth_reachable
	// emission (probe runs only for self-only per R-9).
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "abandoned.example.test.",
			"classification", "parent-only"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.",
			"classification", "both"),
		WithValue(1))
	AssertGaugeMissing(t, metrics, "dnshealth_ns_stealth_reachable",
		WithLabels("zone", "example.test", "nameserver", "abandoned.example.test."))
}

// TestNSClassification_MultiAuthUnion covers FR-007 / R-4: when
// multiple auths report different self sets, the classifier
// computes the UNION (any NS reported by at least one auth is in
// the self set). A single dissenting auth's view is enough to
// surface an NS as `both` or `self-only`.
func TestNSClassification_MultiAuthUnion(t *testing.T) {
	// Fixture Setup — parent has [ns1, ns2]; ns1 reports [ns1, ns2]
	// (matches parent); ns2 reports [ns1, ns2, leaked-master] (adds
	// a third name). Union should include all three; leaked-master
	// classifies self-only despite only ONE auth knowing about it.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			// ns1 view: matches parent, no extras.
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			// ns2 view: adds leaked-master to the NS RR set.
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			NS("example.test", "leaked-master.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeNSClassification, "example.test")

	// Verification — leaked-master surfaces as self-only via UNION
	// semantics even though only ns2 reports it.
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "leaked-master.example.test.",
			"classification", "self-only"),
		WithValue(1))
	// Sanity: ns1 and ns2 are still classified `both`.
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.",
			"classification", "both"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_classification",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.",
			"classification", "both"),
		WithValue(1))
}
