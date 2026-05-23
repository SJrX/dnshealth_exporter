//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

// TestGlueProber_ParentV4OnlyGlue_OutOfBandAAAA — spec 006 US2
// scenario 3 (FR-011). The parent referral carries A glue only for
// in-bailiwick NSes — the most common real-world configuration when
// a TLD (or any parent) hasn't yet shipped AAAA glue for child zones
// whose NSes happen to have AAAA records. Pre-spec-006 the runner
// would skip ResolveHostnames whenever ANY parent IP was present;
// the AAAA records served by the auth would never reach the data
// model. Post-fix (FR-011 augmentation in cycle.runner /
// testutil.Probe): the runner detects the missing IP family and
// fires an out-of-band ResolveHostnames lookup; the resolver finds
// the AAAA at the auth and the v6 entries surface.
//
// Topology mirrors the user's real-world sjrx.net case: parent
// (root) advertises NS records for example.test with A glue only
// at 127.240.0.2 and 127.240.0.3. The auth servers there serve
// example.test and publish both A AND AAAA records for their own
// NS hostnames. The address-override pattern maps the v6 lookups
// back to v4 destinations so the test runs on a v4-only host.
func TestGlueProber_ParentV4OnlyGlue_OutOfBandAAAA(t *testing.T) {
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
			// Parent glue: A ONLY for each NS (no AAAA glue from
			// parent — the FR-011 case). This is the configuration
			// that under-reported pre-fix.
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			// Auth publishes BOTH families for itself — the v6
			// addresses parent failed to advertise as glue.
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

	// Parent-side v4 entries — from parent's A glue, expected
	// pre- and post-fix.
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "ip", "127.240.0.2", "source", "parent"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "ip", "127.240.0.3", "source", "parent"))

	// Self-side v4 entries — from auth's NS+A — expected pre-fix.
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "ip", "127.240.0.2", "source", "self"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "ip", "127.240.0.3", "source", "self"))

	// Self-side v6 entries — auth publishes AAAA; T007 makes
	// querySelfForNSAndA query both families and emit them. Pre-fix
	// these were silently absent.
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "ip", "2001:db8::2", "source", "self"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "ip", "2001:db8::3", "source", "self"))
}
