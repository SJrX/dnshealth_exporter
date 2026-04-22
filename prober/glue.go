package prober

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	RegisterProber("glue", ProbeGlue)
}

// ProbeGlue queries the parent zone for NS records and glue (A records),
// then queries each authoritative NS for its own NS and A records.
// Both views are exposed as info metrics with a "source" label so
// Grafana can detect discrepancies.
func ProbeGlue(ctx context.Context, zone string, client *dns.Client, registry prometheus.Registerer, logger *slog.Logger) error {
	// Step 1: Get NS records and glue from parent
	parentNS, parentGlue, err := queryParentForNSAndGlue(ctx, zone, client, logger)
	if err != nil {
		return fmt.Errorf("glue: querying parent: %w", err)
	}

	// Register parent's view
	for _, ns := range parentNS {
		labels := prometheus.Labels{
			"zone":       zone,
			"nameserver": ns.hostname,
			"ip":         ns.ip,
			"source":     "parent",
		}
		newGauge(registry, "dnshealth_ns_record",
			"NS record presence by source (value always 1).",
			labels, 1)
	}
	for _, g := range parentGlue {
		labels := prometheus.Labels{
			"zone":       zone,
			"nameserver": g.hostname,
			"ip":         g.ip,
			"source":     "parent",
		}
		newGauge(registry, "dnshealth_ns_glue",
			"Glue/A record presence by source (value always 1).",
			labels, 1)
	}

	// Step 2: Query each authoritative NS for its own NS + A records
	// Track registered metrics to avoid duplicate registration when
	// multiple NSes report the same records.
	registered := make(map[string]bool)

	for _, ns := range parentNS {
		selfNS, selfGlue, err := querySelfForNSAndA(ctx, zone, ns, client, logger)
		if err != nil {
			logger.Warn("glue: could not query NS for self records",
				"zone", zone, "nameserver", ns.hostname, "ip", ns.ip, "err", err)
			continue
		}

		for _, sn := range selfNS {
			key := "ns_record:" + sn.hostname + ":" + sn.ip + ":self"
			if registered[key] {
				continue
			}
			registered[key] = true
			labels := prometheus.Labels{
				"zone":       zone,
				"nameserver": sn.hostname,
				"ip":         sn.ip,
				"source":     "self",
			}
			newGauge(registry, "dnshealth_ns_record",
				"NS record presence by source (value always 1).",
				labels, 1)
		}
		for _, sg := range selfGlue {
			key := "ns_glue:" + sg.hostname + ":" + sg.ip + ":self"
			if registered[key] {
				continue
			}
			registered[key] = true
			labels := prometheus.Labels{
				"zone":       zone,
				"nameserver": sg.hostname,
				"ip":         sg.ip,
				"source":     "self",
			}
			newGauge(registry, "dnshealth_ns_glue",
				"Glue/A record presence by source (value always 1).",
				labels, 1)
		}
	}

	return nil
}

// queryParentForNSAndGlue queries for NS records of the zone.
// In a real implementation this would query the parent zone's servers;
// for now we use the system resolver to get the delegation info.
func queryParentForNSAndGlue(ctx context.Context, zone string, client *dns.Client, logger *slog.Logger) (nsRecords []nameserver, glue []nameserver, err error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeNS)

	resp, _, err := client.ExchangeContext(ctx, msg, DefaultResolver)
	if err != nil {
		return nil, nil, fmt.Errorf("querying parent at %s: %w", DefaultResolver, err)
	}

	// Extract NS records from answer
	for _, rr := range resp.Answer {
		nsRR, ok := rr.(*dns.NS)
		if !ok {
			continue
		}
		nsRecords = append(nsRecords, nameserver{hostname: nsRR.Ns, ip: ""})
	}

	// Extract glue from additional section
	glueMap := make(map[string]string) // hostname -> ip
	for _, rr := range resp.Extra {
		if a, ok := rr.(*dns.A); ok {
			glueMap[a.Hdr.Name] = a.A.String()
		}
	}

	// Fill in IPs for NS records from glue
	for i, ns := range nsRecords {
		if ip, ok := glueMap[ns.hostname]; ok {
			nsRecords[i].ip = ip
			glue = append(glue, nameserver{hostname: ns.hostname, ip: ip})
		}
	}

	if len(nsRecords) == 0 {
		return nil, nil, fmt.Errorf("no NS records found for zone %s from parent", zone)
	}

	return nsRecords, glue, nil
}

// querySelfForNSAndA queries an authoritative NS for its own NS records
// and resolves their A records.
func querySelfForNSAndA(ctx context.Context, zone string, ns nameserver, client *dns.Client, logger *slog.Logger) (nsRecords []nameserver, aRecords []nameserver, err error) {
	// Query for NS records
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeNS)

	resp, _, err := client.ExchangeContext(ctx, msg, ResolveAddress(ns.ip))
	if err != nil {
		return nil, nil, fmt.Errorf("querying NS at %s: %w", ns.ip, err)
	}

	for _, rr := range resp.Answer {
		nsRR, ok := rr.(*dns.NS)
		if !ok {
			continue
		}

		// Resolve the NS hostname via the same authoritative server
		aMsg := new(dns.Msg)
		aMsg.SetQuestion(nsRR.Ns, dns.TypeA)
		aResp, _, err := client.ExchangeContext(ctx, aMsg, ResolveAddress(ns.ip))
		if err != nil {
			logger.Debug("could not resolve NS via authoritative", "ns", nsRR.Ns, "server", ns.ip, "err", err)
			continue
		}

		for _, aRR := range aResp.Answer {
			if a, ok := aRR.(*dns.A); ok {
				nsRecords = append(nsRecords, nameserver{hostname: nsRR.Ns, ip: a.A.String()})
				aRecords = append(aRecords, nameserver{hostname: nsRR.Ns, ip: a.A.String()})
				break
			}
		}
	}

	return nsRecords, aRecords, nil
}
