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
func ProbeGlue(ctx context.Context, zone string, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error) {
	delegation, err := WalkDelegation(ctx, zone, client, logger)
	if err != nil {
		return nil, fmt.Errorf("glue: %w", err)
	}

	// Resolve missing IPs for nameservers without glue
	for i, ns := range delegation.NSRecords {
		if ns.IP != "" {
			continue
		}
		ip, err := ResolveHostname(ctx, ns.Hostname, client, logger)
		if err != nil {
			logger.Warn("glue: could not resolve NS without glue",
				"zone", zone, "nameserver", ns.Hostname, "err", err)
			continue
		}
		delegation.NSRecords[i].IP = ip
	}

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

	// Self-query each authoritative NS
	registered := make(map[string]bool)

	for _, ns := range delegation.NSRecords {
		if ns.IP == "" {
			continue
		}

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

// querySelfForNSAndA queries an authoritative NS for its own NS records
// and resolves their A records.
func querySelfForNSAndA(ctx context.Context, zone string, ns Nameserver, client *dns.Client, logger *slog.Logger) (nsRecords []Nameserver, aRecords []Nameserver, err error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeNS)

	resp, _, err := client.ExchangeContext(ctx, msg, ResolveAddress(ns.IP))
	if err != nil {
		return nil, nil, fmt.Errorf("querying NS at %s: %w", ns.IP, err)
	}

	for _, rr := range resp.Answer {
		nsRR, ok := rr.(*dns.NS)
		if !ok {
			continue
		}

		aMsg := new(dns.Msg)
		aMsg.SetQuestion(nsRR.Ns, dns.TypeA)
		aResp, _, err := client.ExchangeContext(ctx, aMsg, ResolveAddress(ns.IP))
		if err != nil {
			logger.Debug("could not resolve NS via authoritative", "ns", nsRR.Ns, "server", ns.IP, "err", err)
			continue
		}

		for _, aRR := range aResp.Answer {
			if a, ok := aRR.(*dns.A); ok {
				nsRecords = append(nsRecords, Nameserver{Hostname: nsRR.Ns, IP: a.A.String()})
				aRecords = append(aRecords, Nameserver{Hostname: nsRR.Ns, IP: a.A.String()})
				break
			}
		}
	}

	return nsRecords, aRecords, nil
}
