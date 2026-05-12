//go:build integration

package cycle_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sjr/dnshealth_exporter/cache"
	"github.com/sjr/dnshealth_exporter/config"
	"github.com/sjr/dnshealth_exporter/cycle"
	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"
)

const CycleTestPort = "10054"

func TestMain(m *testing.M) {
	prober.RootServers = []string{"127.240.0.1:" + CycleTestPort}
	prober.ResolveAddress = func(ip string) string {
		return net.JoinHostPort(ip, CycleTestPort)
	}
	os.Exit(m.Run())
}

func TestCycleRunner_ProducesResults(t *testing.T) {
	// Fixture Setup — standard zone with two nameservers
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+CycleTestPort,
			SOA("example.test", Serial(42)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+CycleTestPort,
			SOA("example.test", Serial(42)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	runner := &cycle.Runner{
		Cache:  cache.NewDelegationCache(30 * time.Minute),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	cfg := &config.Config{
		Zones:        []string{"example.test"},
		QueryTimeout: 10 * time.Second,
		ZoneDeadline: 60 * time.Second,
	}

	// Exercise SUT
	result := runner.Run(context.Background(), cfg)

	// Verification
	if result.ZoneCount != 1 {
		t.Errorf("zone count: got %d, want 1", result.ZoneCount)
	}
	if len(result.Results) == 0 {
		t.Fatal("expected probe results, got none")
	}

	registry := prober.BuildRegistry(result.Results)
	AssertGaugeExists(t, registry, "dnshealth_soa_serial",
		WithLabels("zone", "example.test"))
}

func TestCycleRunner_MetricsRefreshOnSubsequentCycles(t *testing.T) {
	// Fixture Setup — ns1 starts with serial 100
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+CycleTestPort,
			SOA("example.test", Serial(100)),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	runner := &cycle.Runner{
		Cache:  cache.NewDelegationCache(30 * time.Minute),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	cfg := &config.Config{
		Zones:        []string{"example.test"},
		QueryTimeout: 5 * time.Second,
		ZoneDeadline: 30 * time.Second,
	}

	// Exercise SUT — first cycle
	result1 := runner.Run(context.Background(), cfg)
	reg1 := prober.BuildRegistry(result1.Results)

	// Verification — serial 100
	AssertGauge(t, reg1, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(100))

	// Stop old fixture, start new one with serial 200
	env.Stop()
	env2 := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+CycleTestPort,
			SOA("example.test", Serial(200)),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env2.Stop()

	// Invalidate delegation cache so we re-walk
	runner.Cache.Invalidate()

	// Exercise SUT — second cycle
	result2 := runner.Run(context.Background(), cfg)
	reg2 := prober.BuildRegistry(result2.Results)

	// Verification — serial updated to 200
	AssertGauge(t, reg2, "dnshealth_soa_serial",
		WithLabels("zone", "example.test", "ip", "127.240.0.2"),
		WithValue(200))
}

func TestCycleRunner_StaleNSRemovedFromMetrics(t *testing.T) {
	// Fixture Setup — cycle 1: ns1 + ns2
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+CycleTestPort,
			SOA("example.test", Serial(1)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Server("127.240.0.3:"+CycleTestPort,
			SOA("example.test", Serial(1)),
			NS("example.test", "ns1.example.test"),
			NS("example.test", "ns2.example.test"),
			A("ns1.example.test", "127.240.0.2"),
			A("ns2.example.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	runner := &cycle.Runner{
		Cache:  cache.NewDelegationCache(30 * time.Minute),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	cfg := &config.Config{
		Zones:        []string{"example.test"},
		QueryTimeout: 5 * time.Second,
		ZoneDeadline: 30 * time.Second,
	}

	// Cycle 1: both nameservers present
	result1 := runner.Run(context.Background(), cfg)
	reg1 := prober.BuildRegistry(result1.Results)
	AssertGaugeExists(t, reg1, "dnshealth_soa_serial",
		WithLabels("ip", "127.240.0.2"))
	AssertGaugeExists(t, reg1, "dnshealth_soa_serial",
		WithLabels("ip", "127.240.0.3"))

	// Stop, restart with only ns1 (ns2 removed from delegation)
	env.Stop()
	runner.Cache.Invalidate()
	env2 := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+CycleTestPort,
			SOA("example.test", Serial(2)),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env2.Stop()

	// Cycle 2: ns2 should be absent
	result2 := runner.Run(context.Background(), cfg)
	reg2 := prober.BuildRegistry(result2.Results)

	// Verification — ns1 present, ns2 absent (scatter-gather naturally)
	AssertGaugeExists(t, reg2, "dnshealth_soa_serial",
		WithLabels("ip", "127.240.0.2"))
	AssertGaugeMissing(t, reg2, "dnshealth_soa_serial",
		WithLabels("ip", "127.240.0.3"))
}

func TestCycleRunner_MultipleZones(t *testing.T) {
	// Fixture Setup — two independent zones
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("alpha.test"),
			NS("alpha.test", "ns1.alpha.test"),
			A("ns1.alpha.test", "127.240.0.2"),
			SOA("beta.test"),
			NS("beta.test", "ns1.beta.test"),
			A("ns1.beta.test", "127.240.0.3"),
		).
		Server("127.240.0.2:"+CycleTestPort,
			SOA("alpha.test", Serial(111)),
			NS("alpha.test", "ns1.alpha.test"),
			A("ns1.alpha.test", "127.240.0.2"),
		).
		Server("127.240.0.3:"+CycleTestPort,
			SOA("beta.test", Serial(222)),
			NS("beta.test", "ns1.beta.test"),
			A("ns1.beta.test", "127.240.0.3"),
		).
		Start(t)
	defer env.Stop()

	runner := &cycle.Runner{
		Cache:  cache.NewDelegationCache(30 * time.Minute),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	cfg := &config.Config{
		Zones:        []string{"alpha.test", "beta.test"},
		QueryTimeout: 5 * time.Second,
		ZoneDeadline: 30 * time.Second,
	}

	// Exercise SUT
	result := runner.Run(context.Background(), cfg)
	registry := prober.BuildRegistry(result.Results)

	// Verification — both zones probed
	if result.ZoneCount != 2 {
		t.Errorf("zone count: got %d, want 2", result.ZoneCount)
	}
	AssertGauge(t, registry, "dnshealth_soa_serial",
		WithLabels("zone", "alpha.test"), WithValue(111))
	AssertGauge(t, registry, "dnshealth_soa_serial",
		WithLabels("zone", "beta.test"), WithValue(222))
}

func TestCycleRunner_SlowZoneDoesNotBlockOthers(t *testing.T) {
	// Fixture Setup — alpha.test is normal, beta.test drops all queries
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("alpha.test"),
			NS("alpha.test", "ns1.alpha.test"),
			A("ns1.alpha.test", "127.240.0.2"),
			SOA("beta.test"),
			NS("beta.test", "ns1.beta.test"),
			A("ns1.beta.test", "127.240.0.4"),
		).
		Server("127.240.0.2:"+CycleTestPort,
			SOA("alpha.test", Serial(999)),
			NS("alpha.test", "ns1.alpha.test"),
			A("ns1.alpha.test", "127.240.0.2"),
		).
		ServerWithOptions("127.240.0.4:"+CycleTestPort, ServerOptions{Drop: true}).
		Start(t)
	defer env.Stop()

	runner := &cycle.Runner{
		Cache:  cache.NewDelegationCache(30 * time.Minute),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	cfg := &config.Config{
		Zones:        []string{"alpha.test", "beta.test"},
		QueryTimeout: 1 * time.Second,
		ZoneDeadline: 2 * time.Second,
	}

	// Exercise SUT
	start := time.Now()
	result := runner.Run(context.Background(), cfg)
	elapsed := time.Since(start)
	registry := prober.BuildRegistry(result.Results)

	// Verification — alpha.test completed despite beta.test timing out.
	// Total time should be ~2s (zone deadline), not 2x (sequential).
	AssertGauge(t, registry, "dnshealth_soa_serial",
		WithLabels("zone", "alpha.test"), WithValue(999))
	if elapsed > 5*time.Second {
		t.Errorf("cycle took %v — slow zone blocked others", elapsed)
	}
}

func TestCycleRunner_RetrySucceedsOnSecondAttempt(t *testing.T) {
	// Fixture Setup — ns1 drops the first query but responds to retry
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		ServerWithOptions("127.240.0.2:"+CycleTestPort, ServerOptions{DropFirstN: 1},
			SOA("example.test", Serial(777)),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	runner := &cycle.Runner{
		Cache:  cache.NewDelegationCache(30 * time.Minute),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	cfg := &config.Config{
		Zones:        []string{"example.test"},
		QueryTimeout: 1 * time.Second,
		ZoneDeadline: 10 * time.Second,
	}

	// Exercise SUT
	result := runner.Run(context.Background(), cfg)
	registry := prober.BuildRegistry(result.Results)

	// Verification — retry succeeded, SOA metric present
	AssertGaugeExists(t, registry, "dnshealth_soa_serial",
		WithLabels("zone", "example.test"))

	// The server received at least 2 queries (original + retry)
	if count := env.QueryCount("127.240.0.2:" + CycleTestPort); count < 2 {
		t.Errorf("expected at least 2 queries to ns1, got %d", count)
	}
}

func TestCycleRunner_RetryOnSERVFAIL(t *testing.T) {
	// Fixture Setup — ns1 returns SERVFAIL (rcode 2)
	// ExchangeWithRetry only retries on transport errors, not rcodes.
	// SERVFAIL is a valid DNS response, so no retry — but the prober
	// should still report Success=false.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		ServerWithOptions("127.240.0.2:"+CycleTestPort, ServerOptions{Rcode: 2}, // SERVFAIL
			SOA("example.test"),
		).
		Start(t)
	defer env.Stop()

	runner := &cycle.Runner{
		Cache:  cache.NewDelegationCache(30 * time.Minute),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	cfg := &config.Config{
		Zones:        []string{"example.test"},
		QueryTimeout: 2 * time.Second,
		ZoneDeadline: 10 * time.Second,
	}

	// Exercise SUT
	result := runner.Run(context.Background(), cfg)
	registry := prober.BuildRegistry(result.Results)

	// Verification — query_success=0 for this NS (SERVFAIL means no usable data)
	AssertGauge(t, registry, "dnshealth_query_success",
		WithLabels("zone", "example.test", "ip", "127.240.0.2", "check", "soa"),
		WithValue(0))
}

func TestCycleRunner_OperationalCountersIncrement(t *testing.T) {
	// Fixture Setup
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+CycleTestPort,
			SOA("example.test", Serial(1)),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	permRegistry := prometheus.NewRegistry()
	runner := cycle.NewRunner(
		cache.NewDelegationCache(30*time.Minute),
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		permRegistry,
	)
	cfg := &config.Config{
		Zones:        []string{"example.test"},
		QueryTimeout: 5 * time.Second,
		ZoneDeadline: 30 * time.Second,
	}

	// Exercise SUT
	runner.Run(context.Background(), cfg)

	// Verification — operational metrics on permanent registry
	AssertGaugeExists(t, permRegistry, "dnshealth_probe_cycle_duration_seconds")
	AssertGauge(t, permRegistry, "dnshealth_probe_zones_total", WithValue(1))

	// Per-server counters should have been incremented
	// (counters are gathered as gauges by the test helper)
	families, _ := permRegistry.Gather()
	foundQueries := false
	for _, f := range families {
		if f.GetName() == "dnshealth_dns_queries_total" {
			foundQueries = true
			if len(f.GetMetric()) == 0 {
				t.Error("dnshealth_dns_queries_total has no metrics")
			}
			for _, m := range f.GetMetric() {
				if m.GetCounter().GetValue() < 1 {
					t.Errorf("expected at least 1 query, got %v", m.GetCounter().GetValue())
				}
			}
		}
	}
	if !foundQueries {
		t.Error("dnshealth_dns_queries_total not found on permanent registry")
	}
}

// counterValue returns the value of the dnshealth_dns_timeouts_total
// counter for the given server label, or 0 if no series exists.
func counterValue(t *testing.T, reg *prometheus.Registry, metric, label, value string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	for _, f := range families {
		if f.GetName() != metric {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == label && lp.GetValue() == value {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func TestCycleRunner_TimeoutCounterIncrementsOnTimeout(t *testing.T) {
	// Fixture Setup — referral works, target NS drops all queries
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		ServerWithOptions("127.240.0.2:"+CycleTestPort, ServerOptions{Drop: true}).
		Start(t)
	defer env.Stop()

	permRegistry := prometheus.NewRegistry()
	runner := cycle.NewRunner(
		cache.NewDelegationCache(30*time.Minute),
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		permRegistry,
	)
	cfg := &config.Config{
		Zones:        []string{"example.test"},
		QueryTimeout: 500 * time.Millisecond,
		ZoneDeadline: 5 * time.Second,
	}

	// Exercise SUT
	runner.Run(context.Background(), cfg)

	// Verification — timeouts counter incremented for the dropped server.
	// soa, recursion, and glue self-query all time out, so >= 1.
	got := counterValue(t, permRegistry, "dnshealth_dns_timeouts_total", "server", "127.240.0.2")
	if got < 1 {
		t.Errorf("dnshealth_dns_timeouts_total{server=127.240.0.2}: got %v, want >= 1", got)
	}
}

func TestCycleRunner_TimeoutCounterDoesNotIncrementOnSERVFAIL(t *testing.T) {
	// Regression test for the original bug — SERVFAIL is a probe
	// failure but not a timeout. Pre-fix, the timeout counter
	// incremented on any !Success; post-fix it only increments on
	// actual network timeouts.
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		ServerWithOptions("127.240.0.2:"+CycleTestPort, ServerOptions{Rcode: 2},
			SOA("example.test"),
		).
		Start(t)
	defer env.Stop()

	permRegistry := prometheus.NewRegistry()
	runner := cycle.NewRunner(
		cache.NewDelegationCache(30*time.Minute),
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		permRegistry,
	)
	cfg := &config.Config{
		Zones:        []string{"example.test"},
		QueryTimeout: 2 * time.Second,
		ZoneDeadline: 10 * time.Second,
	}

	// Exercise SUT
	runner.Run(context.Background(), cfg)

	// Verification — SERVFAIL is a probe failure but not a timeout.
	// The timeout counter must NOT have incremented for this server.
	got := counterValue(t, permRegistry, "dnshealth_dns_timeouts_total", "server", "127.240.0.2")
	if got != 0 {
		t.Errorf("dnshealth_dns_timeouts_total{server=127.240.0.2}: got %v on SERVFAIL, want 0", got)
	}

	// Sanity check — queries DID happen, so we know the counter would
	// have been visible if it had incremented.
	queries := counterValue(t, permRegistry, "dnshealth_dns_queries_total", "server", "127.240.0.2")
	if queries < 1 {
		t.Errorf("dnshealth_dns_queries_total{server=127.240.0.2}: got %v, want >= 1 (sanity)", queries)
	}
}

// unlabeledCounterValue returns the value of an unlabeled counter,
// or 0 if no series exists.
func unlabeledCounterValue(t *testing.T, reg *prometheus.Registry, metric string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	for _, f := range families {
		if f.GetName() != metric {
			continue
		}
		for _, m := range f.GetMetric() {
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

func TestCycleRunner_DelegationCacheCountersTrackHitsAndMisses(t *testing.T) {
	// Fixture Setup — standard zone, no fancy options
	env := NewDNSFixture(t).
		ReferralServer("127.240.0.1:"+CycleTestPort,
			SOA("example.test"),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Server("127.240.0.2:"+CycleTestPort,
			SOA("example.test", Serial(1)),
			NS("example.test", "ns1.example.test"),
			A("ns1.example.test", "127.240.0.2"),
		).
		Start(t)
	defer env.Stop()

	permRegistry := prometheus.NewRegistry()
	runner := cycle.NewRunner(
		cache.NewDelegationCache(30*time.Minute),
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		permRegistry,
	)
	cfg := &config.Config{
		Zones:        []string{"example.test"},
		QueryTimeout: 5 * time.Second,
		ZoneDeadline: 30 * time.Second,
	}

	// Exercise SUT — first cycle: cache empty → miss
	runner.Run(context.Background(), cfg)

	misses1 := unlabeledCounterValue(t, permRegistry, "dnshealth_delegation_cache_misses_total")
	hits1 := unlabeledCounterValue(t, permRegistry, "dnshealth_delegation_cache_hits_total")
	if misses1 != 1 {
		t.Errorf("after first cycle: misses=%v, want 1", misses1)
	}
	if hits1 != 0 {
		t.Errorf("after first cycle: hits=%v, want 0", hits1)
	}

	// Exercise SUT — second cycle: same zone, within TTL → hit
	runner.Run(context.Background(), cfg)

	misses2 := unlabeledCounterValue(t, permRegistry, "dnshealth_delegation_cache_misses_total")
	hits2 := unlabeledCounterValue(t, permRegistry, "dnshealth_delegation_cache_hits_total")
	if misses2 != 1 {
		t.Errorf("after second cycle: misses=%v, want 1 (no new miss)", misses2)
	}
	if hits2 != 1 {
		t.Errorf("after second cycle: hits=%v, want 1", hits2)
	}

	// Exercise SUT — invalidate cache, run again → miss again.
	// This is the reload-style scenario.
	runner.Cache.Invalidate()
	runner.Run(context.Background(), cfg)

	misses3 := unlabeledCounterValue(t, permRegistry, "dnshealth_delegation_cache_misses_total")
	hits3 := unlabeledCounterValue(t, permRegistry, "dnshealth_delegation_cache_hits_total")
	if misses3 != 2 {
		t.Errorf("after invalidate + third cycle: misses=%v, want 2", misses3)
	}
	if hits3 != 1 {
		t.Errorf("after invalidate + third cycle: hits=%v, want 1 (no new hit)", hits3)
	}
}
