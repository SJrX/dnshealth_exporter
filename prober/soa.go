package prober

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	RegisterProber("soa", ProbeSOA)
}

// ProbeSOA queries each nameserver for the SOA record of the given zone
// and registers gauges for each SOA field.
func ProbeSOA(ctx context.Context, zone string, client *dns.Client, registry prometheus.Registerer, logger *slog.Logger) error {
	nameservers, err := discoverNameservers(ctx, zone, client, logger)
	if err != nil {
		return fmt.Errorf("soa: discovering nameservers: %w", err)
	}

	for _, ns := range nameservers {
		if err := probeSOAForNS(ctx, zone, ns, client, registry, logger); err != nil {
			logger.Warn("soa check failed for nameserver", "zone", zone, "nameserver", ns.hostname, "ip", ns.ip, "err", err)
		}
	}
	return nil
}

func probeSOAForNS(ctx context.Context, zone string, ns nameserver, client *dns.Client, registry prometheus.Registerer, logger *slog.Logger) error {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeSOA)

	resp, _, err := client.ExchangeContext(ctx, msg, ns.ip+":53")
	if err != nil {
		return fmt.Errorf("querying %s: %w", ns.ip, err)
	}

	for _, rr := range resp.Answer {
		soa, ok := rr.(*dns.SOA)
		if !ok {
			continue
		}

		labels := prometheus.Labels{
			"zone":       zone,
			"nameserver": ns.hostname,
			"ip":         ns.ip,
		}

		newGauge(registry, "dnshealth_soa_serial", "SOA serial number.", labels, float64(soa.Serial))
		newGauge(registry, "dnshealth_soa_refresh_seconds", "SOA REFRESH interval in seconds.", labels, float64(soa.Refresh))
		newGauge(registry, "dnshealth_soa_retry_seconds", "SOA RETRY interval in seconds.", labels, float64(soa.Retry))
		newGauge(registry, "dnshealth_soa_expire_seconds", "SOA EXPIRE interval in seconds.", labels, float64(soa.Expire))
		newGauge(registry, "dnshealth_soa_minimum_seconds", "SOA MINIMUM TTL (negative caching) in seconds.", labels, float64(soa.Minttl))

		return nil
	}

	return fmt.Errorf("no SOA record in response from %s", ns.ip)
}
