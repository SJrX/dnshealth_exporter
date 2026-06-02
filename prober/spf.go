package prober

import "strings"

// spfResult is the pure-parse outcome for a zone's apex TXT records,
// covering exactly what the Tier-1 dashboard rows need (spec 009 US1):
// presence, the multiple-record PermError, the terminal `all`
// qualifier, and a narrow malformed signal. No DNS, no recursion — the
// RFC 7208 §4.6.4 lookup-budget check (the only resolution-requiring
// part) is deferred to issue #58.
type spfResult struct {
	present     bool   // at least one v=spf1 record at the apex
	recordCount int    // number of v=spf1 records (>1 ⇒ RFC 7208 §3.2 PermError)
	valid       bool   // exactly one record that parsed without malformation
	qualifier   string // terminal `all` qualifier: fail/softfail/neutral/pass/none
}

// analyzeSPF selects the v=spf1 record(s) from the per-RR TXT strings at
// a zone apex and classifies them. `records` is the list of already-
// concatenated TXT RRs (multi-string concatenation done by the caller,
// RFC 7208 §3.3). Selection is by the case-insensitive `v=spf1` version
// token; all other TXT records (verification tokens, etc.) are ignored.
func analyzeSPF(records []string) spfResult {
	var spf []string
	for _, r := range records {
		if isSPFRecord(r) {
			spf = append(spf, strings.TrimSpace(r))
		}
	}

	res := spfResult{recordCount: len(spf)}
	if len(spf) == 0 {
		return res // present=false; qualifier unset
	}
	res.present = true
	if len(spf) > 1 {
		// Multiple v=spf1 records is a permanent error (RFC 7208 §3.2):
		// a receiver cannot pick one. Mark invalid; the qualifier is
		// meaningless, so leave it unset (the prober suppresses the
		// terminal-qualifier info gauge unless exactly one record).
		return res
	}

	rec := spf[0]
	res.valid = !spfMalformed(rec)
	res.qualifier = terminalAllQualifier(rec)
	return res
}

// isSPFRecord reports whether a TXT record is an SPF record: it begins
// (case-insensitively) with the `v=spf1` version token, followed by
// whitespace or end-of-string (so `v=spf10`-style false positives are
// rejected).
func isSPFRecord(record string) bool {
	r := strings.TrimSpace(record)
	const tok = "v=spf1"
	if len(r) < len(tok) || !strings.EqualFold(r[:len(tok)], tok) {
		return false
	}
	rest := r[len(tok):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t'
}

// terminalAllQualifier returns the qualifier of the record's `all`
// mechanism — the last `all` term wins (RFC 7208 evaluates left-to-right
// and `all` short-circuits, so the effective terminal is the final one).
// Returns "none" when the record has no `all` mechanism. This is a
// syntactic read: a `redirect=` is NOT followed (that needs resolution,
// deferred to #58); when both `all` and `redirect` appear, `all` wins per
// RFC 7208, which this captures.
func terminalAllQualifier(record string) string {
	qualifier := "none"
	for _, f := range strings.Fields(record) {
		switch strings.ToLower(f) {
		case "all", "+all":
			qualifier = "pass"
		case "-all":
			qualifier = "fail"
		case "~all":
			qualifier = "softfail"
		case "?all":
			qualifier = "neutral"
		}
	}
	return qualifier
}

// spfMalformed flags the narrow, well-defined breakage the dashboard
// FAILs on (spec 009 R-9): a mechanism that is a bare qualifier with no
// body, or an `include:`/`redirect=` with an empty target. It does NOT
// attempt full RFC 7208 ABNF conformance and deliberately TOLERATES
// unknown-but-harmless terms, so a valid-but-exotic record is never
// false-FAILed.
func spfMalformed(record string) bool {
	fields := strings.Fields(record)
	if len(fields) == 0 {
		return true
	}
	for _, f := range fields[1:] { // skip the v=spf1 token
		// Strip a leading qualifier from mechanisms.
		body := f
		if strings.ContainsRune("+-~?", rune(body[0])) {
			body = body[1:]
			if body == "" {
				return true // bare qualifier, e.g. a lone "-"
			}
		}
		// An include/redirect with no target after the delimiter is broken.
		lower := strings.ToLower(body)
		if lower == "include:" || lower == "redirect=" {
			return true
		}
	}
	return false
}
