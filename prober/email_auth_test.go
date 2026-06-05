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

// TestEmailAuth_LookupBudget_OverBudget — apex SPF chains include: to
// in-zone sub-records totalling >10 lookups (spec 010 US1). The walk
// resolves them from root, reaches 11, and stops.
func TestEmailAuth_LookupBudget_OverBudget(t *testing.T) {
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
			TXT("example.test", "v=spf1 include:_a.example.test include:_b.example.test include:_c.example.test -all"),
			TXT("_a.example.test", "v=spf1 a mx a mx -all"), // 4
			TXT("_b.example.test", "v=spf1 a mx a mx -all"), // 4
			TXT("_c.example.test", "v=spf1 a mx -all"),      // 2
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT
	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	// Verification — 3 includes + 4 + 4 = 11 (stop), exceeded, complete.
	AssertGauge(t, metrics, "dnshealth_spf_lookup_count", WithLabels("zone", "example.test"), WithValue(11))
	AssertGauge(t, metrics, "dnshealth_spf_lookup_budget_exceeded", WithLabels("zone", "example.test"), WithValue(1))
	AssertGauge(t, metrics, "dnshealth_spf_lookup_eval_complete", WithLabels("zone", "example.test"), WithValue(1))
}

// TestEmailAuth_LookupBudget_InBudget — a single valid SPF record with no
// lookup mechanisms reads count 0, not exceeded. No-SPF / multiple-SPF
// zones emit no lookup gauges (→ dashboard N/A).
func TestEmailAuth_LookupBudget_InBudget(t *testing.T) {
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
		).
		Start(t)
	defer env.Stop()

	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	AssertGauge(t, metrics, "dnshealth_spf_lookup_count", WithLabels("zone", "example.test"), WithValue(0))
	AssertGauge(t, metrics, "dnshealth_spf_lookup_budget_exceeded", WithLabels("zone", "example.test"), WithValue(0))
	AssertGauge(t, metrics, "dnshealth_spf_lookup_eval_complete", WithLabels("zone", "example.test"), WithValue(1))
}

// TestEmailAuth_LookupBudget_NoSPF — a zone with no SPF record emits NO
// lookup gauges (dashboard reads N/A via absent()).
func TestEmailAuth_LookupBudget_NoSPF(t *testing.T) {
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

	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	AssertGaugeMissing(t, metrics, "dnshealth_spf_lookup_count", WithLabels("zone", "example.test"))
	AssertGaugeMissing(t, metrics, "dnshealth_spf_lookup_budget_exceeded", WithLabels("zone", "example.test"))
}

// TestEmailAuth_LookupBudget_UnreachableInclude (T013a / US2) — an
// include: to a name with no SPF record sets eval_complete=0 and does
// NOT report over budget (no false FAIL from an unresolvable include).
func TestEmailAuth_LookupBudget_UnreachableInclude(t *testing.T) {
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
			// _missing.example.test is NOT served → resolves to no SPF.
			TXT("example.test", "v=spf1 include:_missing.example.test a -all"),
		).
		Start(t)
	defer env.Stop()

	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	AssertGauge(t, metrics, "dnshealth_spf_lookup_eval_complete", WithLabels("zone", "example.test"), WithValue(0))
	AssertGauge(t, metrics, "dnshealth_spf_lookup_budget_exceeded", WithLabels("zone", "example.test"), WithValue(0))
	AssertGauge(t, metrics, "dnshealth_spf_lookup_count", WithLabels("zone", "example.test"), WithValue(2)) // include(1) + a(1)
}

// TestEmailAuth_LookupBudget_SlowIncludeBounded (T013b / FR-006 / SC-004)
// — an include: to a target whose authoritative server never answers
// (Drop) must not hang the probe or raise a false FAIL: the query times
// out, the resolver gives up, eval_complete=0, budget_exceeded=0. The
// main zone answers normally, so the apex SPF still resolves.
func TestEmailAuth_LookupBudget_SlowIncludeBounded(t *testing.T) {
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			// Delegate a second zone whose auth server drops everything.
			NS("dropzone.test", "ns1.dropzone.test"),
			A("ns1.dropzone.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			TXT("example.test", "v=spf1 include:_x.dropzone.test a -all"),
		).
		ServerWithOptions("127.240.0.3:"+TestPort, ServerOptions{Drop: true}).
		Start(t)
	defer env.Stop()

	metrics := env.Probe(prober.ProbeEmailAuth, "example.test")

	// The probe returned (did not hang); the unreachable include is a
	// lower-bound caveat, not a FAIL.
	AssertGauge(t, metrics, "dnshealth_spf_lookup_eval_complete", WithLabels("zone", "example.test"), WithValue(0))
	AssertGauge(t, metrics, "dnshealth_spf_lookup_budget_exceeded", WithLabels("zone", "example.test"), WithValue(0))
}
