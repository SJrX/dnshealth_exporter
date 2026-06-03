package prober

import (
	"context"
	"log/slog"
	"strings"

	"github.com/miekg/dns"
)

// spfResolveMaxHops bounds the iterative referral chase (root → TLD →
// zone → …). Real chains are 2–3 hops; this is a safety cap.
const spfResolveMaxHops = 16

// resolveSPFRecord is the production fetchSPFFunc (spec 010 R-4): it
// resolves the SPF (`v=spf1`) TXT record published at `name` — an
// include/redirect target — and returns it, or ok=false if `name` has no
// SPF record or could not be resolved.
//
// It resolves by an iterative walk from RootServers, following referrals
// until it reaches the authoritative server for `name` and querying TXT
// there — the same root-anchored path the delegation walk uses, so the
// demo's `.demo` targets resolve from the in-stack fake root with no
// internet. It is ctx-aware throughout (every query via ExchangeWithRetry,
// ctx checked between hops), so once the per-zone deadline expires it
// returns ok=false promptly and the overall walk cannot outlive the cycle.
func resolveSPFRecord(ctx context.Context, name string, client *dns.Client, logger *slog.Logger) (string, bool) {
	for _, txt := range iterativeResolveTXT(ctx, name, client, logger) {
		if isSPFRecord(txt) {
			return strings.TrimSpace(txt), true
		}
	}
	return "", false
}

// iterativeResolveTXT resolves the TXT records at `name` by walking from
// root and following referrals to the authoritative server. Returns the
// concatenated per-RR TXT strings (possibly empty) and ok=true once an
// authoritative answer (or NXDOMAIN) is reached; ok=false on any query
// failure, lame response, deadline, or hop-limit.
func iterativeResolveTXT(ctx context.Context, name string, client *dns.Client, logger *slog.Logger) (records []string) {
	if len(RootServers) == 0 {
		return nil
	}
	server := RootServers[0]
	qname := dns.Fqdn(name)

	for hop := 0; hop < spfResolveMaxHops; hop++ {
		if ctx.Err() != nil {
			return nil
		}
		msg := new(dns.Msg)
		msg.SetQuestion(qname, dns.TypeTXT)
		msg.RecursionDesired = false

		resp, _, err := ExchangeWithRetry(ctx, client, msg, server)
		if err != nil {
			logger.Debug("spf resolve: query failed", "name", qname, "server", server, "err", err)
			return nil
		}
		if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError {
			logger.Debug("spf resolve: non-answer rcode", "name", qname, "server", server, "rcode", dns.RcodeToString[resp.Rcode])
			return nil
		}

		// Authoritative answer (or NXDOMAIN) — collect the TXT records.
		// A referral, by contrast, is non-authoritative with NS in the
		// authority section and no answer.
		if resp.Authoritative || resp.Rcode == dns.RcodeNameError || len(resp.Ns) == 0 {
			var txts []string
			for _, rr := range resp.Answer {
				if txt, ok := rr.(*dns.TXT); ok {
					txts = append(txts, strings.Join(txt.Txt, ""))
				}
			}
			return txts
		}

		next := nextReferralServer(ctx, resp, client, logger)
		if next == "" || next == server {
			return nil
		}
		server = next
	}
	logger.Debug("spf resolve: hop limit reached", "name", qname)
	return nil
}

// nextReferralServer picks the next server to query from a referral
// response's NS + glue (mirrors WalkDelegation's referral handling).
// Falls back to resolving an NS hostname out-of-band when there is no
// glue. Returns "" if no server can be determined.
func nextReferralServer(ctx context.Context, resp *dns.Msg, client *dns.Client, logger *slog.Logger) string {
	glue := make(map[string]string)
	for _, rr := range resp.Extra {
		switch g := rr.(type) {
		case *dns.A:
			glue[g.Hdr.Name] = g.A.String()
		case *dns.AAAA:
			if _, ok := glue[g.Hdr.Name]; !ok {
				glue[g.Hdr.Name] = g.AAAA.String()
			}
		}
	}

	var nsNames []string
	for _, rr := range resp.Ns {
		if ns, ok := rr.(*dns.NS); ok {
			nsNames = append(nsNames, ns.Ns)
		}
	}
	if len(nsNames) == 0 {
		return ""
	}

	// Prefer a glued NS.
	for _, ns := range nsNames {
		if ip, ok := glue[ns]; ok {
			return ResolveAddress(ip)
		}
	}
	// No glue — resolve an NS hostname out of band.
	for _, ns := range nsNames {
		ips, err := ResolveHostnames(ctx, ns, client, logger)
		if err == nil && len(ips) > 0 {
			return ResolveAddress(ips[0])
		}
	}
	return ""
}
