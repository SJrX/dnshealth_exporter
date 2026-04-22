//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	"github.com/sjr/dnshealth_exporter/testutil"
)

func TestSOAProber_ReturnsSerialFromNameservers(t *testing.T) {
	// Fixture Setup — both nameservers serve same zone with same serial
	env := testutil.NewDNSFixture(t).
		WriteZone("ns1", testutil.ZoneFile("example.test",
			testutil.SOA("example.test", testutil.Serial(2026042101)),
			testutil.NS("example.test", "ns1.example.test"),
			testutil.NS("example.test", "ns2.example.test"),
			testutil.A("ns1.example.test", "127.240.0.2"),
			testutil.A("ns2.example.test", "127.240.0.3"),
		)).
		WriteZone("ns2", testutil.ZoneFile("example.test",
			testutil.SOA("example.test", testutil.Serial(2026042101)),
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
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification
	testutil.AssertGauge(t, metrics, "dnshealth_soa_serial",
		testutil.WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		testutil.WithValue(2026042101))
	testutil.AssertGauge(t, metrics, "dnshealth_soa_serial",
		testutil.WithLabels("zone", "example.test", "ip", "127.240.0.3"),
		testutil.WithValue(2026042101))
}

func TestSOAProber_DetectsSerialDrift(t *testing.T) {
	// Fixture Setup — ns1 and ns2 have different serials
	env := testutil.NewDNSFixture(t).
		WriteZone("ns1", testutil.ZoneFile("example.test",
			testutil.SOA("example.test", testutil.Serial(2026042101)),
			testutil.NS("example.test", "ns1.example.test"),
			testutil.NS("example.test", "ns2.example.test"),
			testutil.A("ns1.example.test", "127.240.0.2"),
			testutil.A("ns2.example.test", "127.240.0.3"),
		)).
		WriteZone("ns2", testutil.ZoneFile("example.test",
			testutil.SOA("example.test", testutil.Serial(1)),
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
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification — different serials per nameserver
	testutil.AssertGauge(t, metrics, "dnshealth_soa_serial",
		testutil.WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		testutil.WithValue(2026042101))
	testutil.AssertGauge(t, metrics, "dnshealth_soa_serial",
		testutil.WithLabels("zone", "example.test", "ip", "127.240.0.3"),
		testutil.WithValue(1))
}
