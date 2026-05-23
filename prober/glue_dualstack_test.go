//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

// TestGlueProber_DualStackNameservers — spec 006 US2 scenario 1.
// Every NS hostname has BOTH an A and an AAAA record in the parent's
// glue (the common modern dual-stack case). Each NS appears as two
// series in every per-NS metric — one per IP family — and the IP
// label distinguishes them.
//
// Pre-spec-006: extractDelegation discarded AAAA glue (only the v4
// entry surfaced); the dual-stack case under-reported every metric
// family by 50%. Post-fix: 4 series per metric (2 NSes × 2 IPs).
func TestGlueProber_DualStackNameservers(t *testing.T) {
	// Address-override redirects v6 glue addresses to the v4 sockets
	// where the auth servers actually run — same trick as the v6-only
	// test, with the difference being parent ALSO provides A glue
	// here (not just AAAA).
	saved := prober.ResolveAddress
	prober.ResolveAddress = func(ip string) string {
		switch ip {
		case "2001:db8::2":
			return "127.240.0.2:" + TestPort
		case "2001:db8::3":
			return "127.240.0.3:" + TestPort
		default:
			return saved(ip)
		}
	}
	t.Cleanup(func() { prober.ResolveAddress = saved })

	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			// Dual-stack glue — both A AND AAAA for each NS.
			A("ns1.example.test", "127.240.0.2"),
			AAAA("ns1.example.test", "2001:db8::2"),
			A("ns2.example.test", "127.240.0.3"),
			AAAA("ns2.example.test", "2001:db8::3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			AAAA("ns1.example.test", "2001:db8::2"),
			A("ns2.example.test", "127.240.0.3"),
			AAAA("ns2.example.test", "2001:db8::3"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			AAAA("ns1.example.test", "2001:db8::2"),
			A("ns2.example.test", "127.240.0.3"),
			AAAA("ns2.example.test", "2001:db8::3"),
		).
		Start(t)
	defer env.Stop()

	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Each NS has 2 entries per source. Verify all 4 parent + 4 self.
	for _, ns := range []struct{ host, v4, v6 string }{
		{"ns1.example.test.", "127.240.0.2", "2001:db8::2"},
		{"ns2.example.test.", "127.240.0.3", "2001:db8::3"},
	} {
		AssertGaugeExists(t, metrics, "dnshealth_ns_record",
			WithLabels("zone", "example.test", "nameserver", ns.host, "ip", ns.v4, "source", "parent"))
		AssertGaugeExists(t, metrics, "dnshealth_ns_record",
			WithLabels("zone", "example.test", "nameserver", ns.host, "ip", ns.v6, "source", "parent"))
		AssertGaugeExists(t, metrics, "dnshealth_ns_record",
			WithLabels("zone", "example.test", "nameserver", ns.host, "ip", ns.v4, "source", "self"))
		AssertGaugeExists(t, metrics, "dnshealth_ns_record",
			WithLabels("zone", "example.test", "nameserver", ns.host, "ip", ns.v6, "source", "self"))
	}
}
