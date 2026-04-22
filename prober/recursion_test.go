//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

func TestRecursionProber_AuthoritativeRefusesRecursion(t *testing.T) {
	// Fixture Setup — in-process miekg/dns servers are authoritative-only
	// by default (they never set RA in responses)
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
	metrics := env.Probe(prober.ProbeRecursion, "example.test")

	// Verification — authoritative servers should return RA=0
	AssertGauge(t, metrics, "dnshealth_ns_recursion_available",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(0))
	AssertGauge(t, metrics, "dnshealth_ns_recursion_available",
		WithLabels("zone", "example.test", "ip", "127.240.0.3"),
		WithValue(0))
}

func TestRecursionProber_RecursiveServerDetected(t *testing.T) {
	// Fixture Setup — ns2 allows recursion (sets RA in response)
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
		ServerWithOptions("127.240.0.3:"+TestPort, ServerOptions{RecursionAvailable: true},
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeRecursion, "example.test")

	// Verification — ns1 refuses, ns2 allows
	AssertGauge(t, metrics, "dnshealth_ns_recursion_available",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(0))
	AssertGauge(t, metrics, "dnshealth_ns_recursion_available",
		WithLabels("zone", "example.test", "ip", "127.240.0.3"),
		WithValue(1))
}

// Ensure the test server handler respects RecursionAvailable option.
func TestRecursionProber_MixedRecursionAcrossNameservers(t *testing.T) {
	// Fixture Setup — 3 nameservers, only the middle one allows recursion
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			NS("example.test", "ns3.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
			A("ns3.example.test", "127.240.0.4"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			NS("example.test", "ns3.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
			A("ns3.example.test", "127.240.0.4"),
		).
		ServerWithOptions("127.240.0.3:"+TestPort, ServerOptions{RecursionAvailable: true},
			SOA("example.test"),
		).
		Server("127.240.0.4:"+TestPort,
			SOA("example.test"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeRecursion, "example.test")

	// Verification
	AssertGauge(t, metrics, "dnshealth_ns_recursion_available",
		WithLabels("ip", "127.240.0.2"), WithValue(0))
	AssertGauge(t, metrics, "dnshealth_ns_recursion_available",
		WithLabels("ip", "127.240.0.3"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_recursion_available",
		WithLabels("ip", "127.240.0.4"), WithValue(0))
}

