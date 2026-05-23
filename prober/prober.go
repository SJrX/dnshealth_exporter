package prober

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"slices"
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
			// Accept both A and AAAA records as referral glue. Either
			// family lets the walker reach the next server (T009b /
			// H1). Prefer A over AAAA when both are present for the
			// same NS hostname — A is more universally reachable from
			// a v4-capable probe host.
			for _, rr := range resp.Extra {
				switch g := rr.(type) {
				case *dns.A:
					referralGlue[g.Hdr.Name] = g.A.String()
				case *dns.AAAA:
					if _, exists := referralGlue[g.Hdr.Name]; !exists {
						referralGlue[g.Hdr.Name] = g.AAAA.String()
					}
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
				// Fallback: query the referral NS for both A and AAAA
				// (T009b / H2). Use whichever family returns first.
				for _, qtype := range [...]uint16{dns.TypeA, dns.TypeAAAA} {
					aMsg := new(dns.Msg)
					aMsg.SetQuestion(referralNS[0], qtype)
					aResp, _, qerr := client.ExchangeContext(ctx, aMsg, server)
					if qerr != nil {
						continue
					}
					for _, rr := range aResp.Answer {
						switch a := rr.(type) {
						case *dns.A:
							if qtype == dns.TypeA {
								nextServer = ResolveAddress(a.A.String())
							}
						case *dns.AAAA:
							if qtype == dns.TypeAAAA {
								nextServer = ResolveAddress(a.AAAA.String())
							}
						}
						if nextServer != "" {
							break
						}
					}
					if nextServer != "" {
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

// extractDelegation builds a DelegationResult from a referral
// response, expanding glue across both A and AAAA records and
// fanning out one Nameserver entry per (hostname, IP) tuple. A
// hostname with both A and AAAA glue produces two NSRecords / Glue
// entries; a hostname with no glue produces one NSRecords entry
// with IP="" (the existing "needs out-of-band resolution" signal)
// and no Glue entry. Per spec 006 FR-002 / FR-010 (T009b).
func extractDelegation(parentServer string, nsSection, extraSection []dns.RR, zone string) *DelegationResult {
	result := &DelegationResult{ParentServer: parentServer}

	// hostname → list of all (A + AAAA) addresses found in Additional.
	// Deduplicated by (hostname, IP) — if a parent returns the same
	// glue record twice for one hostname (legal per RFC but unusual),
	// we still emit only one NSRecords / Glue entry. Without this,
	// downstream per-server counters (dnshealth_dns_queries_total)
	// over-report for the affected NS — see issue #26. The address
	// strings are already canonical (.A.String() / .AAAA.String()
	// produce RFC 5952 form), so plain string equality is correct.
	glueMap := make(map[string][]string)
	for _, rr := range extraSection {
		var name, ip string
		switch g := rr.(type) {
		case *dns.A:
			name, ip = g.Hdr.Name, g.A.String()
		case *dns.AAAA:
			name, ip = g.Hdr.Name, g.AAAA.String()
		default:
			continue
		}
		if slices.Contains(glueMap[name], ip) {
			continue
		}
		glueMap[name] = append(glueMap[name], ip)
	}

	for _, rr := range nsSection {
		nsRR, ok := rr.(*dns.NS)
		if !ok {
			continue
		}
		ips := glueMap[nsRR.Ns]
		if len(ips) == 0 {
			// No glue for this hostname. Emit a single sentinel
			// entry with IP="" so the caller knows to resolve it
			// out-of-band (preserves the existing #14 contract).
			result.NSRecords = append(result.NSRecords, Nameserver{
				Hostname: nsRR.Ns,
				IP:       "",
			})
			continue
		}
		for _, ip := range ips {
			entry := Nameserver{Hostname: nsRR.Ns, IP: ip}
			result.NSRecords = append(result.NSRecords, entry)
			result.Glue = append(result.Glue, entry)
		}
	}

	return result
}

// ResolveHostnames resolves a DNS hostname to all of its A and AAAA
// addresses by walking the delegation chain from root for each
// family independently.
//
// Return semantics (per spec FR-001/003/009 + research R-3):
//   - Non-empty slice + nil error: at least one address found across
//     the two families. A addresses appear first, then AAAA.
//   - Empty slice + nil error: hostname has neither A nor AAAA
//     records (NODATA on both families, or referral chains with no
//     usable glue). Legitimately unresolvable; caller drops it.
//   - Non-nil error: BOTH families failed at the protocol level
//     (timeout, SERVFAIL, network error). Caller logs and skips.
//
// Partial-success cases (A succeeds with records, AAAA fails at the
// protocol level — or vice versa) return the successful family with
// nil top-level error; the failed family's failure is logged at
// WARN. This preserves visibility on the successful family rather
// than collapsing the whole hostname.
func ResolveHostnames(ctx context.Context, hostname string, client *dns.Client, logger *slog.Logger) ([]string, error) {
	hostname = dns.Fqdn(hostname)

	ipv4s, errV4 := resolveOneFamily(ctx, hostname, dns.TypeA, client, logger)
	ipv6s, errV6 := resolveOneFamily(ctx, hostname, dns.TypeAAAA, client, logger)

	if errV4 != nil && errV6 != nil {
		return nil, fmt.Errorf("both A and AAAA resolution failed for %s: A: %v; AAAA: %v",
			hostname, errV4, errV6)
	}
	if errV4 != nil {
		logger.Warn("A resolution failed; AAAA succeeded",
			"hostname", hostname, "err", errV4)
	}
	if errV6 != nil {
		logger.Warn("AAAA resolution failed; A succeeded",
			"hostname", hostname, "err", errV6)
	}

	out := make([]string, 0, len(ipv4s)+len(ipv6s))
	out = append(out, ipv4s...)
	out = append(out, ipv6s...)
	return out, nil
}

// resolveOneFamily walks the delegation chain looking for records
// of a single type (TypeA or TypeAAAA). Symmetric per family —
// each call is independent.
//
// Returns:
//   - non-empty slice + nil: records found.
//   - empty slice + nil: legitimately no records of this type
//     (NODATA, or referral chain runs out of usable glue).
//   - nil + non-nil error: protocol failure (timeout, SERVFAIL).
//
// The walker's glue extraction accepts both A and AAAA records —
// either family is acceptable as the address to query for the next
// hop, since both families of address can reach the same server (a
// dual-stack server answers either way).
func resolveOneFamily(ctx context.Context, hostname string, qtype uint16, client *dns.Client, logger *slog.Logger) ([]string, error) {
	server := RootServers[0]

	for depth := 0; depth < 10; depth++ {
		msg := new(dns.Msg)
		msg.SetQuestion(hostname, qtype)
		msg.RecursionDesired = false

		resp, _, err := client.ExchangeContext(ctx, msg, server)
		if err != nil {
			return nil, fmt.Errorf("querying %s for %s: %w", server, hostname, err)
		}

		var found []string
		for _, rr := range resp.Answer {
			switch a := rr.(type) {
			case *dns.A:
				if qtype == dns.TypeA {
					found = append(found, a.A.String())
				}
			case *dns.AAAA:
				if qtype == dns.TypeAAAA {
					found = append(found, a.AAAA.String())
				}
			}
		}
		if len(found) > 0 {
			return found, nil
		}

		// No answer of the queried type. Try to follow a referral.
		// Glue may be A or AAAA — the walker accepts either.
		glueMap := make(map[string]string)
		for _, rr := range resp.Extra {
			switch g := rr.(type) {
			case *dns.A:
				glueMap[g.Hdr.Name] = g.A.String()
			case *dns.AAAA:
				// Prefer A if already present (more universally
				// reachable from a v4-capable probe host).
				if _, exists := glueMap[g.Hdr.Name]; !exists {
					glueMap[g.Hdr.Name] = g.AAAA.String()
				}
			}
		}

		nextServer := ""
		for _, rr := range resp.Ns {
			if nsRR, ok := rr.(*dns.NS); ok {
				if ip, ok := glueMap[nsRR.Ns]; ok {
					nextServer = ResolveAddress(ip)
					break
				}
			}
		}
		if nextServer == "" {
			// No usable referral and no answer — NODATA. Empty
			// slice + nil error is the legitimate "no records of
			// this type at this name" outcome.
			return nil, nil
		}
		server = nextServer
	}

	return nil, fmt.Errorf("resolution exceeded max depth for %s", hostname)
}
