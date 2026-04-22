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
		Server("127.240.0.1:"+TestPort,
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
