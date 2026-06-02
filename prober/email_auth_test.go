//go:build integration

package prober_test

import (
	"testing"

	"github.com/miekg/dns"
	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

// TestEmailAuth_HealthySPFandDMARC — a fully configured zone: a single
// SPF record ending -all and a DMARC record with p=reject.
func TestEmailAuth_HealthySPFandDMARC(t *testing.T) {
	// Fixture Setup
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
			TXT("example.test", "v=spf1 include:_spf.example.net -all"),
			TXT("_dmarc.example.test", "v=DMARC1; p=reject; rua=mailto:dmarc@example.test"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	// Verification
	AssertGauge(t, metrics, "dnshealth_spf_present", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_spf_record_count", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_spf_valid", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_spf_terminal_all",
		WithLabels("zone", "example.test", "qualifier", "fail"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_dmarc_present", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_dmarc_valid", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_dmarc_policy",
		WithLabels("zone", "example.test", "policy", "reject"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_dmarc_rua_present", WithLabels("zone", "example.test"), WithValue(1))
}

// TestEmailAuth_NoRecords — a zone publishing neither SPF nor DMARC. The
// boolean gauges must be zero-emitted (present as 0), and the info gauges
// must be absent, so the dashboard reads WARN (absent) / N/A.
func TestEmailAuth_NoRecords(t *testing.T) {
	// Fixture Setup
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
	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	// Verification — booleans read 0, info gauges absent.
	AssertGauge(t, metrics, "dnshealth_spf_present", WithLabels("zone", "example.test"), WithValue(0))
	AssertGauge(t, metrics, "dnshealth_dmarc_present", WithLabels("zone", "example.test"), WithValue(0))
	AssertGaugeMissing(t, metrics, "dnshealth_spf_terminal_all", WithLabels("zone", "example.test"))
	AssertGaugeMissing(t, metrics, "dnshealth_dmarc_policy", WithLabels("zone", "example.test"))
}

// TestEmailAuth_PermissivePlusAll — SPF +all (valid record, reckless
// policy). The qualifier must surface as "pass" so the dashboard WARNs.
func TestEmailAuth_PermissivePlusAll(t *testing.T) {
	// Fixture Setup
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
			TXT("example.test", "v=spf1 +all"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	// Verification
	AssertGauge(t, metrics, "dnshealth_spf_valid", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_spf_terminal_all",
		WithLabels("zone", "example.test", "qualifier", "pass"), WithValue(1))
}

// TestEmailAuth_MultipleSPFRecords — two v=spf1 records (RFC 7208 §3.2
// PermError). record_count=2, valid=0, and no terminal-qualifier gauge.
func TestEmailAuth_MultipleSPFRecords(t *testing.T) {
	// Fixture Setup
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
			TXT("example.test", "v=spf1 -all"),
			TXT("example.test", "v=spf1 include:_spf.example.net ~all"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	// Verification
	AssertGauge(t, metrics, "dnshealth_spf_present", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_spf_record_count", WithLabels("zone", "example.test"), WithValue(2))
	AssertGauge(t, metrics, "dnshealth_spf_valid", WithLabels("zone", "example.test"), WithValue(0))
	AssertGaugeMissing(t, metrics, "dnshealth_spf_terminal_all", WithLabels("zone", "example.test"))
}

// TestEmailAuth_MalformedDMARC — a v=DMARC1 record with no p= tag.
// present=1, valid=0, and no policy gauge.
func TestEmailAuth_MalformedDMARC(t *testing.T) {
	// Fixture Setup
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
			TXT("_dmarc.example.test", "v=DMARC1; rua=mailto:dmarc@example.test"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	// Verification
	AssertGauge(t, metrics, "dnshealth_dmarc_present", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_dmarc_valid", WithLabels("zone", "example.test"), WithValue(0))
	AssertGaugeMissing(t, metrics, "dnshealth_dmarc_policy", WithLabels("zone", "example.test"))
}

// TestEmailAuth_FailsOverOnServfail — when the first nameserver returns
// SERVFAIL for the TXT query, the prober must try the next nameserver
// rather than misreporting the record as absent. ns1 (127.240.0.2)
// SERVFAILs everything; ns2 (127.240.0.3) serves the real SPF record.
func TestEmailAuth_FailsOverOnServfail(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		ServerWithOptions("127.240.0.2:"+TestPort, ServerOptions{Rcode: dns.RcodeServerFailure}).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
			TXT("example.test", "v=spf1 -all"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	// Verification — the SERVFAIL from ns1 must NOT be read as "no SPF";
	// the prober fails over to ns2 and finds the record.
	AssertGauge(t, metrics, "dnshealth_spf_present", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_spf_terminal_all",
		WithLabels("zone", "example.test", "qualifier", "fail"), WithValue(1))
}

// TestEmailAuth_MultiStringSPF — a long SPF record published as two
// character-strings must be concatenated before parsing (RFC 7208 §3.3),
// not treated as two separate records.
func TestEmailAuth_MultiStringSPF(t *testing.T) {
	// Fixture Setup
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
			TXT("example.test", "v=spf1 ip4:192.0.2.0/24 ", "include:_spf.example.net -all"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	// Verification — concatenated into one valid record ending -all.
	AssertGauge(t, metrics, "dnshealth_spf_record_count", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_spf_valid", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_spf_terminal_all",
		WithLabels("zone", "example.test", "qualifier", "fail"), WithValue(1))
}
