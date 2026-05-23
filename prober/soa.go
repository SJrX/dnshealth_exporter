package prober

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/miekg/dns"
)

func init() {
	RegisterProber("soa", ProbeSOA)
}

// ProbeSOA queries each nameserver for the SOA record of the given
// zone. Beyond the raw SOA fields (serial / refresh / retry / expire
// / minimum) it surfaces MNAME validity:
//
//   - dnshealth_soa_mname{...,mname="..."} — info gauge (=1) carrying
//     the MNAME as a label so disagreement between NSs is visible.
//   - dnshealth_soa_mname_in_ns_set{...} — 1 if the MNAME hostname is
//     itself a member of the zone's NS RR set.
//   - dnshealth_soa_mname_resolves{...} — 1 if the MNAME hostname
//     resolves to at least one A or AAAA record.
func ProbeSOA(ctx context.Context, zone string, nameservers []Nameserver, delegation *DelegationResult, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error) {
	// Canonical NS hostname set for in-NS-set comparison. FQDN-normalised
	// and lowercased — DNS comparison is case-insensitive per RFC 4343.
	nsSet := make(map[string]bool, len(delegation.NSRecords))
	for _, ns := range delegation.NSRecords {
		nsSet[strings.ToLower(dns.Fqdn(ns.Hostname))] = true
	}

	// Resolution cache: a healthy zone usually has every NS reporting
	// the same MNAME, so resolve each distinct MNAME hostname at most
	// once per cycle. Key is the lowercased FQDN.
	mnameResolves := make(map[string]bool)

	var results []ProbeResult
	for _, ns := range nameservers {
		result := probeSOAForNS(ctx, zone, ns, nsSet, mnameResolves, client, logger)
		results = append(results, result)
	}
	return results, nil
}

func probeSOAForNS(ctx context.Context, zone string, ns Nameserver, nsSet map[string]bool, mnameResolves map[string]bool, client *dns.Client, logger *slog.Logger) ProbeResult {
	result := ProbeResult{
		Zone:       zone,
		Check:      "soa",
		Nameserver: ns.Hostname,
		IP:         ns.IP,
	}

	start := time.Now()
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeSOA)

	resp, _, err := ExchangeWithRetry(ctx, client, msg, ResolveAddress(ns.IP))
	result.Duration = time.Since(start)

	if err != nil {
		result.TimedOut = IsTimeout(err)
		logger.Warn("soa query failed", "zone", zone, "nameserver", ns.Hostname, "ip", ns.IP, "err", err)
		return result
	}

	for _, rr := range resp.Answer {
		soa, ok := rr.(*dns.SOA)
		if !ok {
			continue
		}

		// soa.Ns is the primary master per RFC 1035 (miekg/dns names
		// the MNAME field `Ns`, which is easy to misread).
		mname := dns.Fqdn(soa.Ns)
		mnameKey := strings.ToLower(mname)

		var inNSSet float64
		if nsSet[mnameKey] {
			inNSSet = 1
		}

		var resolves float64
		if cached, seen := mnameResolves[mnameKey]; seen {
			if cached {
				resolves = 1
			}
		} else {
			ips, rerr := ResolveHostnames(ctx, mname, client, logger)
			ok := rerr == nil && len(ips) > 0
			mnameResolves[mnameKey] = ok
			if ok {
				resolves = 1
			}
		}

		result.Success = true
		result.Labels = map[string]string{
			"mname": mname,
		}
		result.Metrics = map[string]float64{
			"soa_serial":          float64(soa.Serial),
			"soa_refresh_seconds": float64(soa.Refresh),
			"soa_retry_seconds":   float64(soa.Retry),
			"soa_expire_seconds":  float64(soa.Expire),
			"soa_minimum_seconds": float64(soa.Minttl),
			"soa_mname":           1, // info gauge — MNAME carried in the label
			"soa_mname_in_ns_set": inNSSet,
			"soa_mname_resolves":  resolves,
		}
		return result
	}

	logger.Warn("no SOA record in response", "zone", zone, "nameserver", ns.Hostname, "ip", ns.IP)
	return result
}
