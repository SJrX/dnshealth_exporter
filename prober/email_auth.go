package prober

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/miekg/dns"
)

func init() {
	RegisterProber("email_auth", ProbeEmailAuth)
}

// ProbeEmailAuth surfaces a zone's Tier-1 email-authentication records
// (spec 009): SPF at the apex and DMARC at `_dmarc.<zone>`. It issues
// exactly two TypeTXT queries per zone (no recursion — the SPF lookup-
// budget check is deferred to #58), parses the records with the pure
// spf.go / dmarc.go parsers, and emits per-zone gauges.
//
// Email-auth is per-zone data, not per-nameserver fan-out, so results
// carry Nameserver="" (like ProbeMX). The dashboard aggregates these
// `by (zone)` exactly as the SOA rows do. Boolean gauges are emitted for
// every reachable zone every cycle (value 0 when the record is absent),
// so the per-cycle registry gives the zero-emission the dashboard
// predicates rely on; the info gauges (terminal_all / policy) are emitted
// only for the state that applies.
func ProbeEmailAuth(ctx context.Context, zone string, nameservers []Nameserver, delegation *DelegationResult, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error) {
	var results []ProbeResult

	// --- SPF: TXT at the zone apex ---
	spfTxts, spfOK := fetchTXTRecords(ctx, zone, nameservers, client, logger)
	if spfOK {
		spf := analyzeSPF(spfTxts)
		results = append(results, ProbeResult{
			Zone:    zone,
			Check:   "email_auth",
			Success: true,
			Metrics: map[string]float64{
				"spf_present":      boolToFloat(spf.present),
				"spf_record_count": float64(spf.recordCount),
				"spf_valid":        boolToFloat(spf.valid),
			},
		})
		// Terminal-qualifier info gauge only when exactly one SPF record
		// exists — with zero or multiple records the qualifier is
		// undefined and the dashboard row reads N/A via absent().
		if spf.present && spf.recordCount == 1 {
			results = append(results, ProbeResult{
				Zone:    zone,
				Check:   "email_auth",
				Success: true,
				Metrics: map[string]float64{"spf_terminal_all": 1},
				Labels:  map[string]string{"qualifier": spf.qualifier},
			})
		}
		// SPF DNS-lookup budget (RFC 7208 §4.6.4, spec 010) — only for a
		// single valid record. The fetch closure carries ctx, so the
		// recursive walk respects the per-zone deadline; a/mx/ptr/exists
		// are counted syntactically, only include/redirect are resolved.
		if spf.present && spf.recordCount == 1 && spf.valid {
			fetch := func(target string) (string, bool) {
				return resolveSPFRecord(ctx, target, client, logger)
			}
			count, complete := countSPFLookups(spf.raw, fetch)
			exceeded := 0.0
			if count > spfMaxLookups {
				exceeded = 1
			}
			results = append(results, ProbeResult{
				Zone:    zone,
				Check:   "email_auth",
				Success: true,
				Metrics: map[string]float64{
					"spf_lookup_count":           float64(count),
					"spf_lookup_budget_exceeded": exceeded,
					"spf_lookup_eval_complete":   boolToFloat(complete),
				},
			})
		}
	} else {
		logger.Warn("email_auth: SPF TXT query failed on all nameservers", "zone", zone)
	}

	// --- DMARC: TXT at _dmarc.<zone> ---
	dmarcName := "_dmarc." + dns.Fqdn(zone)
	dmarcTxts, dmarcOK := fetchTXTRecords(ctx, dmarcName, nameservers, client, logger)
	if dmarcOK {
		dmarc := analyzeDMARC(dmarcTxts)
		results = append(results, ProbeResult{
			Zone:    zone,
			Check:   "email_auth",
			Success: true,
			Metrics: map[string]float64{
				"dmarc_present":     boolToFloat(dmarc.present),
				"dmarc_valid":       boolToFloat(dmarc.valid),
				"dmarc_rua_present": boolToFloat(dmarc.rua),
				"dmarc_ruf_present": boolToFloat(dmarc.ruf),
			},
		})
		// Policy info gauge only when a valid p= is parsed.
		if dmarc.present && dmarc.valid {
			results = append(results, ProbeResult{
				Zone:    zone,
				Check:   "email_auth",
				Success: true,
				Metrics: map[string]float64{"dmarc_policy": 1},
				Labels:  map[string]string{"policy": dmarc.policy},
			})
		}
		if dmarc.subdomainPolicy != "" {
			results = append(results, ProbeResult{
				Zone:    zone,
				Check:   "email_auth",
				Success: true,
				Metrics: map[string]float64{"dmarc_sp_policy": 1},
				Labels:  map[string]string{"policy": dmarc.subdomainPolicy},
			})
		}
	} else {
		logger.Warn("email_auth: DMARC TXT query failed on all nameservers", "zone", zone)
	}

	return results, nil
}

// fetchTXTRecords queries TypeTXT at `name` against the zone's
// authoritative nameservers, returning the per-RR records with each RR's
// character-strings concatenated (RFC 7208 §3.3, RFC 7489 §A.5). It tries
// each nameserver until one answers; a NOERROR-but-empty or NXDOMAIN
// answer counts as a successful query returning no records (ok=true,
// empty slice) — that is the honest "record not present" signal. ok is
// false only when every nameserver failed to respond (the zone could not
// be queried at all).
func fetchTXTRecords(ctx context.Context, name string, nameservers []Nameserver, client *dns.Client, logger *slog.Logger) (records []string, ok bool) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), dns.TypeTXT)

	for _, ns := range nameservers {
		start := time.Now()
		resp, _, err := ExchangeWithRetry(ctx, client, msg, ResolveAddress(ns.IP))
		if err != nil {
			logger.Debug("email_auth: TXT query failed, trying next nameserver",
				"name", name, "ip", ns.IP, "err", err, "duration", time.Since(start))
			continue
		}
		// Only a NOERROR or NXDOMAIN response is an authoritative answer
		// (record present, or genuinely absent). A soft failure
		// (SERVFAIL / REFUSED / etc.) means THIS nameserver couldn't
		// answer — try the next one rather than misreporting the record
		// as absent and silently skipping a healthy sibling NS.
		if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError {
			logger.Debug("email_auth: TXT query non-answer rcode, trying next nameserver",
				"name", name, "ip", ns.IP, "rcode", dns.RcodeToString[resp.Rcode], "duration", time.Since(start))
			continue
		}
		var recs []string
		for _, rr := range resp.Answer {
			if txt, isTXT := rr.(*dns.TXT); isTXT {
				recs = append(recs, strings.Join(txt.Txt, ""))
			}
		}
		return recs, true
	}
	return nil, false
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
