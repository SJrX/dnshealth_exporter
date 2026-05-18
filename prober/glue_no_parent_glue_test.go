//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

// TestGlueProber_SelfRecordsWhenParentLacksGlue is a regression test
// for issue #14. When the parent referral does not include A glue for
// the NS records it advertises, ProbeGlue used to silently skip the
// self-side NS queries (it iterated delegation.NSRecords with an
// empty-IP guard instead of the resolved `nameservers` parameter the
// caller had already built via ResolveHostname). Result: zones with
// out-of-bailiwick NSs had no dnshealth_ns_record{source="self"} series.
//
// Topology mirrors a real-world out-of-bailiwick delegation: zone
// example.test is delegated to NSs in a sibling zone different.test,
// so the example.test referral cannot legally carry A glue for those
// NSs. ResolveHostname walks via different.test to resolve the IPs;
// ProbeGlue must then self-query those resolved IPs.
//
// Before fix: source="self" absent. After fix: source="self" present.
func TestGlueProber_SelfRecordsWhenParentLacksGlue(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			// example.test → out-of-bailiwick NSs (no glue intentional)
			NS("example.test", "ns1.different.test"),
			NS("example.test", "ns2.different.test"),
			// different.test → its own NS with in-bailiwick glue
			NS("different.test", "ns.different.test"),
			A("ns.different.test", "127.240.0.4"),
		).
		Server("127.240.0.4:"+TestPort,
			// Authoritative for different.test: resolves the
			// example.test NS hostnames out-of-band.
			A("ns1.different.test", "127.240.0.2"),
			A("ns2.different.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.different.test"),
			NS("example.test", "ns2.different.test"),
			A("ns1.different.test", "127.240.0.2"),
			A("ns2.different.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.different.test"),
			NS("example.test", "ns2.different.test"),
			A("ns1.different.test", "127.240.0.2"),
			A("ns2.different.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Verification — parent-side NS records are present without
	// IPs (parent didn't supply glue; this is correct).
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.different.test.", "source", "parent"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.different.test.", "source", "parent"))

	// The regression check: self-side NS records MUST also be present
	// for both NSs after the resolved-nameservers fix. Pre-fix these
	// were silently missing.
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.different.test.", "source", "self"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.different.test.", "source", "self"))
}
