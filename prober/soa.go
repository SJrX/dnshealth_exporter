package prober

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/miekg/dns"
)

func init() {
	RegisterProber("soa", ProbeSOA)
}

// ProbeSOA queries each nameserver for the SOA record of the given zone.
func ProbeSOA(ctx context.Context, zone string, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error) {
	nameservers, err := DiscoverNameservers(ctx, zone, client, logger)
	if err != nil {
		return nil, fmt.Errorf("soa: discovering nameservers: %w", err)
	}

	var results []ProbeResult
	for _, ns := range nameservers {
		result := probeSOAForNS(ctx, zone, ns, client, logger)
		results = append(results, result)
	}
	return results, nil
}

func probeSOAForNS(ctx context.Context, zone string, ns Nameserver, client *dns.Client, logger *slog.Logger) ProbeResult {
	result := ProbeResult{
		Zone:       zone,
		Check:      "soa",
		Nameserver: ns.Hostname,
		IP:         ns.IP,
	}

	start := time.Now()
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeSOA)

	resp, _, err := client.ExchangeContext(ctx, msg, ResolveAddress(ns.IP))
	result.Duration = time.Since(start)

	if err != nil {
		logger.Warn("soa query failed", "zone", zone, "nameserver", ns.Hostname, "ip", ns.IP, "err", err)
		return result
	}

	for _, rr := range resp.Answer {
		soa, ok := rr.(*dns.SOA)
		if !ok {
			continue
		}

		result.Success = true
		result.Metrics = map[string]float64{
			"soa_serial":          float64(soa.Serial),
			"soa_refresh_seconds": float64(soa.Refresh),
			"soa_retry_seconds":   float64(soa.Retry),
			"soa_expire_seconds":  float64(soa.Expire),
			"soa_minimum_seconds": float64(soa.Minttl),
		}
		return result
	}

	logger.Warn("no SOA record in response", "zone", zone, "nameserver", ns.Hostname, "ip", ns.IP)
	return result
}
