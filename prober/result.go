package prober

import "time"

// ProbeResult represents the output of a single DNS query within
// a probe. It carries data only — no Prometheus types. Metrics
// are built from results centrally at the end of each probe cycle.
type ProbeResult struct {
	// Zone that was probed.
	Zone string
	// Check type (e.g., "soa", "recursion", "glue").
	Check string
	// Nameserver hostname (e.g., "ns1.example.com.").
	Nameserver string
	// IP address of the nameserver queried.
	IP string
	// Success indicates whether the query succeeded.
	Success bool
	// TimedOut indicates the query failed due to a timeout.
	TimedOut bool
	// Duration of the DNS query.
	Duration time.Duration
	// Metrics holds check-specific metric values.
	// Keys are metric name suffixes (e.g., "soa_serial", "soa_refresh_seconds").
	Metrics map[string]float64
	// Labels holds extra labels beyond zone/nameserver/ip
	// (e.g., {"source": "parent"} for glue checks).
	Labels map[string]string
}
