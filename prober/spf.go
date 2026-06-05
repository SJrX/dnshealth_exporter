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
	raw         string // the single record string (set only when present ∧ recordCount==1)
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
	res.raw = rec
	res.valid = !spfMalformed(rec)
	res.qualifier = terminalAllQualifier(rec)
	return res
}

// spfMechKind classifies an SPF term for lookup-budget counting
// (RFC 7208 §4.6.4). The six lookup-incurring kinds are include / a / mx
// / ptr / exists / redirect; only include and redirect pull in a further
// SPF record (and therefore recurse). `all` and everything else
// (ip4/ip6/exp/unknown) cost zero lookups.
type spfMechKind int

const (
	mechOther spfMechKind = iota
	mechAll
	mechInclude
	mechRedirect
	mechA
	mechMX
	mechPTR
	mechExists
)

// spfMechanism is one classified term of an SPF record.
type spfMechanism struct {
	kind     spfMechKind
	target   string // name after include: / redirect= (empty otherwise)
	hasMacro bool   // target embeds a %{...} macro (unresolvable without sender context)
}

// parseSPFMechanisms tokenizes an SPF record into classified mechanisms
// and reports whether the record has an `all` mechanism (so the counter
// can apply the redirect-precedence rule: redirect is consulted only when
// there is no `all`, RFC 7208 §6.1). Pure string work — no DNS.
func parseSPFMechanisms(record string) (mechs []spfMechanism, hasAll bool) {
	for _, f := range strings.Fields(record) {
		if strings.EqualFold(f, "v=spf1") {
			continue
		}
		// Strip a leading qualifier (+ - ~ ?) — applies to mechanisms,
		// not to the `redirect=`/`exp=` modifiers (which never start with
		// a qualifier char, so this leaves them untouched).
		body := f
		if strings.ContainsRune("+-~?", rune(body[0])) {
			body = body[1:]
		}
		lower := strings.ToLower(body)
		switch {
		case lower == "all":
			hasAll = true
			mechs = append(mechs, spfMechanism{kind: mechAll})
		case strings.HasPrefix(lower, "include:"):
			target := body[len("include:"):]
			mechs = append(mechs, spfMechanism{kind: mechInclude, target: target, hasMacro: strings.Contains(target, "%{")})
		case strings.HasPrefix(lower, "redirect="):
			target := body[len("redirect="):]
			mechs = append(mechs, spfMechanism{kind: mechRedirect, target: target, hasMacro: strings.Contains(target, "%{")})
		case lower == "a" || strings.HasPrefix(lower, "a:") || strings.HasPrefix(lower, "a/"):
			mechs = append(mechs, spfMechanism{kind: mechA})
		case lower == "mx" || strings.HasPrefix(lower, "mx:") || strings.HasPrefix(lower, "mx/"):
			mechs = append(mechs, spfMechanism{kind: mechMX})
		case lower == "ptr" || strings.HasPrefix(lower, "ptr:"):
			mechs = append(mechs, spfMechanism{kind: mechPTR})
		case strings.HasPrefix(lower, "exists:"):
			mechs = append(mechs, spfMechanism{kind: mechExists})
		default:
			// ip4: / ip6: / exp= / unknown — zero lookups.
			mechs = append(mechs, spfMechanism{kind: mechOther})
		}
	}
	return mechs, hasAll
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
