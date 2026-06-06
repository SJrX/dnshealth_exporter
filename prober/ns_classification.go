package prober

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/miekg/dns"
)

func init() {
	RegisterProber("ns_classification", ProbeNSClassification)
}

// ProbeNSClassification compares the parent's advertised NS RR set
// against the union of NS RR sets each authoritative server reports
// for the zone, classifying every observed NS hostname as one of
// `parent-only`, `self-only`, or `both`. Emits per-NS info gauges
// (`dnshealth_ns_classification{zone, nameserver, ip, classification}`)
// that the cycle runner then aggregates into per-zone count gauges.
//
// For NSes classified `self-only` (the "stealth" case), the prober
// additionally resolves the hostname out-of-band via ResolveHostnames
// and probes for SOA authoritativeness, emitting `ns_stealth_reachable`
// = 1 if any resolved IP returned an authoritative SOA, 0 otherwise.
// This is the data backing the dashboard's hidden-master-vs-leaked-
// listing disambiguation. See spec 007 FR-010 / R-9.
//
// The prober issues its own per-NS NS-RR-set query rather than reading
// from the glue prober's output — preserves prober isolation per
// Constitution Principle VI.
func ProbeNSClassification(ctx context.Context, zone string, nameservers []Nameserver, delegation *DelegationResult, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error) {
	// parentSet: distinct NS hostnames from the parent's referral,
	// case-folded canonical FQDN per FR-006. ipByHost remembers one
	// parent-side IP per hostname so the emitted per-NS series can
	// carry an IP label when known (informational; cardinality is
	// per-hostname not per-IP).
	parentSet := make(map[string]bool)
	ipByHost := make(map[string]string)
	for _, ns := range delegation.NSRecords {
		key := canonName(ns.Hostname)
		parentSet[key] = true
		if ns.IP != "" {
			if _, seen := ipByHost[key]; !seen {
				ipByHost[key] = ns.IP
			}
		}
	}

	// selfSet: union of NS hostnames each parent-listed NS reports
	// when queried for the zone's NS RR set. Per FR-007 / R-4 we use
	// union semantics (any NS reported by at least one auth counts).
	// Dedupe queries by IP to avoid hitting the same backend twice
	// when multiple NS hostnames share an IP.
	selfSet := make(map[string]bool)
	queriedIPs := make(map[string]bool)
	for _, ns := range nameservers {
		if ns.IP == "" || queriedIPs[ns.IP] {
			continue
		}
		queriedIPs[ns.IP] = true
		hostnames := querySelfNSSet(ctx, zone, ns.IP, client, logger)
		for _, h := range hostnames {
			selfSet[canonName(h)] = true
		}
	}

	// Iterate the union deterministically (sorted) so result order
	// is stable across cycles — easier to reason about in tests
	// and in `/metrics` diffs.
	union := unionSorted(parentSet, selfSet)

	// Per-self-only-hostname reachability cache so we don't repeat
	// the out-of-band resolve+SOA-probe for hostnames that already
	// appeared earlier in the loop. (Shouldn't happen since hostnames
	// are unique in the union, but cheap insurance for future code.)
	reachabilityChecked := make(map[string]bool)

	var results []ProbeResult
	for _, host := range union {
		classification := classifyHost(host, parentSet, selfSet)

		result := ProbeResult{
			Zone:       zone,
			Check:      "ns_classification",
			Nameserver: host,
			IP:         ipByHost[host], // empty for self-only NSes
			Success:    true,
			Metrics: map[string]float64{
				"ns_classification": 1,
			},
			Labels: map[string]string{
				"classification": classification,
			},
		}

		// Active reachability probe for self-only NSes — the data
		// backing the dashboard's hidden-master-vs-leaked-listing
		// disambiguation. See spec 007 FR-010 / R-9.
		if classification == "self-only" && !reachabilityChecked[host] {
			reachabilityChecked[host] = true
			reachable := probeStealthReachable(ctx, zone, host, client, logger)
			var val float64
			if reachable {
				val = 1
				logger.Debug("stealth NS reachable (likely working hidden master)",
					"zone", zone, "stealth_ns", host)
			} else {
				logger.Warn("stealth NS not reachable (likely leaked listing)",
					"zone", zone, "stealth_ns", host)
			}
			result.Metrics["ns_stealth_reachable"] = val
		}

		results = append(results, result)
	}

	return results, nil
}

// querySelfNSSet asks one nameserver IP for the zone's NS RR set and
// returns the list of NS hostnames it reported. Returns an empty
// slice (not error) on any failure — the union semantics from R-4
// mean a single auth's failure doesn't invalidate the rest.
func querySelfNSSet(ctx context.Context, zone, ip string, client *dns.Client, logger *slog.Logger) []string {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeNS)

	resp, _, err := ExchangeWithRetry(ctx, client, msg, ResolveAddress(ip))
	if err != nil {
		logger.Warn("ns_classification self-NS query failed",
			"zone", zone, "ip", ip, "err", err)
		return nil
	}

	var hostnames []string
	for _, rr := range resp.Answer {
		if nsRR, ok := rr.(*dns.NS); ok {
			hostnames = append(hostnames, nsRR.Ns)
		}
	}
	return hostnames
}

// probeStealthReachable resolves a self-only NS hostname out-of-band
// and queries each resolved IP for the zone's SOA. Returns true iff
// at least one resolved IP returned an authoritative response with
// a SOA record — i.e., a working hidden master pattern. False on
// resolution failure, no IPs, no authoritative SOA response, etc.
// — i.e., a leaked / forgotten listing.
//
// Per R-9, only fires for self-only classifications and short-
// circuits on first authoritative success.
func probeStealthReachable(ctx context.Context, zone, hostname string, client *dns.Client, logger *slog.Logger) bool {
	ips, err := ResolveHostnames(ctx, hostname, client, logger)
	if err != nil || len(ips) == 0 {
		return false
	}

	for _, ip := range ips {
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(zone), dns.TypeSOA)
		resp, _, err := ExchangeWithRetry(ctx, client, msg, ResolveAddress(ip))
		if err != nil {
			continue
		}
		if !resp.Authoritative {
			continue
		}
		for _, rr := range resp.Answer {
			if _, ok := rr.(*dns.SOA); ok {
				return true
			}
		}
	}
	return false
}

// classifyHost decides the classification value for one hostname
// given the parent and self sets. Inputs are case-folded canonical
// FQDNs (use canonName before lookup).
func classifyHost(host string, parentSet, selfSet map[string]bool) string {
	inParent := parentSet[host]
	inSelf := selfSet[host]
	switch {
	case inParent && inSelf:
		return "both"
	case inParent:
		return "parent-only"
	default:
		return "self-only"
	}
}

// canonName returns the case-folded canonical FQDN form of a DNS
// name. Used everywhere NS hostnames are compared per FR-006.
func canonName(name string) string {
	return strings.ToLower(dns.Fqdn(name))
}

// unionSorted returns the deterministic sorted union of two sets.
// Sorting buys reproducibility — `/metrics` order is stable, tests
// don't need to handle ordering noise.
func unionSorted(a, b map[string]bool) []string {
	merged := make(map[string]bool, len(a)+len(b))
	for k := range a {
		merged[k] = true
	}
	for k := range b {
		merged[k] = true
	}
	out := make([]string, 0, len(merged))
	for k := range merged {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
