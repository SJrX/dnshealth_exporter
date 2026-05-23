//go:build integration

package prober_test

import (
	"testing"

	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

// TestGlueProber_MultiANameserver — spec 006 US2 scenario 2.
// A single NS hostname (e.g., from an anycast cluster) has two A
// records in the parent's glue. Each address surfaces as its own
// per-NS series.
//
// Pre-spec-006: extractDelegation's `glueMap[name] = ip` last-write-
// wins discarded all but one A address per hostname. Post-fix:
// glueMap is a slice and emits one Nameserver per (hostname, IP).
func TestGlueProber_MultiANameserver(t *testing.T) {
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			// Two distinct A records for the same NS hostname.
			A("ns1.example.test", "127.240.0.2"),
			A("ns1.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns1.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns1.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	metrics := env.Probe(prober.ProbeGlue, "example.test")

	// Both addresses must appear in parent-side series.
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "ip", "127.240.0.2", "source", "parent"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "ip", "127.240.0.3", "source", "parent"))

	// And in self-side series. Each address probes its own auth
	// server, each auth server reports both A records, so each
	// (hostname, IP) pair becomes a source="self" entry. The
	// `registered` dedup map in ProbeGlue keeps each (hostname, IP,
	// source) tuple to one entry.
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "ip", "127.240.0.2", "source", "self"))
	AssertGaugeExists(t, metrics, "dnshealth_ns_record",
		WithLabels("zone", "example.test", "nameserver", "ns1.example.test.", "ip", "127.240.0.3", "source", "self"))
}
