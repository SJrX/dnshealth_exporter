package prober

import (
	"github.com/prometheus/client_golang/prometheus"
)

// BuildRegistry creates a Prometheus registry from probe results.
// Each ProbeResult becomes one or more gauge metrics.
func BuildRegistry(results []ProbeResult) *prometheus.Registry {
	registry := prometheus.NewRegistry()

	for _, r := range results {
		baseLabels := prometheus.Labels{
			"zone":       r.Zone,
			"nameserver": r.Nameserver,
			"ip":         r.IP,
		}

		// Merge extra labels (e.g., source for glue checks)
		for k, v := range r.Labels {
			baseLabels[k] = v
		}

		// query_success per nameserver per check
		successLabels := prometheus.Labels{
			"zone":       r.Zone,
			"nameserver": r.Nameserver,
			"ip":         r.IP,
			"check":      r.Check,
		}
		var successVal float64
		if r.Success {
			successVal = 1
		}
		registerGauge(registry, "dnshealth_query_success",
			"Whether the query to this nameserver succeeded (1=success, 0=failure).",
			successLabels, successVal)

		// query_duration per nameserver per check
		registerGauge(registry, "dnshealth_query_duration_seconds",
			"Duration of the query to this nameserver in seconds.",
			successLabels, r.Duration.Seconds())

		// Check-specific metrics
		for name, value := range r.Metrics {
			registerGauge(registry, "dnshealth_"+name,
				"", baseLabels, value)
		}
	}

	return registry
}

func registerGauge(registry *prometheus.Registry, name, help string, labels prometheus.Labels, value float64) {
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: labels,
	})
	if err := registry.Register(g); err != nil {
		// Duplicate registration — skip silently.
		// This can happen when multiple results share the same labels.
		return
	}
	g.Set(value)
}
