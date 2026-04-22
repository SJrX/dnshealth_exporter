package prober

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
)

// DefaultResolver is the DNS server used to discover nameservers.
// Override this in tests to point at the test root fixture.
var DefaultResolver = "127.0.0.1:53"

// ResolveAddress maps a nameserver IP to the address to query.
// In production, this appends :53. In tests or with config overrides,
// it can map to a different port.
var ResolveAddress = func(ip string) string {
	return net.JoinHostPort(ip, "53")
}

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

// nameserver represents a discovered nameserver with its hostname and IP.
type nameserver struct {
	hostname string
	ip       string
}

// discoverNameservers queries NS records for a zone via DefaultResolver
// and resolves their A records to get IPs.
func discoverNameservers(ctx context.Context, zone string, client *dns.Client, logger *slog.Logger) ([]nameserver, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeNS)

	resp, _, err := client.ExchangeContext(ctx, msg, DefaultResolver)
	if err != nil {
		return nil, fmt.Errorf("querying NS records at %s: %w", DefaultResolver, err)
	}

	var servers []nameserver
	for _, rr := range resp.Answer {
		nsRR, ok := rr.(*dns.NS)
		if !ok {
			continue
		}
		host := nsRR.Ns
		// Resolve A records via the same resolver
		aMsg := new(dns.Msg)
		aMsg.SetQuestion(host, dns.TypeA)
		aResp, _, err := client.ExchangeContext(ctx, aMsg, DefaultResolver)
		if err != nil {
			logger.Warn("could not resolve nameserver A record", "ns", host, "err", err)
			continue
		}
		for _, aRR := range aResp.Answer {
			if a, ok := aRR.(*dns.A); ok {
				servers = append(servers, nameserver{hostname: host, ip: a.A.String()})
				break
			}
		}
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("no nameservers found for zone %s", zone)
	}
	return servers, nil
}

// newGauge creates a new gauge, registers it, sets its value, and returns it.
func newGauge(registry prometheus.Registerer, name, help string, labels prometheus.Labels, value float64) {
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: labels,
	})
	registry.MustRegister(g)
	g.Set(value)
}
