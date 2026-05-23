package prober

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/miekg/dns"
)

func init() {
	RegisterProber("glue", ProbeGlue)
}

// ProbeGlue walks the delegation chain to get the parent's view,
// then queries each authoritative NS for its own view. Returns
// results with source labels for parent vs self comparison.
func ProbeGlue(ctx context.Context, zone string, nameservers []Nameserver, delegation *DelegationResult, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error) {
	var results []ProbeResult

	// Parent's NS records
	for _, ns := range delegation.NSRecords {
		results = append(results, ProbeResult{
			Zone:       zone,
			Check:      "glue",
			Nameserver: ns.Hostname,
			IP:         ns.IP,
			Success:    true,
			Metrics:    map[string]float64{"ns_record": 1},
			Labels:     map[string]string{"source": "parent"},
		})
	}

	// Parent's glue
	for _, g := range delegation.Glue {
		results = append(results, ProbeResult{
			Zone:       zone,
			Check:      "glue",
			Nameserver: g.Hostname,
			IP:         g.IP,
			Success:    true,
			Metrics:    map[string]float64{"ns_glue": 1},
			Labels:     map[string]string{"source": "parent"},
		})
	}

	// Self-query each authoritative NS. Iterate `nameservers` (the
	// resolved slice the caller built) rather than delegation.NSRecords:
	// when the parent referral does not include A glue (out-of-bailiwick
	// NSs are the common case), delegation.NSRecords entries have
	// IP=="", but the caller has already resolved those hostnames via
	// ResolveHostname and added them to `nameservers`. Iterating the
	// resolved slice means the self-side check runs against every NS
	// the exporter can reach, not just the ones the parent happened to
	// hand us glue for. Fixes #14.
	registered := make(map[string]bool)

	for _, ns := range nameservers {
		selfNS, selfGlue, err := querySelfForNSAndA(ctx, zone, ns, client, logger)
		if err != nil {
			logger.Warn("glue: could not query NS for self records",
				"zone", zone, "nameserver", ns.Hostname, "ip", ns.IP, "err", err)
			results = append(results, ProbeResult{
				Zone:       zone,
				Check:      "glue",
				Nameserver: ns.Hostname,
				IP:         ns.IP,
				Success:    false,
				TimedOut:   IsTimeout(err),
			})
			continue
		}

		for _, sn := range selfNS {
			key := "ns_record:" + sn.Hostname + ":" + sn.IP + ":self"
			if registered[key] {
				continue
			}
			registered[key] = true
			results = append(results, ProbeResult{
				Zone:       zone,
				Check:      "glue",
				Nameserver: sn.Hostname,
				IP:         sn.IP,
				Success:    true,
				Metrics:    map[string]float64{"ns_record": 1},
				Labels:     map[string]string{"source": "self"},
			})
		}
		for _, sg := range selfGlue {
			key := "ns_glue:" + sg.Hostname + ":" + sg.IP + ":self"
			if registered[key] {
				continue
			}
			registered[key] = true
			results = append(results, ProbeResult{
				Zone:       zone,
				Check:      "glue",
				Nameserver: sg.Hostname,
				IP:         sg.IP,
				Success:    true,
				Metrics:    map[string]float64{"ns_glue": 1},
				Labels:     map[string]string{"source": "self"},
			})
		}
	}

	return results, nil
}

// querySelfForNSAndA queries an authoritative NS for its own NS
// records and resolves their addresses across both IPv4 and IPv6
// families.
//
// For each self-reported NS hostname, both TypeA and TypeAAAA are
// queried against the same auth server. Every address returned (any
// count, any family) produces one Nameserver entry in both
// nsRecords and aRecords. Pre-issue-#23 this function queried only
// TypeA and stopped at the first A record — silently dropping
// IPv6-only NSes and additional A records (anycast clusters).
//
// Per-family query failures are logged at DEBUG and do not abort
// the per-NS loop — a hostname for which AAAA times out can still
// surface its v4 entries from the successful A query. Matches the
// partial-success semantics of ResolveHostnames (research R-3).
func querySelfForNSAndA(ctx context.Context, zone string, ns Nameserver, client *dns.Client, logger *slog.Logger) (nsRecords []Nameserver, aRecords []Nameserver, err error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeNS)

	resp, _, err := ExchangeWithRetry(ctx, client, msg, ResolveAddress(ns.IP))
	if err != nil {
		return nil, nil, fmt.Errorf("querying NS at %s: %w", ns.IP, err)
	}

	for _, rr := range resp.Answer {
		nsRR, ok := rr.(*dns.NS)
		if !ok {
			continue
		}

		// Query both families. Each may NODATA legitimately (zone
		// has no AAAA, etc.) — that's not a failure, just an empty
		// answer.
		for _, qtype := range [...]uint16{dns.TypeA, dns.TypeAAAA} {
			aMsg := new(dns.Msg)
			aMsg.SetQuestion(nsRR.Ns, qtype)
			aResp, _, err := ExchangeWithRetry(ctx, client, aMsg, ResolveAddress(ns.IP))
			if err != nil {
				logger.Debug("could not resolve NS via authoritative",
					"ns", nsRR.Ns, "server", ns.IP, "qtype", dns.TypeToString[qtype], "err", err)
				continue
			}

			for _, aRR := range aResp.Answer {
				switch a := aRR.(type) {
				case *dns.A:
					if qtype == dns.TypeA {
						nsRecords = append(nsRecords, Nameserver{Hostname: nsRR.Ns, IP: a.A.String()})
						aRecords = append(aRecords, Nameserver{Hostname: nsRR.Ns, IP: a.A.String()})
					}
				case *dns.AAAA:
					if qtype == dns.TypeAAAA {
						nsRecords = append(nsRecords, Nameserver{Hostname: nsRR.Ns, IP: a.AAAA.String()})
						aRecords = append(aRecords, Nameserver{Hostname: nsRR.Ns, IP: a.AAAA.String()})
					}
				}
			}
		}
	}

	return nsRecords, aRecords, nil
}
