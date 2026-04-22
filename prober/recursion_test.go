//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	"github.com/sjr/dnshealth_exporter/testutil"
)

func TestRecursionProber_AuthoritativeRefusesRecursion(t *testing.T) {
	// Fixture Setup — CoreDNS is authoritative-only by default (no recursion)
	env := testutil.NewDNSFixture(t).
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
		WriteZone("root", testutil.ZoneFile("example.test",
			testutil.SOA("example.test"),
			testutil.NS("example.test", "ns1.example.test"),
			testutil.NS("example.test", "ns2.example.test"),
			testutil.A("ns1.example.test", "127.240.0.2"),
			testutil.A("ns2.example.test", "127.240.0.3"),
		)).
		Reload(t)

	// Exercise SUT
	metrics := env.Probe(prober.ProbeRecursion, "example.test")

	// Verification — CoreDNS authoritative should return RA=0
	testutil.AssertGauge(t, metrics, "dnshealth_ns_recursion_available",
		testutil.WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		testutil.WithValue(0))
	testutil.AssertGauge(t, metrics, "dnshealth_ns_recursion_available",
		testutil.WithLabels("zone", "example.test", "ip", "127.240.0.3"),
		testutil.WithValue(0))
}
