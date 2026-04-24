package cycle

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sjr/dnshealth_exporter/cache"
	"github.com/sjr/dnshealth_exporter/config"
	"github.com/sjr/dnshealth_exporter/prober"
)

// Runner executes probe cycles.
type Runner struct {
	Cache  *cache.DelegationCache
	Logger *slog.Logger

	// Operational metrics on the permanent registry.
	CycleDuration prometheus.Gauge
	ZonesProbed   prometheus.Gauge
}

// NewRunner creates a Runner and registers operational metrics on
// the given permanent registry.
func NewRunner(c *cache.DelegationCache, logger *slog.Logger, reg prometheus.Registerer) *Runner {
	cycleDuration := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dnshealth_probe_cycle_duration_seconds",
		Help: "Duration of the last probe cycle in seconds.",
	})
	zonesProbed := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dnshealth_probe_zones_total",
		Help: "Number of zones probed in the last cycle.",
	})
	reg.MustRegister(cycleDuration, zonesProbed)

	return &Runner{
		Cache:         c,
		Logger:        logger,
		CycleDuration: cycleDuration,
		ZonesProbed:   zonesProbed,
	}
}

// CycleResult holds the output of a single probe cycle.
type CycleResult struct {
	Results     []prober.ProbeResult
	Duration    time.Duration
	ZoneCount   int
	CompletedAt time.Time
}

// Run executes a single probe cycle: fans out zone probes concurrently,
// collects all results, and returns them.
func (r *Runner) Run(ctx context.Context, cfg *config.Config) *CycleResult {
	start := time.Now()

	var (
		mu         sync.Mutex
		allResults []prober.ProbeResult
		wg         sync.WaitGroup
	)

	for _, zone := range cfg.Zones {
		wg.Add(1)
		go func(zone string) {
			defer wg.Done()

			// Per-zone deadline
			zoneCtx, cancel := context.WithTimeout(ctx, cfg.ZoneDeadline)
			defer cancel()

			results := r.probeZone(zoneCtx, zone, cfg)

			mu.Lock()
			allResults = append(allResults, results...)
			mu.Unlock()
		}(zone)
	}

	wg.Wait()

	result := &CycleResult{
		Results:     allResults,
		Duration:    time.Since(start),
		ZoneCount:   len(cfg.Zones),
		CompletedAt: time.Now(),
	}

	// Update operational metrics
	if r.CycleDuration != nil {
		r.CycleDuration.Set(result.Duration.Seconds())
	}
	if r.ZonesProbed != nil {
		r.ZonesProbed.Set(float64(result.ZoneCount))
	}

	return result
}

// probeZone runs all checks for a single zone.
func (r *Runner) probeZone(ctx context.Context, zone string, cfg *config.Config) []prober.ProbeResult {
	client := &dns.Client{Timeout: cfg.QueryTimeout}

	var allResults []prober.ProbeResult
	for name, fn := range prober.Probers {
		r.Logger.Debug("running probe", "check", name, "zone", zone)

		results, err := fn(ctx, zone, client, r.Logger)
		if err != nil {
			r.Logger.Warn("probe failed", "check", name, "zone", zone, "err", err)
			continue
		}
		allResults = append(allResults, results...)
	}
	return allResults
}
