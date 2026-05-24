package prober

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/miekg/dns"
)

func init() {
	RegisterProber("mx", ProbeMX)
}

// ProbeMX queries each configured zone for MX records, then for every
// MX target hostname validates resolution (A/AAAA presence), checks
// for CNAME at the target (RFC 2181 §10.3 violation), validates LDH
// syntax (reusing isValidNSHostname), detects Null MX per RFC 7505,
// and surfaces per-MX info plus per-zone count gauges via the cycle
// runner's aggregation pass.
//
// See spec 008 for the full design and the "Scope of this feature"
// section disclaiming SMTP-protocol probing and email-auth records
// (SPF/DMARC/DKIM/etc., tracked as follow-up issue #44) as out of
// reach for this DNS-focused exporter.
func ProbeMX(ctx context.Context, zone string, nameservers []Nameserver, delegation *DelegationResult, client *dns.Client, logger *slog.Logger) ([]ProbeResult, error) {
	// Try parent-listed nameservers in order, breaking on the first
	// successful TypeMX response. MX records are zone data and should
	// be identical across auths per research R-1, so we don't fan out
	// the data — but we DO need to fail over if the first NS is down,
	// otherwise a single dead NS at index 0 would black out the zone's
	// MX panel entirely (every other prober in the cycle fans out).
	if len(nameservers) == 0 {
		return nil, nil
	}
	var (
		mxRRs       []*dns.MX
		queryErr    error
		queriedIP   string
		queriedAddr string
	)
	for _, ns := range nameservers {
		if ns.IP == "" {
			continue
		}
		queriedIP = ns.IP
		queriedAddr = ResolveAddress(ns.IP)
		mxRRs, queryErr = queryMX(ctx, zone, queriedAddr, client, logger)
		if queryErr == nil {
			break
		}
		// Failure — log and try next. The last error is reported
		// out below if all NSes fail.
		logger.Debug("mx query failed on this NS; trying next",
			"zone", zone, "nameserver", ns.Hostname, "ip", ns.IP, "err", queryErr)
	}
	if queryErr != nil {
		// Every parent-listed NS failed. Surface as query failure
		// against the LAST NS attempted (consistent with how cycle
		// counters attribute the failure).
		return []ProbeResult{{
			Zone:       zone,
			Check:      "mx",
			Nameserver: "",
			IP:         queriedIP,
			Success:    false,
			TimedOut:   IsTimeout(queryErr),
		}}, nil
	}

	// Per-target caches so duplicate targets (rare but possible)
	// don't re-query A/AAAA or CNAME.
	resolvesCache := make(map[string]bool)
	cnameCache := make(map[string]bool)

	var results []ProbeResult

	// Mark the TypeMX query itself as a successful prober run.
	// No metrics carried here — Null MX detection is derived by the
	// cycle runner from the per-MX info gauges below (avoids the
	// double-emission collision pattern that spec 007 D-1 documented).
	// IP attribution: the NS we actually got the answer from
	// (queriedIP), not nameservers[0].IP — important when failover
	// kicked in.
	results = append(results, ProbeResult{
		Zone:       zone,
		Check:      "mx",
		Nameserver: "",
		IP:         queriedIP,
		Success:    true,
	})

	// Per-target dedup: a zone may legally publish two MX RRs that
	// share the same target at different priorities (e.g.,
	// `10 mail` + `20 mail`). mx_info is per-RR (one series per
	// {target, priority}); the per-target validity gauges (mx_resolves,
	// mx_is_cname, mx_syntax_valid) are per-unique-target, dedup at
	// emit time so each target produces ONE series. See
	// contracts/mx-metrics.md `dnshealth_mx_resolves` cardinality note.
	emittedValidity := make(map[string]bool)

	for _, mxRR := range mxRRs {
		target := canonName(mxRR.Mx)
		priority := mxRR.Preference

		// Special-case Null MX's "." target — RFC 7505 doesn't
		// have it resolve to anything, and treating it as a real
		// hostname for resolution/CNAME checks would produce
		// spurious failures. Per data-model.md: emit the info gauge
		// but skip the per-target validity checks.
		isNullSentinel := target == "."

		// Info-result: one ProbeResult per MX RR, carrying ONLY
		// mx_info with {target, priority} labels. Split from the
		// validity-result below because mx_info is per-RR while
		// the validity metrics are per-unique-target — keeping
		// them on the same result would force `priority` onto
		// every metric (contract violation; also creates duplicate
		// per-target series for legal duplicate-target zones).
		// Nameserver/IP intentionally empty: MX is per-zone data,
		// not per-NS fan-out; the actual NS that answered is
		// attributed via the top-level success result above.
		infoResult := ProbeResult{
			Zone:       zone,
			Check:      "mx",
			Nameserver: "",
			IP:         "",
			Success:    true,
			Labels: map[string]string{
				"target": target,
				// Zero-pad to 5 digits (uint16 max = 65535 → 5
				// digits). String sort in Grafana's table SortBy
				// becomes numerically correct: "00005" < "00010"
				// < "00100", whereas an unpadded "5", "10", "100"
				// would sort as "10", "100", "5". The runner
				// aggregation parses this back to float for
				// is_primary derivation; operator-facing PromQL
				// filters like `priority="00010"` need the
				// padded form (documented in mx-metrics.md).
				"priority": fmt.Sprintf("%05d", priority),
			},
			Metrics: map[string]float64{
				"mx_info": 1,
			},
		}
		results = append(results, infoResult)

		// Validity-result: at most one per unique target, even if
		// the same target appears in multiple MX RRs. No `priority`
		// label — validity is a property of the target hostname,
		// independent of which RR(s) reference it.
		if emittedValidity[target] {
			continue
		}
		emittedValidity[target] = true

		validityResult := ProbeResult{
			Zone:       zone,
			Check:      "mx",
			Nameserver: "",
			IP:         "",
			Success:    true,
			Labels: map[string]string{
				"target": target,
			},
			Metrics: map[string]float64{},
		}

		if !isNullSentinel {
			// Resolves check (cached per target).
			var resolves float64
			if cached, ok := resolvesCache[target]; ok {
				if cached {
					resolves = 1
				}
			} else {
				ips, rerr := ResolveHostnames(ctx, target, client, logger)
				ok := rerr == nil && len(ips) > 0
				resolvesCache[target] = ok
				if ok {
					resolves = 1
				} else if rerr != nil {
					logger.Warn("mx target failed to resolve",
						"zone", zone, "target", target, "err", rerr)
				}
			}
			validityResult.Metrics["mx_resolves"] = resolves

			// CNAME check (cached per target).
			var isCNAME float64
			if cached, ok := cnameCache[target]; ok {
				if cached {
					isCNAME = 1
				}
			} else {
				flag, cerr := lookupCNAME(ctx, target, client, logger)
				ok := cerr == nil && flag
				cnameCache[target] = ok
				if ok {
					isCNAME = 1
					logger.Warn("mx target is a CNAME (RFC 2181 §10.3 violation)",
						"zone", zone, "target", target)
				}
			}
			validityResult.Metrics["mx_is_cname"] = isCNAME

			// Syntax check — pure local, no caching needed
			// (reuses spec N6's isValidNSHostname).
			var syntaxValid float64
			if isValidNSHostname(target) {
				syntaxValid = 1
			}
			validityResult.Metrics["mx_syntax_valid"] = syntaxValid
		} else {
			// Null MX's "." sentinel — syntactically valid by
			// RFC-defined construction, no resolution/CNAME
			// checks apply.
			validityResult.Metrics["mx_syntax_valid"] = 1
		}

		results = append(results, validityResult)
	}

	return results, nil
}

// queryMX issues a TypeMX query to the given server for the zone
// and returns the MX RRs in the Answer section. Empty slice + nil
// error means the zone has no MX records (NOERROR/NXDOMAIN with no
// MX in Answer — distinct from a query failure).
func queryMX(ctx context.Context, zone, server string, client *dns.Client, logger *slog.Logger) ([]*dns.MX, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(zone), dns.TypeMX)

	resp, _, err := ExchangeWithRetry(ctx, client, msg, server)
	if err != nil {
		logger.Warn("mx query failed", "zone", zone, "server", server, "err", err)
		return nil, err
	}

	var mxs []*dns.MX
	for _, rr := range resp.Answer {
		if mx, ok := rr.(*dns.MX); ok {
			mxs = append(mxs, mx)
		}
	}
	// Normalize MX target case-folding consistently with the rest of
	// the codebase (RFC 4343 case-insensitive comparison).
	for _, mx := range mxs {
		mx.Mx = strings.ToLower(dns.Fqdn(mx.Mx))
	}
	return mxs, nil
}
