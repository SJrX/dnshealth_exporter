//go:build integration

package cycle_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/sjr/dnshealth_exporter/cache"
	"github.com/sjr/dnshealth_exporter/config"
	"github.com/sjr/dnshealth_exporter/cycle"
	"github.com/sjr/dnshealth_exporter/prober"
	. "github.com/sjr/dnshealth_exporter/testutil"

	"log/slog"
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
