package prober

import (
	"context"
	"log/slog"
	"time"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
)

// ProbeFn is the signature for all DNS health check probers.
// Each prober queries DNS for a zone and registers metrics on the
// provided registry.
type ProbeFn func(ctx context.Context, zone string, client *dns.Client, registry prometheus.Registerer, logger *slog.Logger) error

// Probers maps check names to their prober functions.
var Probers = map[string]ProbeFn{}

// RegisterProber adds a prober to the global registry.
func RegisterProber(name string, fn ProbeFn) {
	Probers[name] = fn
}

// RunProber executes a named prober and records common metrics
// (check_success, check_duration_seconds).
func RunProber(ctx context.Context, name string, zone string, client *dns.Client, registry prometheus.Registerer, logger *slog.Logger) error {
	fn, ok := Probers[name]
	if !ok {
		logger.Error("unknown prober", "name", name)
		return nil
	}

	success := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "dnshealth_check_success",
		Help:        "Whether the check succeeded (1=success, 0=failure).",
		ConstLabels: prometheus.Labels{"zone": zone, "check": name},
	})
	duration := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "dnshealth_check_duration_seconds",
		Help:        "Duration of the check in seconds.",
		ConstLabels: prometheus.Labels{"zone": zone, "check": name},
	})
	registry.MustRegister(success, duration)

	start := time.Now()
	err := fn(ctx, zone, client, registry, logger)
	duration.Set(time.Since(start).Seconds())

	if err != nil {
		success.Set(0)
		logger.Warn("check failed", "check", name, "zone", zone, "err", err)
	} else {
		success.Set(1)
	}

	return err
}
