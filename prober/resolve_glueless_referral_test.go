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

// TestResolveOneFamily_GluelessReferralFallback covers issue #27.
//
// Pre-fix, resolveOneFamily silently returned (nil, nil) when an
// intermediate referral named an out-of-zone NS without glue —
// indistinguishable from a real NODATA. Caller dropped the hostname.
// Post-fix, resolveOneFamily falls back to an out-of-band walk to
// find the referral NS's IP, then continues the original walk.
//
// Topology:
//
//   - Root (127.240.0.1) holds two delegations:
//   - outside.test → nameserver.zone-x.test (NS in a DIFFERENT
//     zone, and root holds no A record for nameserver.zone-x.test,
//     so the fixture attaches NO glue — the bug trigger).
//   - zone-x.test → ns.zone-x.test (in-zone NS, root holds the A,
//     glue ships — gives the fallback walk a way through).
//   - 127.240.0.3 is authoritative for zone-x.test and returns A
//     for nameserver.zone-x.test = 127.240.0.2.
//   - 127.240.0.2 is authoritative for outside.test and returns A
//     for target.outside.test = 127.240.0.99.
//
// The resolve walk for target.outside.test now: query root → no
// glue for outside.test's referral → fallback resolves
// nameserver.zone-x.test via root → zone-x.test (127.240.0.3) →
// returns 127.240.0.2 → outer walk asks 127.240.0.2 → returns
// 127.240.0.99.
func TestResolveOneFamily_GluelessReferralFallback(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			// outside.test delegation — no glue for its NS (the bug
			// trigger: NS hostname is out-of-zone, no A in records).
			NS("outside.test", "nameserver.zone-x.test"),
			// zone-x.test delegation — in-zone NS with glue.
			NS("zone-x.test", "ns.zone-x.test"),
			A("ns.zone-x.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+TestPort,
			// Authoritative for zone-x.test.
			SOA("zone-x.test"),
			NS("zone-x.test", "ns.zone-x.test"),
			A("ns.zone-x.test", "127.240.0.3"),
			// The crucial record: A for the out-of-zone NS that
			// outside.test's referral pointed at. This is what the
			// fallback walk has to find.
			A("nameserver.zone-x.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+TestPort,
			// Authoritative for outside.test (this is what
			// "nameserver.zone-x.test" actually serves).
			SOA("outside.test"),
			NS("outside.test", "nameserver.zone-x.test"),
			A("target.outside.test", "127.240.0.99"),
		).
		Start(t)
	defer env.Stop()

	client := &dns.Client{Timeout: 2 * time.Second}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Exercise SUT
	ips, err := prober.ResolveHostnames(context.Background(), "target.outside.test", client, logger)

	// Verification
	if err != nil {
		t.Fatalf("ResolveHostnames returned error: %v", err)
	}
	if len(ips) == 0 {
		t.Fatal("ResolveHostnames returned no IPs — glueless-referral fallback did not engage")
	}
	var found bool
	for _, ip := range ips {
		if ip == "127.240.0.99" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 127.240.0.99 in resolution; got %v", ips)
	}
}

// TestResolveOneFamily_GluelessReferralFallbackTerminates is the
// negative control: when the fallback walk genuinely cannot find
// the referral NS (no path to its zone exists anywhere in the
// stub root), resolveOneFamily must return (nil, nil) cleanly
// rather than loop or error. Bounds the budget mechanism.
func TestResolveOneFamily_GluelessReferralFallbackTerminates(t *testing.T) {
	// Fixture Setup — root delegates outside.test to an NS in a
	// zone the root knows nothing about. The fallback walk for the
	// NS hostname will hit a root that has no referral for the
	// containing zone → returns (nil, nil) recursively up the chain.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+TestPort,
			NS("outside.test", "ns1.completely-unknown.test"),
			// NO records about completely-unknown.test or its NSes.
		).
		Start(t)
	defer env.Stop()

	client := &dns.Client{Timeout: 2 * time.Second}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Run with an outer timeout so a budget-bug-induced hang fails
	// the test loudly rather than tying up the suite.
	done := make(chan struct{})
	var ips []string
	var err error
	go func() {
		ips, err = prober.ResolveHostnames(context.Background(), "target.outside.test", client, logger)
		close(done)
	}()

	select {
	case <-done:
		// Returned cleanly — good.
	case <-time.After(5 * time.Second):
		t.Fatal("ResolveHostnames hung — budget mechanism didn't terminate the fallback")
	}

	// Verification — empty result, nil error. Both families failed
	// to resolve via this path; ResolveHostnames collapses (nil, nil)
	// from both families to (empty, nil).
	if err != nil {
		t.Errorf("expected nil error for genuinely unresolvable chain, got: %v", err)
	}
	if len(ips) != 0 {
		t.Errorf("expected empty result, got: %v", ips)
	}
}
