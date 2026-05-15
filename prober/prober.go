package prober

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/miekg/dns"
)

// DefaultRootServers is the canonical fallback list of public root DNS
// server addresses. RootServers is initialized from this slice and is
// restored to it on config reload when no override is configured.
var DefaultRootServers = []string{
	"198.41.0.4:53",     // a.root-servers.net
	"170.247.170.2:53",  // b.root-servers.net
	"192.33.4.12:53",    // c.root-servers.net
	"199.7.91.13:53",    // d.root-servers.net
	"192.203.230.10:53", // e.root-servers.net
}

// RootServers is the active list of root DNS server addresses used by
// delegation walking. By default it is initialized to a copy of
// DefaultRootServers (so that mutations to one slice do not affect the
// other). main.go replaces it with config.RootServers at startup and
// on reload when the operator configures an override; tests assign it
// directly to point at in-process fake roots. Callers MUST always
// reassign the whole slice — never mutate elements via index.
var RootServers = append([]string(nil), DefaultRootServers...)

// ResolveAddress maps a nameserver IP to the address to query.
// In production, this appends :53. In tests or with config overrides,
// it can map to a different port.
var ResolveAddress = func(ip string) string {
	return net.JoinHostPort(ip, "53")
}

// ProbeFn is the signature for all DNS health check probers.
// Probers receive pre-discovered nameservers and the delegation
// result so the delegation walk happens once per zone per cycle,
// not once per prober.
type ProbeFn func(ctx context.Context, zone string, nameservers []Nameserver, delegation *DelegationResult, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error)

// Probers maps check names to their prober functions.
var Probers = map[string]ProbeFn{}

// RegisterProber adds a prober to the global registry.
func RegisterProber(name string, fn ProbeFn) {
	Probers[name] = fn
}

// Nameserver represents a discovered nameserver with its hostname and IP.
type Nameserver struct {
	Hostname string
	IP       string
}

// DelegationResult holds the parent's delegation response for a zone.
type DelegationResult struct {
	ParentServer string
	NSRecords    []Nameserver
	Glue         []Nameserver
}

// WalkDelegation follows the DNS delegation chain from a root server
// down to the parent of the target zone.
func WalkDelegation(ctx context.Context, zone string, client *dns.Client, logger *slog.Logger) (*DelegationResult, error) {
	zone = dns.Fqdn(zone)
	server := RootServers[0]

	for depth := 0; depth < 10; depth++ {
		msg := new(dns.Msg)
		msg.SetQuestion(zone, dns.TypeNS)
		msg.RecursionDesired = false

		logger.Debug("delegation walk", "zone", zone, "server", server, "depth", depth)

		resp, _, err := client.ExchangeContext(ctx, msg, server)
		if err != nil {
			return nil, fmt.Errorf("querying %s: %w", server, err)
		}

		if resp.Authoritative && len(resp.Answer) > 0 {
			return extractDelegation(server, resp.Answer, resp.Extra, zone), nil
		}

		if len(resp.Ns) > 0 {
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

			referralZone := ""
			for _, rr := range resp.Ns {
				referralZone = rr.Header().Name
				break
			}

			if strings.EqualFold(referralZone, zone) {
				return extractDelegation(server, resp.Ns, resp.Extra, zone), nil
			}

			nextServer := ""
			for _, ns := range referralNS {
				if ip, ok := referralGlue[ns]; ok {
					nextServer = ResolveAddress(ip)
					break
				}
			}
			if nextServer == "" {
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
		result.NSRecords = append(result.NSRecords, Nameserver{
			Hostname: nsRR.Ns,
			IP:       ip,
		})
		if ip != "" {
			result.Glue = append(result.Glue, Nameserver{
				Hostname: nsRR.Ns,
				IP:       ip,
			})
		}
	}

	return result
}

// ResolveHostname resolves a DNS hostname to an IPv4 address by
// walking the delegation chain from root.
func ResolveHostname(ctx context.Context, hostname string, client *dns.Client, logger *slog.Logger) (string, error) {
	hostname = dns.Fqdn(hostname)
	server := RootServers[0]

	for depth := 0; depth < 10; depth++ {
		msg := new(dns.Msg)
		msg.SetQuestion(hostname, dns.TypeA)
		msg.RecursionDesired = false

		resp, _, err := client.ExchangeContext(ctx, msg, server)
		if err != nil {
			return "", fmt.Errorf("querying %s for %s: %w", server, hostname, err)
		}

		for _, rr := range resp.Answer {
			if a, ok := rr.(*dns.A); ok {
				return a.A.String(), nil
			}
		}

		if len(resp.Ns) > 0 {
			glueMap := make(map[string]string)
			for _, rr := range resp.Extra {
				if a, ok := rr.(*dns.A); ok {
					glueMap[a.Hdr.Name] = a.A.String()
				}
			}

			for _, rr := range resp.Ns {
				if nsRR, ok := rr.(*dns.NS); ok {
					if ip, ok := glueMap[nsRR.Ns]; ok {
						server = ResolveAddress(ip)
						goto nextHop
					}
				}
			}
			return "", fmt.Errorf("referral from %s has no glue for %s", server, hostname)
		}

		return "", fmt.Errorf("no answer and no referral from %s for %s", server, hostname)
	nextHop:
	}

	return "", fmt.Errorf("resolution exceeded max depth for %s", hostname)
}
