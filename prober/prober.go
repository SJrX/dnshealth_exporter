package prober

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
)

// RootServers is the list of root DNS server addresses to start
// delegation walking from. Override in tests to point at the test
// root fixture.
var RootServers = []string{
	"198.41.0.4:53",     // a.root-servers.net
	"170.247.170.2:53",  // b.root-servers.net
	"192.33.4.12:53",    // c.root-servers.net
	"199.7.91.13:53",    // d.root-servers.net
	"192.203.230.10:53", // e.root-servers.net
}

// ResolveAddress maps a nameserver IP to the address to query.
// In production, this appends :53. In tests or with config overrides,
// it can map to a different port.
var ResolveAddress = func(ip string) string {
	return net.JoinHostPort(ip, "53")
}

// ProbeFn is the signature for all DNS health check probers.
type ProbeFn func(ctx context.Context, zone string, client *dns.Client, registry prometheus.Registerer, logger *slog.Logger) error

// Probers maps check names to their prober functions.
var Probers = map[string]ProbeFn{}

// RegisterProber adds a prober to the global registry.
func RegisterProber(name string, fn ProbeFn) {
	Probers[name] = fn
}

// RunProber executes a named prober and records common metrics.
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

// DelegationResult holds the parent's delegation response for a zone.
type DelegationResult struct {
	// ParentServer is the parent server that provided the delegation.
	ParentServer string
	// NSRecords are the NS records from the parent's delegation.
	NSRecords []nameserver
	// Glue are the A records from the parent's additional section.
	Glue []nameserver
}

// WalkDelegation follows the DNS delegation chain from a root server
// down to the parent of the target zone. Returns the parent's
// delegation response (NS records + glue).
//
// For example, for "example.com":
//  1. Query root for "example.com" → referral to .com TLD servers
//  2. Query .com TLD for "example.com" → delegation to authoritative NSes
//  3. Return the delegation from step 2
func WalkDelegation(ctx context.Context, zone string, client *dns.Client, logger *slog.Logger) (*DelegationResult, error) {
	zone = dns.Fqdn(zone)

	// Start with a root server
	server := RootServers[0]

	// Walk referrals until we get the delegation for our zone
	for depth := 0; depth < 10; depth++ {
		msg := new(dns.Msg)
		msg.SetQuestion(zone, dns.TypeNS)
		msg.RecursionDesired = false

		logger.Debug("delegation walk", "zone", zone, "server", server, "depth", depth)

		resp, _, err := client.ExchangeContext(ctx, msg, server)
		if err != nil {
			return nil, fmt.Errorf("querying %s: %w", server, err)
		}

		// If we got an authoritative answer with NS records in
		// the Answer section, we've reached the zone's own servers.
		// The PREVIOUS hop was the parent — but we want this hop's
		// referral. If Answer has NS records, the server we queried
		// IS authoritative. For the parent view, we needed the
		// referral from the hop before. So we track the last referral.
		if resp.Authoritative && len(resp.Answer) > 0 {
			// This server is authoritative for the zone.
			// Extract NS and glue from the answer.
			return extractDelegation(server, resp.Answer, resp.Extra, zone), nil
		}

		// Check for referral in Authority section
		if len(resp.Ns) > 0 {
			// Extract NS records from the referral
			var referralNS []string
			referralGlue := make(map[string]string)

			for _, rr := range resp.Ns {
				if nsRR, ok := rr.(*dns.NS); ok {
					referralNS = append(referralNS, nsRR.Ns)
				}
			}
			for _, rr := range resp.Extra {
				if a, ok := rr.(*dns.A); ok {
					referralGlue[a.Hdr.Name] = a.A.String()
				}
			}

			if len(referralNS) == 0 {
				return nil, fmt.Errorf("empty referral from %s", server)
			}

			// Check if this referral is for our zone (the parent
			// is delegating to the zone's own servers)
			referralZone := ""
			for _, rr := range resp.Ns {
				referralZone = rr.Header().Name
				break
			}

			if strings.EqualFold(referralZone, zone) {
				// This IS the parent delegation we want
				return extractDelegation(server, resp.Ns, resp.Extra, zone), nil
			}

			// Follow the referral — pick a server we have glue for
			nextServer := ""
			for _, ns := range referralNS {
				if ip, ok := referralGlue[ns]; ok {
					nextServer = ResolveAddress(ip)
					break
				}
			}
			if nextServer == "" {
				// No glue — try resolving the first NS name
				// via the current server
				aMsg := new(dns.Msg)
				aMsg.SetQuestion(referralNS[0], dns.TypeA)
				aResp, _, err := client.ExchangeContext(ctx, aMsg, server)
				if err != nil {
					return nil, fmt.Errorf("resolving referral NS %s: %w", referralNS[0], err)
				}
				for _, rr := range aResp.Answer {
					if a, ok := rr.(*dns.A); ok {
						nextServer = ResolveAddress(a.A.String())
						break
					}
				}
			}
			if nextServer == "" {
				return nil, fmt.Errorf("could not resolve any referral NS from %s", server)
			}

			server = nextServer
			continue
		}

		return nil, fmt.Errorf("unexpected response from %s: no answer and no referral", server)
	}

	return nil, fmt.Errorf("delegation walk exceeded max depth for %s", zone)
}

func extractDelegation(parentServer string, nsSection, extraSection []dns.RR, zone string) *DelegationResult {
	result := &DelegationResult{ParentServer: parentServer}

	glueMap := make(map[string]string)
	for _, rr := range extraSection {
		if a, ok := rr.(*dns.A); ok {
			glueMap[a.Hdr.Name] = a.A.String()
		}
	}

	for _, rr := range nsSection {
		nsRR, ok := rr.(*dns.NS)
		if !ok {
			continue
		}
		ip := glueMap[nsRR.Ns]
		result.NSRecords = append(result.NSRecords, nameserver{
			hostname: nsRR.Ns,
			ip:       ip,
		})
		if ip != "" {
			result.Glue = append(result.Glue, nameserver{
				hostname: nsRR.Ns,
				ip:       ip,
			})
		}
	}

	return result
}

// discoverNameservers walks the delegation chain from root to find
// the authoritative nameservers for a zone, then resolves their IPs.
func discoverNameservers(ctx context.Context, zone string, client *dns.Client, logger *slog.Logger) ([]nameserver, error) {
	delegation, err := WalkDelegation(ctx, zone, client, logger)
	if err != nil {
		return nil, fmt.Errorf("discovering nameservers: %w", err)
	}

	var servers []nameserver
	for _, ns := range delegation.NSRecords {
		if ns.ip != "" {
			servers = append(servers, ns)
			continue
		}
		// No glue — resolve the NS hostname
		aMsg := new(dns.Msg)
		aMsg.SetQuestion(ns.hostname, dns.TypeA)
		// Query a server we already know about
		resolver := RootServers[0]
		if len(servers) > 0 {
			resolver = ResolveAddress(servers[0].ip)
		}
		aResp, _, err := client.ExchangeContext(ctx, aMsg, resolver)
		if err != nil {
			logger.Warn("could not resolve NS hostname", "ns", ns.hostname, "err", err)
			continue
		}
		for _, rr := range aResp.Answer {
			if a, ok := rr.(*dns.A); ok {
				servers = append(servers, nameserver{hostname: ns.hostname, ip: a.A.String()})
				break
			}
		}
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("no nameservers found for zone %s", zone)
	}
	return servers, nil
}

// newGauge creates a new gauge, registers it, and sets its value.
func newGauge(registry prometheus.Registerer, name, help string, labels prometheus.Labels, value float64) {
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: labels,
	})
	registry.MustRegister(g)
	g.Set(value)
}
