package prober

import "strings"

// SPF DNS-lookup budget (RFC 7208 §4.6.4): an SPF record may cause at
// most this many DNS-lookup-incurring mechanisms when evaluated.
const spfMaxLookups = 10

// spfMaxDepth bounds the include/redirect recursion well past any
// legitimate SPF nesting — belt-and-suspenders alongside the visited-set
// and the stop-at-11 rule, so a pathological record can never recurse
// unboundedly.
const spfMaxDepth = 20

// fetchSPFFunc fetches the SPF record published at `name` (an include/
// redirect target). Returns ok=false if the name has no SPF record or
// could not be resolved. Injected so the counter is pure and unit-
// testable with no DNS; the production impl is resolveSPFRecord.
type fetchSPFFunc func(name string) (record string, ok bool)

// countSPFLookups counts the DNS-lookup-incurring mechanisms required to
// evaluate `record`, recursing through include/redirect targets via
// fetch (research R-1/R-2). It returns:
//
//   - count: the exact total for an in-budget record (0–10); for an
//     over-budget record the walk stops the instant the count exceeds 10
//     and returns 11 ("≥11" — clarification Q3).
//   - complete: false iff any include/redirect branch was unreachable,
//     macro-bearing, or cycle/depth-truncated, so count/exceeded are a
//     lower bound (research R-3).
//
// a/mx/ptr/exists are counted +1 with NO fetch (they pull in no further
// SPF terms). Only include/redirect are resolved. Bounded by the visited
// set (cycle-safe), the depth cap, and stop-at-11 — it cannot hang.
func countSPFLookups(record string, fetch fetchSPFFunc) (count int, complete bool) {
	c := spfCounter{fetch: fetch, visited: map[string]bool{}, complete: true}
	c.walk(record, 0)
	if c.count > spfMaxLookups {
		return spfMaxLookups + 1, c.complete // "≥11"
	}
	return c.count, c.complete
}

type spfCounter struct {
	fetch    fetchSPFFunc
	visited  map[string]bool // include/redirect targets already expanded (cycle guard)
	count    int
	complete bool
}

// over reports whether the budget is already blown (stop-early).
func (c *spfCounter) over() bool { return c.count > spfMaxLookups }

func (c *spfCounter) walk(record string, depth int) {
	mechs, hasAll := parseSPFMechanisms(record)
	for _, m := range mechs {
		if c.over() {
			return
		}
		switch m.kind {
		case mechA, mechMX, mechPTR, mechExists:
			c.count++
		case mechInclude:
			c.count++
			if c.over() {
				return
			}
			c.recurse(m, depth)
		case mechRedirect:
			// redirect is consulted only when there is no `all` (RFC 7208 §6.1).
			if hasAll {
				continue
			}
			c.count++
			if c.over() {
				return
			}
			c.recurse(m, depth)
		}
		// mechAll, mechOther: zero lookups.
	}
}

func (c *spfCounter) recurse(m spfMechanism, depth int) {
	if m.hasMacro {
		// A macro target can't be expanded without sender context — the
		// branch is unresolvable, so the evaluation is incomplete.
		c.complete = false
		return
	}
	key := strings.ToLower(strings.TrimSuffix(m.target, "."))
	if c.visited[key] || depth >= spfMaxDepth {
		// Cyclic / over-deep include chain: a real PermError. Report it
		// as over budget (the operator's fix is the same) and incomplete
		// (research R-5 / spec US1.4) rather than hanging or erroring.
		c.count = spfMaxLookups + 1
		c.complete = false
		return
	}
	rec, ok := c.fetch(m.target)
	if !ok {
		// Unreachable include — count its own +1 (already added by the
		// caller) but no sub-count, and flag incomplete (no false FAIL).
		c.complete = false
		return
	}
	c.visited[key] = true
	c.walk(rec, depth+1)
}
