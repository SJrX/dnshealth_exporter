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
		ReferralServer("127.240.0.1:"+TestPort,
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

func TestSOAProber_ReturnsAllSOAFields(t *testing.T) {
	// Fixture Setup — verify all SOA fields are exposed, not just serial
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test", Serial(42), Refresh(7200), Retry(900), Expire(1209600), Minttl(600)),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification — every SOA field exposed as a gauge
	labels := []string{"zone", "example.test", "ip", "127.240.0.2"}
	AssertGauge(t, metrics, "dnshealth_soa_serial", WithLabels(labels...), WithValue(42))
	AssertGauge(t, metrics, "dnshealth_soa_refresh_seconds", WithLabels(labels...), WithValue(7200))
	AssertGauge(t, metrics, "dnshealth_soa_retry_seconds", WithLabels(labels...), WithValue(900))
	AssertGauge(t, metrics, "dnshealth_soa_expire_seconds", WithLabels(labels...), WithValue(1209600))
	AssertGauge(t, metrics, "dnshealth_soa_minimum_seconds", WithLabels(labels...), WithValue(600))
}

func TestSOAProber_DetectsSerialDrift(t *testing.T) {
	// Fixture Setup — ns1 and ns2 have different serials
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
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

func TestSOAProber_NameserverReturnsNoSOARecord(t *testing.T) {
	// Fixture Setup — ns1 serves the zone but has no SOA record
	// (like mirski.ca on ns0.sjrx.net — zone not loaded)
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			// Has NS and A but no SOA — simulates a misconfigured NS
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification — no SOA field metrics, but query_success=0 reported
	AssertGaugeMissing(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"))
	AssertGauge(t, metrics, "dnshealth_query_success",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(0))
}

func TestSOAProber_SingleNameserver(t *testing.T) {
	// Fixture Setup — zone with only one NS
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test", Serial(999)),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification — works with a single nameserver
	AssertGauge(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(999))
}

func TestSOAProber_NoGlueFromParent(t *testing.T) {
	// Fixture Setup — parent delegates without glue (NS hostnames
	// in a different zone). A separate server hosts the NS hostname zone.
	// Simulates .ca delegating to ns1.otherdomain.net without glue.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			// Root: delegates example.test without glue,
			// and knows how to refer other.test queries
			NS("example.test", "ns1.other.test"),
			NS("other.test", "ns1.other.test"),
			A("ns1.other.test", "127.240.0.6"),
		).
		Server("127.240.0.6:"+TestPort,
			// Authoritative for other.test — serves A record for ns1.other.test
			SOA("other.test"),
			NS("other.test", "ns1.other.test"),
			A("ns1.other.test", "127.240.0.6"),
			// Also serves example.test (it IS ns1.other.test)
			SOA("example.test", Serial(777)),
			NS("example.test", "ns1.other.test"),
			A("ns1.other.test", "127.240.0.6"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification — resolved the NS hostname despite no glue
	AssertGauge(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.6"),
		WithValue(777))
}

func TestSOAProber_NameserverTimesOut(t *testing.T) {
	// Fixture Setup — ns2 drops all queries (simulates unreachable)
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test", Serial(42)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		ServerWithOptions("127.240.0.3:"+TestPort, ServerOptions{Drop: true},
			SOA("example.test", Serial(42)),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT — should not hang or crash, ns1 still works
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification — ns1 succeeds, ns2 fails
	AssertGauge(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(42))
	AssertGauge(t, metrics, "dnshealth_query_success",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_query_success",
		WithLabels("zone", "example.test", "ip", "127.240.0.3"),
		WithValue(0))
	AssertGaugeMissing(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.3"))

	// Duration metrics exist for both — ns1 is fast, ns2 is ~1s (timeout)
	AssertGaugeExists(t, metrics, "dnshealth_query_duration_seconds",
		WithLabels("ip", "127.240.0.2"))
	AssertGaugeInRange(t, metrics, "dnshealth_query_duration_seconds",
		[]MetricOption{WithLabels("ip", "127.240.0.3")},
		0.5, 2.0) // timeout is 1s, allow some slack
}

func TestSOAProber_NameserverReturnsNXDOMAIN(t *testing.T) {
	// Fixture Setup — ns1 returns NXDOMAIN for the zone
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		ServerWithOptions("127.240.0.2:"+TestPort, ServerOptions{Rcode: 3}, // NXDOMAIN
			SOA("example.test"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT — should not crash
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification — query_success=0, no SOA field metrics
	AssertGauge(t, metrics, "dnshealth_query_success",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(0))
	AssertGaugeMissing(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"))
}

func TestSOAProber_NameserverReturnsGarbage(t *testing.T) {
	// Fixture Setup — ns1 sends garbage bytes
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		ServerWithOptions("127.240.0.2:"+TestPort, ServerOptions{Garbage: true}).
		Start(t)
	defer env.Stop()

	// Exercise SUT — should not crash
	metrics := env.Probe(prober.ProbeSOA, "example.test")

	// Verification — query_success=0, no SOA field metrics
	AssertGauge(t, metrics, "dnshealth_query_success",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(0))
	AssertGaugeMissing(t, metrics, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"))
}
