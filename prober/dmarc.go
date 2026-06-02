package prober

import "strings"

// dmarcResult is the pure-parse outcome for a zone's `_dmarc` TXT
// records (spec 009 US2). DMARC's grammar is a simple `tag=value;` list
// (RFC 7489 §6.3) — no qualifiers, macros, or recursion — so this is a
// thin hand parser.
type dmarcResult struct {
	present         bool   // a v=DMARC1 record exists at _dmarc.<zone>
	valid           bool   // present AND carries a valid p= tag
	policy          string // p= value: none/quarantine/reject ("" if absent/malformed)
	subdomainPolicy string // sp= value if present
	rua             bool   // a rua= (aggregate reporting) tag is present
	ruf             bool   // a ruf= (forensic reporting) tag is present
}

// analyzeDMARC selects the v=DMARC1 record from the per-RR TXT strings at
// `_dmarc.<zone>` and parses its tags. NXDOMAIN/NODATA both surface as an
// empty `records` slice ⇒ not present (handled by the caller).
func analyzeDMARC(records []string) dmarcResult {
	var rec string
	for _, r := range records {
		if isDMARCRecord(r) {
			rec = strings.TrimSpace(r)
			break
		}
	}
	if rec == "" {
		return dmarcResult{} // not present
	}

	res := dmarcResult{present: true}
	tags := parseDMARCTags(rec)
	if p := tags["p"]; p == "none" || p == "quarantine" || p == "reject" {
		// RFC 7489 §6.3: a valid record requires the p= policy tag.
		res.valid = true
		res.policy = p
	}
	if sp := tags["sp"]; sp == "none" || sp == "quarantine" || sp == "reject" {
		res.subdomainPolicy = sp
	}
	_, res.rua = tags["rua"]
	_, res.ruf = tags["ruf"]
	return res
}

// isDMARCRecord reports whether a TXT record begins (case-insensitively)
// with the `v=DMARC1` version token.
func isDMARCRecord(record string) bool {
	r := strings.TrimSpace(record)
	const tok = "v=DMARC1"
	if len(r) < len(tok) || !strings.EqualFold(r[:len(tok)], tok) {
		return false
	}
	rest := r[len(tok):]
	return rest == "" || rest[0] == ' ' || rest[0] == ';' || rest[0] == '\t'
}

// parseDMARCTags splits a DMARC record into its `tag=value` pairs,
// returning a map of lowercased tag name → lowercased value. Whitespace
// around tags and values is trimmed (RFC 7489 §6.4 permits it).
func parseDMARCTags(record string) map[string]string {
	tags := make(map[string]string)
	for _, part := range strings.Split(record, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val := strings.ToLower(strings.TrimSpace(kv[1]))
		if key == "" || key == "v" {
			continue
		}
		if _, dup := tags[key]; !dup {
			tags[key] = val
		}
	}
	return tags
}
