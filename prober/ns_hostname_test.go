//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

func TestNSHostnameProber_HappyPath(t *testing.T) {
	// Fixture Setup — NSs have valid syntax and no CNAME aliases.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeNSHostname, "example.test")

	// Verification — syntax valid, not a CNAME.
	AssertGauge(t, metrics, "dnshealth_ns_hostname_syntax_valid",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.",
			"ip", "127.240.0.2"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_hostname_is_cname",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.",
			"ip", "127.240.0.2"),
		WithValue(0))
}

func TestNSHostnameProber_SyntaxInvalid(t *testing.T) {
	// Fixture Setup — NS hostname contains an underscore, which is
	// disallowed by RFC 952 / RFC 1123 for hostnames. The NS RR
	// itself parses (DNS labels permit underscores), but the
	// syntax-validity check should flag it.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "bad_ns.example.test"),
			A("bad_ns.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "bad_ns.example.test"),
			A("bad_ns.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeNSHostname, "example.test")

	// Verification — syntax invalid; CNAME check still passes.
	AssertGauge(t, metrics, "dnshealth_ns_hostname_syntax_valid",
		WithLabels("zone", "example.test", "nameserver", "bad_ns.example.test.",
			"ip", "127.240.0.2"),
		WithValue(0))
	AssertGauge(t, metrics, "dnshealth_ns_hostname_is_cname",
		WithLabels("zone", "example.test", "nameserver", "bad_ns.example.test.",
			"ip", "127.240.0.2"),
		WithValue(0))
}

func TestNSHostnameProber_IsCNAME(t *testing.T) {
	// Fixture Setup — NS hostname is a CNAME pointing at a real host.
	// The CNAME-walk should walk root → example.test. auth, query
	// CNAME for "alias.example.test.", and find the CNAME RR.
	//
	// The parent referral still has to be walkable, so the prober
	// has nameservers to iterate. We give it an A record alongside
	// the CNAME (a "white lie" — strictly the auth shouldn't serve
	// both, but it's how we make the delegation walker happy without
	// resolving the alias chain itself).
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "alias.example.test"),
			A("alias.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "alias.example.test"),
			A("alias.example.test", "127.240.0.2"),
			CNAME("alias.example.test", "real.example.test"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeNSHostname, "example.test")

	// Verification — syntax valid; CNAME presence detected.
	AssertGauge(t, metrics, "dnshealth_ns_hostname_syntax_valid",
		WithLabels("zone", "example.test", "nameserver", "alias.example.test.",
			"ip", "127.240.0.2"),
		WithValue(1))
	AssertGauge(t, metrics, "dnshealth_ns_hostname_is_cname",
		WithLabels("zone", "example.test", "nameserver", "alias.example.test.",
			"ip", "127.240.0.2"),
		WithValue(1))
}
