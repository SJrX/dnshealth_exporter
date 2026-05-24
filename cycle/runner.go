package cycle

import (
	"context"
	"log/slog"
	"net"
	"strconv"
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

	// Per-zone gauge: 1 if the parent's delegation walk succeeded
	// this cycle (NS RR set is non-empty), 0 if it failed. Lives on
	// the permanent registry but Reset()s each cycle so zones removed
	// from config drop out after one cycle. Eliminates the
	// silent-absence failure mode where a broken delegation produced
	// NO series at all for the affected zone.
	ParentDelegation *prometheus.GaugeVec

	// Per-(zone, classification) gauge: count of NS hostnames in
	// each classification (parent-only, self-only, both) for this
	// zone this cycle. Reset()s each cycle so removed zones / NSes
	// drop out. The classifier prober emits per-NS info gauges; the
	// aggregation pass in Run() derives these counts from them
	// — including explicit Set(0) for classifications with no
	// entries, so PromQL can distinguish "no divergence" from "no
	// data this cycle". See spec 007 FR-005 / FR-008 / R-2.
	//
	// Note: dnshealth_ns_stealth_reachable (FR-010) is NOT a
	// runner-owned gauge — it's emitted directly via the classifier's
	// ProbeResult Metrics pipeline (which produces a per-cycle
	// registry entry). Per-cycle registry semantics already give the
	// reset-on-no-stealth behavior the contract requires.
	NSClassificationCount *prometheus.GaugeVec

	// Per-zone MX aggregation gauges (spec 008). Reset()s each cycle;
	// the aggregation pass in Run() Sets explicit values (including
	// Set(0)) for every configured zone every cycle, so PromQL can
	// distinguish "no MX records" (count=0) from "no data this cycle"
	// (series absent). Mirrors the NSClassificationCount pattern.
	//
	// Note: dnshealth_mx_info, _resolves, _is_cname, _syntax_valid
	// are emitted via the ProbeMX prober's ProbeResult pipeline (per-
	// cycle registry), NOT via runner-owned gauges. Per-cycle registry
	// semantics already give the reset-on-removal behavior the
	// contract needs. See spec 008 R-3 and the cycle.Runner notes in
	// the dnshealth_ns_stealth_reachable comment above for precedent.
	MXCount             *prometheus.GaugeVec
	MXResolvedCount     *prometheus.GaugeVec
	MXCNAMECount        *prometheus.GaugeVec
	MXSyntaxValidCount  *prometheus.GaugeVec

	// NullMX = 1 iff the canonical RFC 7505 Null MX form is met
	// (exactly one MX RR with preference 0 and target "."). Used by
	// the MX-presence row to grant PASS on intentional-no-email zones.
	NullMX *prometheus.GaugeVec

	// MXHasNullMXRR = 1 iff ANY MX RR for the zone has preference 0
	// and target ".", regardless of how many MX RRs exist total.
	// Distinguishes the canonical Null MX case (NullMX=1, count=1)
	// from the conflict case (MXHasNullMXRR=1, count>1) which row E
	// catches. Without this, NullMX alone is `NullMX==1 ⟹ count==1`
	// — making the obvious conflict predicate a tautology.
	MXHasNullMXRR *prometheus.GaugeVec

	MXIsPrimary *prometheus.GaugeVec
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
	parentDelegation := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dnshealth_parent_delegation",
		Help: "1 if the parent's NS RR set for this zone is non-empty (delegation walk succeeded), 0 otherwise. Reset each cycle.",
	}, []string{"zone"})
	nsClassificationCount := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dnshealth_ns_classification_count",
		Help: "Count of NS hostnames in each classification (parent-only / self-only / both) for this zone this cycle. Reset each cycle; explicit Set(0) per (zone, classification) when count is zero so PromQL can distinguish 'no divergence' from 'no data'.",
	}, []string{"zone", "classification"})
	mxCount := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dnshealth_mx_count",
		Help: "Total MX records for this zone this cycle (includes Null MX if present). Reset each cycle; explicit Set(0) when zone has no MX records.",
	}, []string{"zone"})
	mxResolvedCount := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dnshealth_mx_resolved_count",
		Help: "Count of MX targets that resolve (A or AAAA returned at least one record) for this zone this cycle. Reset each cycle; explicit Set(0).",
	}, []string{"zone"})
	mxCNAMECount := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dnshealth_mx_cname_count",
		Help: "Count of MX targets that are CNAMEs (RFC 2181 §10.3 violation) for this zone this cycle. Reset each cycle; explicit Set(0).",
	}, []string{"zone"})
	mxSyntaxValidCount := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dnshealth_mx_syntax_valid_count",
		Help: "Count of MX targets with LDH-valid hostnames for this zone this cycle. Compare with dnshealth_mx_count to detect any invalid target. Reset each cycle; explicit Set(0).",
	}, []string{"zone"})
	nullMX := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dnshealth_mx_null_mx",
		Help: "1 if this zone publishes exactly one MX RR with preference 0 and exchange '.' (canonical RFC 7505 Null MX form), 0 otherwise. Reset each cycle.",
	}, []string{"zone"})
	mxHasNullMXRR := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dnshealth_mx_has_null_mx_rr",
		Help: "1 if any MX RR for this zone has preference 0 and exchange '.', regardless of total MX count. Distinguishes canonical Null MX (count==1 AND has_null_mx_rr==1) from the conflict case (count>1 AND has_null_mx_rr==1) which RFC 7505 §3 forbids. Reset each cycle.",
	}, []string{"zone"})
	mxIsPrimary := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dnshealth_mx_is_primary",
		Help: "1 if this MX target's priority equals the minimum priority across all MX records for the zone; 0 if a lower-priority MX exists. Multi-MX-tied-at-minimum cases all read 1. Reset each cycle.",
	}, []string{"zone", "target"})
	reg.MustRegister(cycleDuration, zonesProbed, dnsQueries, dnsDuration, dnsTimeouts, cacheHits, cacheMisses, parentDelegation, nsClassificationCount, mxCount, mxResolvedCount, mxCNAMECount, mxSyntaxValidCount, nullMX, mxHasNullMXRR, mxIsPrimary)

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
		ParentDelegation:      parentDelegation,
		NSClassificationCount: nsClassificationCount,
		MXCount:               mxCount,
		MXResolvedCount:       mxResolvedCount,
		MXCNAMECount:          mxCNAMECount,
		MXSyntaxValidCount:    mxSyntaxValidCount,
		NullMX:                nullMX,
		MXHasNullMXRR:         mxHasNullMXRR,
		MXIsPrimary:           mxIsPrimary,
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

	// Reset per-zone operational gauges so zones removed from config
	// drop out of /metrics after one cycle (instead of carrying their
	// last-seen value forever).
	if r.ParentDelegation != nil {
		r.ParentDelegation.Reset()
	}
	if r.NSClassificationCount != nil {
		r.NSClassificationCount.Reset()
	}
	if r.MXCount != nil {
		r.MXCount.Reset()
	}
	if r.MXResolvedCount != nil {
		r.MXResolvedCount.Reset()
	}
	if r.MXCNAMECount != nil {
		r.MXCNAMECount.Reset()
	}
	if r.MXSyntaxValidCount != nil {
		r.MXSyntaxValidCount.Reset()
	}
	if r.NullMX != nil {
		r.NullMX.Reset()
	}
	if r.MXHasNullMXRR != nil {
		r.MXHasNullMXRR.Reset()
	}
	if r.MXIsPrimary != nil {
		r.MXIsPrimary.Reset()
	}

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

	// NS classification aggregation — per-zone counts derived from
	// the ns_classification prober's per-NS results. Initialize all
	// three classification values to 0 for every configured zone so
	// PromQL can distinguish "no divergence" (count=0) from "no data
	// this cycle" (series absent). Then count up from results.
	// See spec 007 FR-005, FR-008, R-2.
	if r.NSClassificationCount != nil {
		for _, zone := range cfg.Zones {
			r.NSClassificationCount.WithLabelValues(zone, "parent-only").Set(0)
			r.NSClassificationCount.WithLabelValues(zone, "self-only").Set(0)
			r.NSClassificationCount.WithLabelValues(zone, "both").Set(0)
		}
		for _, res := range allResults {
			if res.Check != "ns_classification" {
				continue
			}
			classification, ok := res.Labels["classification"]
			if !ok {
				continue
			}
			r.NSClassificationCount.WithLabelValues(res.Zone, classification).Inc()
		}
	}

	// MX aggregation — per-zone counts + Null-MX detection + per-MX
	// is-primary derivation. Initialize per-zone gauges to 0 for every
	// configured zone so PromQL can distinguish "no MX records" from
	// "no data this cycle" (spec 008 FR-007 / R-2). Then walk
	// allResults, collecting per-target metadata before deriving
	// downstream values.
	if r.MXCount != nil {
		for _, zone := range cfg.Zones {
			r.MXCount.WithLabelValues(zone).Set(0)
			r.MXResolvedCount.WithLabelValues(zone).Set(0)
			r.MXCNAMECount.WithLabelValues(zone).Set(0)
			r.MXSyntaxValidCount.WithLabelValues(zone).Set(0)
			r.NullMX.WithLabelValues(zone).Set(0)
			r.MXHasNullMXRR.WithLabelValues(zone).Set(0)
		}

		// Collect per-zone metadata in two shapes:
		//   - mxRRs: every MX RR seen (in arrival order). Drives
		//     count gauges + Null-MX detection.
		//   - perTargetMinPriority: zone → target → min(priority) seen.
		//     Drives is_primary derivation. Consolidating by target
		//     here prevents the duplicate-target last-write-wins bug
		//     that two MX RRs sharing an exchange could otherwise
		//     cause (legal config: `10 mail` + `20 mail` — same target,
		//     two different priorities).
		type mxRR struct {
			zone     string
			target   string
			priority float64
		}
		var mxRRs []mxRR
		perTargetMinPriority := make(map[string]map[string]float64)

		for _, res := range allResults {
			if res.Check != "mx" {
				continue
			}
			if _, ok := res.Metrics["mx_info"]; ok {
				r.MXCount.WithLabelValues(res.Zone).Inc()
				priorityStr, hasP := res.Labels["priority"]
				if !hasP {
					continue
				}
				p, perr := strconv.ParseFloat(priorityStr, 64)
				if perr != nil {
					continue
				}
				// Target comes from Labels["target"], not Nameserver
				// — the prober emits MX results with Nameserver=""
				// because MX is per-zone data, not per-NS fan-out
				// (the prober change for spec 008 D-7 fixed the
				// earlier mis-use of Nameserver to carry the target).
				target := res.Labels["target"]
				mxRRs = append(mxRRs, mxRR{
					zone:     res.Zone,
					target:   target,
					priority: p,
				})
				// Track min priority per (zone, target) for the
				// is_primary derivation.
				if perTargetMinPriority[res.Zone] == nil {
					perTargetMinPriority[res.Zone] = make(map[string]float64)
				}
				if existing, ok := perTargetMinPriority[res.Zone][target]; !ok || p < existing {
					perTargetMinPriority[res.Zone][target] = p
				}
			}
			if v, ok := res.Metrics["mx_resolves"]; ok && v == 1 {
				r.MXResolvedCount.WithLabelValues(res.Zone).Inc()
			}
			if v, ok := res.Metrics["mx_is_cname"]; ok && v == 1 {
				r.MXCNAMECount.WithLabelValues(res.Zone).Inc()
			}
			if v, ok := res.Metrics["mx_syntax_valid"]; ok && v == 1 {
				r.MXSyntaxValidCount.WithLabelValues(res.Zone).Inc()
			}
		}

		// Null-MX detection: two related signals.
		//   NullMX (canonical RFC 7505 §3 form): exactly one MX RR
		//     for the zone AND that RR is `0 .`. Used by the MX-
		//     presence row to grant PASS on intentional-no-email zones.
		//   MXHasNullMXRR (broader): ANY MX RR for the zone is `0 .`,
		//     regardless of total count. Catches the conflict case
		//     (Null MX RR alongside real MX RRs) that RFC 7505 §3
		//     forbids — exposed via row E.
		zoneRRCount := make(map[string]int)
		hasNullMXRR := make(map[string]bool)
		for _, rr := range mxRRs {
			zoneRRCount[rr.zone]++
			if rr.priority == 0 && rr.target == "." {
				hasNullMXRR[rr.zone] = true
			}
		}
		for zone := range hasNullMXRR {
			r.MXHasNullMXRR.WithLabelValues(zone).Set(1)
			if zoneRRCount[zone] == 1 {
				r.NullMX.WithLabelValues(zone).Set(1)
			}
		}

		// is_primary: for each (zone, target), Set 1 if that
		// target's MIN priority equals the zone's MIN across all
		// targets; 0 otherwise. Consolidating by target first means
		// duplicate-target MX RRs collapse to one is_primary signal
		// per (zone, target) — no last-write-wins ambiguity.
		if r.MXIsPrimary != nil {
			for zone, targetMinP := range perTargetMinPriority {
				if len(targetMinP) == 0 {
					continue
				}
				var zoneMin float64
				first := true
				for _, p := range targetMinP {
					if first || p < zoneMin {
						zoneMin = p
						first = false
					}
				}
				for target, p := range targetMinP {
					var v float64
					if p == zoneMin {
						v = 1
					}
					r.MXIsPrimary.WithLabelValues(zone, target).Set(v)
				}
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
			if r.ParentDelegation != nil {
				r.ParentDelegation.WithLabelValues(zone).Set(0)
			}
			return nil
		}
		r.Cache.Set(zone, delegation)
		r.Logger.Debug("delegation cache miss, stored", "zone", zone)
	}

	// Delegation acquired (fresh walk or cache hit) and is non-nil.
	if r.ParentDelegation != nil {
		r.ParentDelegation.WithLabelValues(zone).Set(1)
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

	for _, host := range hostsNeedingAugmentation(delegation.NSRecords) {
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
func hostsNeedingAugmentation(nsRecords []prober.Nameserver) []string {
	// hostname → which IP families parent glue covers. A hostname
	// with no glue at all is still entered here (with both fields
	// false) on its first-seen iteration, so it appears in the
	// keyset below and gets flagged for augmentation.
	families := make(map[string]struct{ v4, v6 bool })

	for _, ns := range nsRecords {
		f := families[ns.Hostname]
		if ns.IP != "" {
			if isIPv6(ns.IP) {
				f.v6 = true
			} else {
				f.v4 = true
			}
		}
		families[ns.Hostname] = f
	}

	out := make([]string, 0, len(families))
	for host, f := range families {
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
