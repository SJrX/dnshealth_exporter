//go:build integration

package prober_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

// TestExtractDelegation_DedupesDuplicateParentGlue covers issue #26
// — when a parent returns the same A record twice for one NS
// hostname (legal per RFC, unusual in practice), extractDelegation
// MUST dedupe so downstream per-server counters don't over-report.
//
// Pre-fix: glueMap[name] = append(glueMap[name], ip) without a
// containment check produced two entries in NSRecords / Glue for
// the duplicated (host, IP) pair. cycle.Runner increments the
// per-server counter once per ProbeResult, so the duplicate
// inflated dnshealth_dns_queries_total for that IP.
//
// Post-fix: slices.Contains gates the append; only one entry per
// (host, IP) survives. The check guards against future "I'll just
// reorder this loop" regressions of the deduping behavior.
func TestExtractDelegation_DedupesDuplicateParentGlue(t *testing.T) {
	// Fixture Setup — root referral lists ns1 once but attaches the
	// SAME A record twice in its records slice. The fixture's
	// referral handler walks records and appends every A whose name
	// matches the NS hostname, so the on-wire Additional section
	// carries the duplicate, which is what the prober sees.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns1.example.test", "127.240.0.2"), // deliberate duplicate
		).
		Server("127.240.0.2:"+TestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	// Exercise SUT — call WalkDelegation directly so we can inspect
	// the deduped NSRecords / Glue slices without going through the
	// gauge registry (which would silently swallow the duplicate
	// gauge registration, hiding the bug under test).
	client := &dns.Client{Timeout: 2 * time.Second}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	delegation, err := prober.WalkDelegation(context.Background(), "example.test", client, logger)
	if err != nil {
		t.Fatalf("WalkDelegation: %v", err)
	}

	// Verification — exactly one NSRecords entry and one Glue entry
	// for the duplicated (host, IP). Pre-fix this assertion would
	// see two of each.
	nsRecordCount := countMatching(delegation.NSRecords, "ns1.example.test.", "127.240.0.2")
	if nsRecordCount != 1 {
		t.Errorf("NSRecords entries for (ns1.example.test., 127.240.0.2): got %d, want 1", nsRecordCount)
	}
	glueCount := countMatching(delegation.Glue, "ns1.example.test.", "127.240.0.2")
	if glueCount != 1 {
		t.Errorf("Glue entries for (ns1.example.test., 127.240.0.2): got %d, want 1", glueCount)
	}
}

func countMatching(ns []prober.Nameserver, host, ip string) int {
	var n int
	for _, entry := range ns {
		if entry.Hostname == host && entry.IP == ip {
			n++
		}
	}
	return n
}
