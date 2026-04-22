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

	// Glue also matches
	AssertGaugeExists(t, metrics, "dnshealth_ns_glue",
		WithLabels("nameserver", "ns1.example.test.", "ip", "127.240.0.2", "source", "parent"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_glue",
		WithLabels("nameserver", "ns1.example.test.", "ip", "127.240.0.2", "source", "self"))
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

	// Verification ��� ns2 appears with source=self (from ns3) but NOT source=parent
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "self"))
	AssertGaugeMissing(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "source", "parent"))

	// ns3 appears with source=parent (root delegates to it)
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns3.example.test.", "source", "parent"))
}

func TestGlueProber_GlueIPMismatch(t *testing.T) {
	// Fixture Setup — parent says ns1 is at 127.240.0.2, but ns1's
	// own A record says it's at 127.240.0.9 (different IP)
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"), // parent's glue
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.9"), // ns1 claims different IP
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Verification — parent glue says 127.240.0.2
	AssertGaugeExists(t, metrics, "dnshealth_ns_glue",
		WithLabels("nameserver", "ns1.example.test.", "ip", "127.240.0.2", "source", "parent"))

	// ns1 self-reports 127.240.0.9
	AssertGaugeExists(t, metrics, "dnshealth_ns_glue",
		WithLabels("nameserver", "ns1.example.test.", "ip", "127.240.0.9", "source", "self"))

	// The mismatch is detectable: 127.240.0.2 has source=parent but not self,
	// 127.240.0.9 has source=self but not parent
	AssertGaugeMissing(t, metrics, "dnshealth_ns_glue",
		WithLabels("nameserver", "ns1.example.test.", "ip", "127.240.0.2", "source", "self"))
	AssertGaugeMissing(t, metrics, "dnshealth_ns_glue",
		WithLabels("nameserver", "ns1.example.test.", "ip", "127.240.0.9", "source", "parent"))
}

func TestGlueProber_NoGlueFromParent(t *testing.T) {
	// Fixture Setup — TLD delegates without glue (NS hostnames in a
	// different zone). 3-level hierarchy: root → TLD (.test) → zone.
	// The TLD knows about example.test and other.test but provides
	// NO glue A records for example.test's NS (ns1.other.test).
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			// Root: refers .test queries to TLD server
			NS("test.", "tld.test."),
			A("tld.test.", "127.240.0.5"),
		).
		ReferralServer("127.240.0.5:"+TestPort,
			// TLD: delegates example.test to ns1.other.test WITHOUT glue,
			// and delegates other.test to ns1.other.test WITH glue
			NS("example.test", "ns1.other.test"),
			// No A("ns1.other.test") here — that's the no-glue case
			NS("other.test", "ns1.other.test"),
			A("ns1.other.test", "127.240.0.6"),
		).
		Server("127.240.0.6:"+TestPort,
			// Authoritative for other.test and example.test
			SOA("other.test"),
			NS("other.test", "ns1.other.test"),
			A("ns1.other.test", "127.240.0.6"),
			SOA("example.test"),
			NS("example.test", "ns1.other.test"),
			A("ns1.other.test", "127.240.0.6"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Verification — parent has NS record but no glue for ns1.other.test
	// when delegating example.test (the A record was in the other.test
	// delegation, not the example.test one)
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("nameserver", "ns1.other.test.", "source", "parent"))

	// Self-query still works (hostname resolved via delegation walk)
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("nameserver", "ns1.other.test.", "source", "self"))
}

func TestGlueProber_PartialGlue(t *testing.T) {
	// Fixture Setup — TLD has glue for ns1 (same zone) but not ns2
	// (different zone). 3-level hierarchy like the no-glue test.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			// Root: refers .test to TLD
			NS("test.", "tld.test."),
			A("tld.test.", "127.240.0.5"),
		).
		ReferralServer("127.240.0.5:"+TestPort,
			// TLD: delegates example.test with glue for ns1 (same zone)
			// but no glue for ns2.other.test (different zone)
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.other.test"),
			A("ns1.example.test", "127.240.0.2"),
			// ns2.other.test has no glue here
			// But TLD knows other.test
			NS("other.test", "ns2.other.test"),
			A("ns2.other.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.other.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.other.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.other.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.other.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Verification — ns1 has glue from parent (same zone), ns2 does not
	AssertGaugeExists(t, metrics, "dnshealth_ns_glue",
		WithLabels("nameserver", "ns1.example.test.", "source", "parent"))

	// Both nameservers should still have self-reported records
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("nameserver", "ns1.example.test.", "source", "self"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("nameserver", "ns2.other.test.", "source", "self"))
}
