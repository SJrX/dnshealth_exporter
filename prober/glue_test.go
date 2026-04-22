//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	"github.com/sjr/dnshealth_exporter/testutil"
)

func TestGlueProber_ConsistentGlue(t *testing.T) {
	// Fixture Setup — parent and both NSes agree on NS records and glue
	env := testutil.NewDNSFixture(t).
		WriteZone("root", testutil.ZoneFile("example.test",
			testutil.SOA("example.test"),
			testutil.NS("example.test", "ns1.example.test"),
			testutil.NS("example.test", "ns2.example.test"),
			testutil.A("ns1.example.test", "127.240.0.2"),
			testutil.A("ns2.example.test", "127.240.0.3"),
		)).
		WriteZone("ns1", testutil.ZoneFile("example.test",
			testutil.SOA("example.test"),
			testutil.NS("example.test", "ns1.example.test"),
			testutil.NS("example.test", "ns2.example.test"),
			testutil.A("ns1.example.test", "127.240.0.2"),
			testutil.A("ns2.example.test", "127.240.0.3"),
		)).
		WriteZone("ns2", testutil.ZoneFile("example.test",
			testutil.SOA("example.test"),
			testutil.NS("example.test", "ns1.example.test"),
			testutil.NS("example.test", "ns2.example.test"),
			testutil.A("ns1.example.test", "127.240.0.2"),
			testutil.A("ns2.example.test", "127.240.0.3"),
		)).
		Reload(t)

	// Exercise SUT
	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Verification — NS records present from both parent and self
	testutil.AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		testutil.WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "source", "parent"))
	testutil.AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		testutil.WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "source", "self"))
	testutil.AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		testutil.WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "parent"))
	testutil.AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		testutil.WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "self"))
}

func TestGlueProber_MismatchedNSRecords(t *testing.T) {
	// Fixture Setup — ns3 claims different NS records than the parent
	env := testutil.NewDNSFixture(t).
		WriteZone("root", testutil.ZoneFile("example.test",
			testutil.SOA("example.test"),
			testutil.NS("example.test", "ns1.example.test"),
			testutil.NS("example.test", "ns3.example.test"),
			testutil.A("ns1.example.test", "127.240.0.2"),
			testutil.A("ns3.example.test", "127.240.0.4"),
		)).
		WriteZone("ns1", testutil.ZoneFile("example.test",
			testutil.SOA("example.test"),
			testutil.NS("example.test", "ns1.example.test"),
			testutil.NS("example.test", "ns3.example.test"),
			testutil.A("ns1.example.test", "127.240.0.2"),
			testutil.A("ns3.example.test", "127.240.0.4"),
		)).
		WriteZone("ns3", testutil.ZoneFile("example.test",
			testutil.SOA("example.test"),
			// ns3 claims ns1 and ns2 (not ns3!) — mismatch with parent
			testutil.NS("example.test", "ns1.example.test"),
			testutil.NS("example.test", "ns2.example.test"),
			testutil.A("ns1.example.test", "127.240.0.2"),
			testutil.A("ns2.example.test", "127.240.0.3"),
		)).
		Reload(t)

	// Exercise SUT
	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Verification — ns3 reports ns2 as self but parent doesn't know about ns2
	// Parent knows: ns1, ns3
	// ns3 self-reports: ns1, ns2
	// So ns2 appears with source=self but NOT source=parent (mismatch)
	testutil.AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		testutil.WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "self"))
	testutil.AssertGaugeMissing(t, metrics, "dnshealth_ns_record",
		testutil.WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "parent"))

	// And ns3 appears with source=parent but ns3 doesn't self-report ns3
	testutil.AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		testutil.WithLabels("zone", "example.test", "nameserver", "ns3.example.test.", "source", "parent"))
}
