package prober

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/miekg/dns"
)

func init() {
	RegisterProber("ns_hostname", ProbeNSHostname)
}

// ProbeNSHostname runs two per-hostname validity checks against each
// nameserver of the zone, emitting one ProbeResult per (host, IP)
// for label consistency with the other per-NS probers:
//
//   - dnshealth_ns_hostname_syntax_valid{zone, nameserver, ip} = 0|1
//     Pure local LDH check (RFC 952/1123). Catches the
//     "underscore-in-NS" and "trailing-hyphen" classes of typo that
//     intoDNS flags as "Bad NS name".
//
//   - dnshealth_ns_hostname_is_cname{zone, nameserver, ip} = 0|1
//     1 if the NS hostname itself is a CNAME, which violates
//     RFC 2181 §10.3 ("the data at an NS record must always be the
//     real name of the server, never an alias").
//
// The CNAME check requires a DNS walk and is cached per cycle keyed
// by hostname — multiple IPs for the same hostname only cost one
// lookup. The syntax check is local and runs unconditionally.
func ProbeNSHostname(ctx context.Context, zone string, nameservers []Nameserver, delegation *DelegationResult, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error) {
	cnameCache := make(map[string]cnameResult)

	var results []ProbeResult
	for _, ns := range nameservers {
		results = append(results, probeOneNSHostname(ctx, zone, ns, cnameCache, client, logger))
	}
	return results, nil
}

// cnameResult is the cached outcome of a per-hostname CNAME lookup.
// duration carries the actual walk time on the first encounter so all
// per-IP results for the same hostname report a consistent value.
type cnameResult struct {
	isCNAME  bool
	duration time.Duration
}

func probeOneNSHostname(ctx context.Context, zone string, ns Nameserver, cnameCache map[string]cnameResult, client *dns.Client, logger *slog.Logger) ProbeResult {
	result := ProbeResult{
		Zone:       zone,
		Check:      "ns_hostname",
		Nameserver: ns.Hostname,
		IP:         ns.IP,
		Success:    true,
	}

	var syntaxValid float64
	if isValidNSHostname(ns.Hostname) {
		syntaxValid = 1
	}

	key := strings.ToLower(dns.Fqdn(ns.Hostname))
	cached, seen := cnameCache[key]
	if !seen {
		start := time.Now()
		isCNAME, err := lookupCNAME(ctx, ns.Hostname, client, logger)
		cached = cnameResult{isCNAME: err == nil && isCNAME, duration: time.Since(start)}
		if err != nil {
			logger.Warn("CNAME lookup failed for NS hostname",
				"zone", zone, "ns", ns.Hostname, "err", err)
		}
		cnameCache[key] = cached
	}
	result.Duration = cached.duration

	var isCNAME float64
	if cached.isCNAME {
		isCNAME = 1
	}

	result.Metrics = map[string]float64{
		"ns_hostname_syntax_valid": syntaxValid,
		"ns_hostname_is_cname":     isCNAME,
	}
	return result
}

// isValidNSHostname checks that name is a syntactically valid host
// name per RFC 952 / RFC 1123 §2.1: each label is 1–63 octets of
// LDH (letters/digits/hyphen) with no leading or trailing hyphen,
// total length ≤ 253 octets after stripping the trailing dot. The
// root label "." alone is rejected.
//
// Deliberately stricter than DNS's general grammar — DNS allows
// arbitrary octets in labels, but NS records identifying actual
// hosts should be hostnames, and underscores / non-LDH characters
// in an NS target are a misconfiguration intoDNS flags as "Bad NS".
func isValidNSHostname(name string) bool {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if !isValidHostnameLabel(label) {
			return false
		}
	}
	return true
}

func isValidHostnameLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

// lookupCNAME walks the delegation chain looking for a CNAME RR
// owned by hostname. Returns true iff the authoritative answer
// contains a CNAME RR for the hostname.
//
// Mirrors resolveOneFamily's iterative walk — both functions do the
// same root-to-auth descent following NS-referral glue, only the
// terminal extraction differs. Kept as a sibling rather than
// abstracted: two parallel implementations is honest, and a generic
// "walker with extractor callback" would obscure the iteration shape
// for a single second caller. Refactor when a third qtype joins.
func lookupCNAME(ctx context.Context, hostname string, client *dns.Client, logger *slog.Logger) (bool, error) {
	hostname = dns.Fqdn(hostname)
	server := RootServers[0]

	for depth := 0; depth < 10; depth++ {
		msg := new(dns.Msg)
		msg.SetQuestion(hostname, dns.TypeCNAME)
		msg.RecursionDesired = false

		resp, _, err := client.ExchangeContext(ctx, msg, server)
		if err != nil {
			return false, fmt.Errorf("querying %s for CNAME %s: %w", server, hostname, err)
		}

		for _, rr := range resp.Answer {
			if _, ok := rr.(*dns.CNAME); ok {
				return true, nil
			}
		}

		// No CNAME RR at this auth — try to follow a referral
		// further down. Same glue logic as resolveOneFamily: A
		// preferred, AAAA accepted as fallback.
		glueMap := make(map[string]string)
		for _, rr := range resp.Extra {
			switch g := rr.(type) {
			case *dns.A:
				glueMap[g.Hdr.Name] = g.A.String()
			case *dns.AAAA:
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
			// No CNAME in answer and no further referral — the
			// authoritative answer for this name simply has no
			// CNAME at this owner, which is the healthy outcome.
			return false, nil
		}
		server = nextServer
	}

	return false, fmt.Errorf("CNAME lookup exceeded max depth for %s", hostname)
}
