//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

func TestSOAProber_ReturnsSerialFromNameservers(t *testing.T) {
	// Fixture Setup — root delegates to ns1 + ns2, both serve same serial
	env := NewDNSFixture(t).
		Server("127.240.0.1:"+TestPort,
			SOA("example.test", Serial(100)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test", Serial(100)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test", Serial(100)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification
	AssertGauge(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(100))
	AssertGauge(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.3"),
		WithValue(100))
}

func TestSOAProber_DetectsSerialDrift(t *testing.T) {
	// Fixture Setup — ns1 and ns2 have different serials
	env := NewDNSFixture(t).
		Server("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test", Serial(200)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test", Serial(1)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification — different serials per nameserver
	AssertGauge(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(200))
	AssertGauge(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.3"),
		WithValue(1))
}
