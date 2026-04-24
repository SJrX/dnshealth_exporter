package prober

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/miekg/dns"
)

func init() {
	RegisterProber("recursion", ProbeRecursion)
}

// ProbeRecursion queries each nameserver with the RD flag set and
// checks if the RA flag is returned.
func ProbeRecursion(ctx context.Context, zone string, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error) {
	nameservers, err := DiscoverNameservers(ctx, zone, client, logger)
	if err != nil {
		return nil, fmt.Errorf("recursion: discovering nameservers: %w", err)
	}

	var results []ProbeResult
	for _, ns := range nameservers {
		result := probeRecursionForNS(ctx, zone, ns, client, logger)
		results = append(results, result)
	}
	return results, nil
}

func probeRecursionForNS(ctx context.Context, zone string, ns Nameserver, client *dns.Client, logger *slog.Logger) ProbeResult {
	result := ProbeResult{
		Zone:       zone,
		Check:      "recursion",
		Nameserver: ns.Hostname,
		IP:         ns.IP,
	}

	start := time.Now()
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeSOA)
	msg.RecursionDesired = true

	resp, _, err := ExchangeWithRetry(ctx, client, msg, ResolveAddress(ns.IP))
	result.Duration = time.Since(start)

	if err != nil {
		logger.Warn("recursion query failed", "zone", zone, "nameserver", ns.Hostname, "ip", ns.IP, "err", err)
		return result
	}

	result.Success = true
	var raValue float64
	if resp.RecursionAvailable {
		raValue = 1
	}
	result.Metrics = map[string]float64{
		"ns_recursion_available": raValue,
	}
	return result
}
