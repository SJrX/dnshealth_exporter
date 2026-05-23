//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

// TestGlueProber_IPv6OnlyNameservers is the spec 006 / #23 regression
// check for the original symptom: a zone whose parent delegation carries
// only AAAA glue (no A) is silently absent from per-NS metric series
// pre-fix. After T004 (ResolveHostnames), T007 (querySelfForNSAndA over
// both A+AAAA), and T009b (extractDelegation extracts AAAA), every per-
// (NS, IP) series surfaces with the v6 address in the ip label.
//
// Topology: the parent's referral for example.test points at in-
// bailiwick NSes ns1.example.test and ns2.example.test with AAAA-only
// glue (2001:db8::2, 2001:db8::3). The auth servers actually run on v4
// loopback addresses (127.240.0.2, 127.240.0.3). prober.ResolveAddress
// is overridden for the test to map the v6 keys to v4 destinations —
// exercising the FR-014 invariant: the network connection goes to v4,
// the metric label carries the v6 address.
//
// This is exactly the topology a deployment cannot reach (no v6
// connectivity on the test host) while still exercising every v6 code
// path the exporter has — the same trick the demo uses per FR-015.
func TestGlueProber_IPv6OnlyNameservers(t *testing.T) {
	// Fixture Setup — address-override redirect for the v6 glue.
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
		ReferralServer("127.240.0.1:"+TestPort, // root/parent
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			// AAAA glue ONLY — the original #23 failure mode.
			AAAA("ns1.example.test", "2001:db8::2"),
			AAAA("ns2.example.test", "2001:db8::3"),
		).
		Server("127.240.0.2:"+TestPort, // ns1, reached over v4 via override
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			// Self-side AAAA records — querySelfForNSAndA's AAAA query
			// against this server returns these.
			AAAA("ns1.example.test", "2001:db8::2"),
			AAAA("ns2.example.test", "2001:db8::3"),
		).
		Server("127.240.0.3:"+TestPort, // ns2
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			AAAA("ns1.example.test", "2001:db8::2"),
			AAAA("ns2.example.test", "2001:db8::3"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Verification — parent-side v6 entries.
	// (Pre-T009b: extractDelegation discarded AAAA glue, so these
	// series did not exist. Post-fix they do.)
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "ip", "2001:db8::2", "source", "parent"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "ip", "2001:db8::3", "source", "parent"))

	// Verification — self-side v6 entries.
	// (Pre-T007: querySelfForNSAndA queried only TypeA, so no
	// source="self" series for v6-only hostnames. Post-fix they do.)
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "ip", "2001:db8::2", "source", "self"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns2.example.test.", "ip", "2001:db8::3", "source", "self"))
}
