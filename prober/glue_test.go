//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

func TestGlueProber_ConsistentGlue(t *testing.T) {
	// Fixture Setup — parent and both NSes agree on NS records and glue
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort, // root/parent
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort, // ns1
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort, // ns2
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Verification — NS records present from both parent and self
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "source", "parent"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "source", "self"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "parent"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "self"))
}

func TestGlueProber_MismatchedNSRecords(t *testing.T) {
	// Fixture Setup — ns3 claims different NS records than the parent
	// Parent: ns1 + ns3
	// ns3 self-reports: ns1 + ns2 (not ns3!) — mismatch
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort, // root/parent
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns3.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns3.example.test", "127.240.0.4"),
		).
		Server("127.240.0.2:"+TestPort, // ns1
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns3.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns3.example.test", "127.240.0.4"),
		).
		Server("127.240.0.4:"+TestPort, // ns3 — mismatched
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Verification — ns2 appears with source=self (from ns3) but NOT source=parent
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "self"))
	AssertGaugeMissing(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "parent"))

	// ns3 appears with source=parent (root delegates to it)
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns3.example.test.", "source", "parent"))
}
