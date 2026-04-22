package prober

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	RegisterProber("recursion", ProbeRecursion)
}

// ProbeRecursion queries each nameserver with the RD (Recursion Desired)
// flag set and checks if the RA (Recursion Available) flag is returned.
// Authoritative-only nameservers should refuse recursion (RA=0).
func ProbeRecursion(ctx context.Context, zone string, client *dns.Client, registry prometheus.Registerer, logger *slog.Logger) error {
	nameservers, err := discoverNameservers(ctx, zone, client, logger)
	if err != nil {
		return fmt.Errorf("recursion: discovering nameservers: %w", err)
	}

	for _, ns := range nameservers {
		if err := probeRecursionForNS(ctx, zone, ns, client, registry, logger); err != nil {
			logger.Warn("recursion check failed for nameserver", "zone", zone, "nameserver", ns.hostname, "ip", ns.ip, "err", err)
		}
	}
	return nil
}

func probeRecursionForNS(ctx context.Context, zone string, ns nameserver, client *dns.Client, registry prometheus.Registerer, logger *slog.Logger) error {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeSOA)
	msg.RecursionDesired = true

	resp, _, err := client.ExchangeContext(ctx, msg, ns.ip+":53")
	if err != nil {
		return fmt.Errorf("querying %s: %w", ns.ip, err)
	}

	var value float64
	if resp.RecursionAvailable {
		value = 1
	}

	labels := prometheus.Labels{
		"zone":       zone,
		"nameserver": ns.hostname,
		"ip":         ns.ip,
	}
	newGauge(registry, "dnshealth_ns_recursion_available",
		"Whether the nameserver allows recursive queries (1=allows, 0=refuses).",
		labels, value)

	return nil
}
