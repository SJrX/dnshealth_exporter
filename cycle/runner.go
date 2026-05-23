package cycle

import (
	"context"
	"log/slog"
	"net"
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
	CycleDuration         prometheus.Gauge
	ZonesProbed           prometheus.Gauge
	DNSQueries            *prometheus.CounterVec
	DNSDuration           *prometheus.CounterVec
	DNSTimeouts           *prometheus.CounterVec
	DelegationCacheHits   prometheus.Counter
	DelegationCacheMisses prometheus.Counter
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
	dnsQueries := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dnshealth_dns_queries_total",
		Help: "Total DNS queries per server.",
	}, []string{"server"})
	dnsDuration := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dnshealth_dns_query_duration_seconds_total",
		Help: "Total DNS query time per server in seconds.",
	}, []string{"server"})
	dnsTimeouts := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dnshealth_dns_timeouts_total",
		Help: "Total DNS query timeouts per server.",
	}, []string{"server"})
	cacheHits := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dnshealth_delegation_cache_hits_total",
		Help: "Total delegation cache hits.",
	})
	cacheMisses := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dnshealth_delegation_cache_misses_total",
		Help: "Total delegation cache misses (triggered a fresh walk).",
	})
	reg.MustRegister(cycleDuration, zonesProbed, dnsQueries, dnsDuration, dnsTimeouts, cacheHits, cacheMisses)

	return &Runner{
		Cache:                 c,
		Logger:                logger,
		CycleDuration:         cycleDuration,
		ZonesProbed:           zonesProbed,
		DNSQueries:            dnsQueries,
		DNSDuration:           dnsDuration,
		DNSTimeouts:           dnsTimeouts,
		DelegationCacheHits:   cacheHits,
		DelegationCacheMisses: cacheMisses,
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
	// Per-server counters derived from results
	if r.DNSQueries != nil {
		for _, res := range allResults {
			if res.IP == "" {
				continue
			}
			r.DNSQueries.WithLabelValues(res.IP).Inc()
			r.DNSDuration.WithLabelValues(res.IP).Add(res.Duration.Seconds())
			if res.TimedOut {
				r.DNSTimeouts.WithLabelValues(res.IP).Inc()
			}
		}
	}

	return result
}

// probeZone does the delegation walk once, discovers nameservers
// once, then runs all checks with the shared results.
func (r *Runner) probeZone(ctx context.Context, zone string, cfg *config.Config) []prober.ProbeResult {
	client := &dns.Client{Timeout: cfg.QueryTimeout}

	// Delegation walk — check cache first, walk on miss
	var delegation *prober.DelegationResult
	if cached := r.Cache.Get(zone); cached != nil {
		delegation = cached.(*prober.DelegationResult)
		if r.DelegationCacheHits != nil {
			r.DelegationCacheHits.Inc()
		}
		r.Logger.Debug("delegation cache hit", "zone", zone)
	} else {
		if r.DelegationCacheMisses != nil {
			r.DelegationCacheMisses.Inc()
		}
		var err error
		delegation, err = prober.WalkDelegation(ctx, zone, client, r.Logger)
		if err != nil {
			r.Logger.Warn("delegation walk failed", "zone", zone, "err", err)
			return nil
		}
		r.Cache.Set(zone, delegation)
		r.Logger.Debug("delegation cache miss, stored", "zone", zone)
	}

	// Discover nameservers — start from the parent's glue, dedupe,
	// then augment any hostname whose parent-supplied glue is missing
	// an IP family by resolving the missing family out-of-band. This
	// covers three real-world cases:
	//   (a) NS with parent A+AAAA glue: use both, no extra lookup.
	//   (b) NS with parent A glue only (common — many TLDs don't ship
	//       AAAA glue): augment with AAAA via ResolveHostnames.
	//   (c) NS with no parent glue at all (out-of-bailiwick): resolve
	//       both families via ResolveHostnames.
	// Per spec 006 FR-002 / FR-010 / FR-011 / contracts/nameserver-fanout.md.
	nameservers, glueByHost := buildInitialNameservers(delegation.NSRecords)

	for _, host := range hostsNeedingAugmentation(delegation.NSRecords, glueByHost) {
		ips, err := prober.ResolveHostnames(ctx, host, client, r.Logger)
		if err != nil {
			r.Logger.Warn("could not resolve NS hostname",
				"zone", zone, "ns", host, "err", err)
			continue
		}
		for _, ip := range ips {
			key := host + ":" + ip
			if _, seen := glueByHost[key]; !seen {
				glueByHost[key] = struct{}{}
				nameservers = append(nameservers, prober.Nameserver{Hostname: host, IP: ip})
			}
		}
	}

	if len(nameservers) == 0 {
		r.Logger.Warn("no nameservers found", "zone", zone)
		return nil
	}

	// Run all checks with the shared nameservers and delegation
	var allResults []prober.ProbeResult
	for name, fn := range prober.Probers {
		r.Logger.Debug("running probe", "check", name, "zone", zone)

		results, err := fn(ctx, zone, nameservers, delegation, client, r.Logger)
		if err != nil {
			r.Logger.Warn("probe failed", "check", name, "zone", zone, "err", err)
			continue
		}
		allResults = append(allResults, results...)
	}
	return allResults
}

// buildInitialNameservers seeds the nameservers slice with every
// parent-supplied glue entry, deduplicated by (hostname, IP). Returns
// the slice and a seen-set the caller uses to dedupe the augmentation
// pass below.
func buildInitialNameservers(nsRecords []prober.Nameserver) ([]prober.Nameserver, map[string]struct{}) {
	seen := make(map[string]struct{})
	var nameservers []prober.Nameserver
	for _, ns := range nsRecords {
		if ns.IP == "" {
			continue
		}
		key := ns.Hostname + ":" + ns.IP
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		nameservers = append(nameservers, ns)
	}
	return nameservers, seen
}

// hostsNeedingAugmentation returns the list of NS hostnames for which
// an out-of-band ResolveHostnames call should be made. A hostname is
// included if either:
//
//   - it has no parent-supplied glue at all (the original #14 case),
//     OR
//   - its parent-supplied glue covers only one IP family (the FR-011
//     case — common when TLDs ship A glue but no AAAA glue).
//
// Hostnames whose parent glue already covers both families are
// skipped — no extra DNS queries beyond what today's code would do.
func hostsNeedingAugmentation(nsRecords []prober.Nameserver, seen map[string]struct{}) []string {
	hosts := make(map[string]bool) // hostname → need-augment
	families := make(map[string]struct{ v4, v6 bool })

	for _, ns := range nsRecords {
		f := families[ns.Hostname]
		if ns.IP == "" {
			// No glue at all → definitely needs augmentation.
			hosts[ns.Hostname] = true
			families[ns.Hostname] = f
			continue
		}
		// Categorise the IP family.
		if isIPv6(ns.IP) {
			f.v6 = true
		} else {
			f.v4 = true
		}
		families[ns.Hostname] = f
		// Mark for augmentation only if not yet definitely-covered.
		if _, already := hosts[ns.Hostname]; !already {
			hosts[ns.Hostname] = false
		}
	}

	// Hostnames missing a family need augmentation. Hostnames with
	// already-covered both families don't.
	out := make([]string, 0, len(hosts))
	for host := range hosts {
		f := families[host]
		if !f.v4 || !f.v6 {
			out = append(out, host)
		}
	}
	return out
}

// isIPv6 reports whether an IP string is in v6 form. Uses
// net.ParseIP / To4 rather than string heuristics so unusual forms
// (IPv4-mapped IPv6, etc.) are categorised correctly.
func isIPv6(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() == nil
}
