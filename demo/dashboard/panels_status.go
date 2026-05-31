package main

import (
	"strings"

	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/table"
)

// statusTable is the shared builder for the three "X — status" tables
// (Parent, NS, SOA). Each table runs one or more instant boolean
// PromQL targets, reduces seriesToRows (one row per legendFormat), and
// renames the resulting two columns to Test / Result. The Result column
// gets the standard PASS/FAIL color background.
//
// Every check also carries a `detail` field — multi-line markdown text
// describing the metric, what a FAIL means in practice, and where to
// look next. These details are concatenated into the panel's
// description, which Grafana surfaces via the "i" icon in the panel
// header — keeps tables narrow while still giving the operator full
// context per check. A guard test (status_detail_guard_test.go)
// rejects any check whose detail is empty, so a future contributor
// adding a row can't forget to document it.
//
// Four-state status convention (constitution Principle IX): the Result
// cell renders one of four values, color-mapped by statusMappings():
//
//	0 → FAIL  (red)
//	1 → PASS  (green)
//	2 → N/A   (gray)    — the check does not apply to this zone
//	3 → WARN  (yellow)  — passes the hard check but a soft concern applies
//
//   - expr     — REQUIRED. The hard PASS/FAIL predicate (yields 0/1).
//   - naExpr   — OPTIONAL. Yields 1 when the check is not applicable.
//     When set and truthy it WINS over everything else (a check that
//     doesn't apply is neither pass, fail, nor warn).
//   - warnExpr — OPTIONAL. Yields 1 when a soft concern applies. Only
//     consulted when the hard check passes and the row is applicable.
//
// Rows that set ONLY expr (no naExpr/warnExpr) compile to byte-identical
// output as before — composeStatusExpr returns expr verbatim in that
// case. See constitution Principle IX for the standing convention.
type statusCheck struct {
	refId, expr, naExpr, warnExpr, legendFormat, detail string
}

// scalarizeStatusPredicate wraps a PromQL predicate so it always yields
// exactly one label-less 0/1 sample, even when the underlying series are
// absent this cycle. `max()` collapses the (already per-zone-filtered)
// predicate to a single label-less value; clamp_max pins it to 0/1 in
// case the inner expression is a raw gauge rather than a bool; the
// `or vector(0)` tail supplies 0 when the predicate matched nothing.
// Stripping labels to the empty set lets composeStatusExpr combine the
// pieces with plain `{} op {}` arithmetic without on()/ignoring() gymnastics.
func scalarizeStatusPredicate(p string) string {
	return "(clamp_max(max(" + p + "), 1) or vector(0))"
}

// composeStatusExpr folds a check's hard / na / warn predicates into the
// single 0/1/2/3 expression the Result cell renders. Priority is
// N/A > FAIL > WARN > PASS, encoded as:
//
//	value = 2·na + (1-na)·hard·(1 + 2·warn)
//
// Truth table (na, hard, warn each 0/1):
//
//	na=1                     → 2          (N/A — wins; the (1-na) factor zeroes the rest)
//	na=0, hard=0             → 0          (FAIL)
//	na=0, hard=1, warn=0     → 1          (PASS)
//	na=0, hard=1, warn=1     → 1+2 = 3    (WARN)
//
// Each sub-predicate is scalarized to a label-less 0/1 first, so the
// arithmetic always matches and always produces a value (no empty
// result → the Reduce transform always gets a row). A check with no
// naExpr/warnExpr returns expr unchanged so existing rows are untouched.
func composeStatusExpr(c statusCheck) string {
	if c.naExpr == "" && c.warnExpr == "" {
		return c.expr
	}
	hard := scalarizeStatusPredicate(c.expr)
	na := "vector(0)"
	if c.naExpr != "" {
		na = scalarizeStatusPredicate(c.naExpr)
	}
	warn := "vector(0)"
	if c.warnExpr != "" {
		warn = scalarizeStatusPredicate(c.warnExpr)
	}
	return "(2 * " + na + ") + ((1 - " + na + ") * " + hard + " * (1 + (2 * " + warn + ")))"
}

// buildStatusPanelDescription assembles each check's detail into a
// single markdown block keyed by the check's legendFormat. Grafana
// renders the panel-level description as markdown in the header
// tooltip, so headings + bullet lines stay readable.
func buildStatusPanelDescription(checks []statusCheck) string {
	var b strings.Builder
	for i, c := range checks {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("### ")
		b.WriteString(c.legendFormat)
		b.WriteString("\n\n")
		b.WriteString(c.detail)
		b.WriteString("\n")
	}
	return b.String()
}

func statusTable(title string, pos dashboard.GridPos, checks []statusCheck) *table.PanelBuilder {
	b := table.NewPanelBuilder().
		Title(title).
		Description(buildStatusPanelDescription(checks)).
		GridPos(pos).
		Datasource(prometheusDS).
		ShowHeader(true).
		CellHeight(common.TableCellHeightSm).
		WithTransformation(Reduce(ReduceOptions{
			Reducers: []string{"last"},
			Mode:     "seriesToRows",
		})).
		WithTransformation(Organize(OrganizeOptions{
			RenameByName: map[string]string{"Field": "Test", "Last": "Result"},
			IndexByName:  map[string]int{"Field": 0, "Last": 1},
		})).
		// Test column gets no width override so it auto-expands to
		// fill whatever space the panel allows — the test descriptions
		// are long enough that fixed widths kept truncating them.
		OverrideByName("Result", []dashboard.DynamicConfigValue{
			{Id: "mappings", Value: statusMappings()},
			{Id: "custom.cellOptions", Value: cellOptionsColorBackground()},
			// Narrow column — only ever holds PASS / FAIL / N/A / WARN
			// (all <=4 chars).
			{Id: "custom.width", Value: 80},
		})

	for _, c := range checks {
		b = b.WithTarget(prometheus.NewDataqueryBuilder().
			RefId(c.refId).
			Expr(composeStatusExpr(c)).
			LegendFormat(c.legendFormat).
			Instant())
	}
	return b
}

// Check lists are package-level vars (rather than inline literals)
// so the guard test in status_detail_guard_test.go can iterate them
// and reject any new row added without a detail string. The order of
// each slice determines the row order in the rendered table.

var parentStatusChecks = []statusCheck{
	{
		refId:        "A",
		expr:         `(count by (zone) (dnshealth_ns_record{source="parent",zone="$zone"}) > bool 0) or on() vector(0)`,
		legendFormat: "Parent has NS records for the zone",
		detail: "**Metric**: `dnshealth_ns_record{source=\"parent\"}`  \n" +
			"**Why FAIL matters**: The zone may be unreachable — resolvers can't find the authoritative NSs without a delegation from the parent.  \n" +
			"**Investigate**: NS records (from parent) panel below; check the TLD's view of the zone.",
	},
	// dnshealth_parent_delegation fires the same cycle the
	// delegation walk fails, whereas the row above only flips
	// when the resulting per-NS series go to zero. Kept as a
	// distinct row rather than a replacement — surfaces the
	// gauge directly so an operator can correlate it with the
	// query-counter panels without translating predicates.
	{
		refId:        "B",
		expr:         `dnshealth_parent_delegation{zone="$zone"} or on() vector(0)`,
		legendFormat: "Parent delegation walk succeeded",
		detail: "**Metric**: `dnshealth_parent_delegation`  \n" +
			"**Why FAIL matters**: The delegation walk from root failed entirely — every downstream check has no data for this zone this cycle.  \n" +
			"**Investigate**: Exporter logs (`delegation walk failed`); root-server reachability.",
	},
}

// nsCount is the per-zone distinct-nameserver count, shared by NS row A's
// hard (`>= 2`) and warn (`== 2`) predicates so the FAIL threshold and the
// WARN threshold can't silently drift apart (mirrors mxNoRealTargets). A
// change to how nameservers are counted now lives in exactly one place.
var nsCount = `count by (zone) (count by (zone, nameserver) (dnshealth_query_success{check="soa",zone="$zone"}))`

var nsStatusChecks = []statusCheck{
	// WARN at exactly 2 nameservers: two is the bare minimum (one NS
	// is a single point of failure, RFC 2182), but RFC 2182 §5
	// recommends more for diversity. 0-1 → FAIL, exactly 2 → WARN,
	// 3+ → PASS.
	{
		refId:        "A",
		expr:         `(` + nsCount + ` >= bool 2) or on() vector(0)`,
		warnExpr:     nsCount + ` == bool 2`,
		legendFormat: "Multiple authoritative nameservers (>=2)",
		detail: "**Metric**: distinct (zone, nameserver) tuples in `dnshealth_query_success{check=\"soa\"}`  \n" +
			"**Why FAIL matters**: A single NS is a single point of failure (RFC 2182). One outage = the whole zone goes dark.  \n" +
			"**WARN at exactly 2**: meets the bare minimum but RFC 2182 §5 recommends more nameservers for topological / provider diversity.  \n" +
			"**Investigate**: NS records panels below; add at least one more NS.",
	},
	{
		refId:        "B",
		expr:         `min by (zone) (dnshealth_query_success{check="soa",zone="$zone"}) or on() vector(0)`,
		legendFormat: "All NSs answered SOA authoritatively",
		detail: "**Metric**: `min(dnshealth_query_success{check=\"soa\"})`  \n" +
			"**Why FAIL matters**: At least one NS isn't responding correctly; further failures take the zone dark.  \n" +
			"**Investigate**: NS records (from the zone) — `Responded` column highlights which NS failed.",
	},
	{
		refId:        "C",
		expr:         `(max by (zone) (dnshealth_ns_recursion_available{zone="$zone"}) == bool 0) or on() vector(0)`,
		legendFormat: "No NS advertises recursion (RA=0)",
		detail: "**Metric**: `max(dnshealth_ns_recursion_available) == 0`  \n" +
			"**Why FAIL matters**: Open recursive resolvers can be abused for DNS amplification / reflection attacks.  \n" +
			"**Investigate**: NS records (from the zone) — `Recursion` column highlights the offending NS.",
	},
	// Set-equality on nameserver names: for every (zone, nameserver)
	// tuple in either source, BOTH source values must be present
	// (count by (zone, nameserver) over the source-collapsed series
	// must equal 2 everywhere). Previously this row checked only
	// that the counts matched — which passed when parent and self
	// advertised the same NUMBER of NSes with entirely different
	// names. See #36.
	{
		refId:        "D",
		expr:         `(min by (zone) (count by (zone, nameserver) (group by (zone, nameserver, source) (dnshealth_ns_record{zone="$zone"}))) == bool 2) or on() vector(0)`,
		legendFormat: "Parent and self report same NS records",
		detail: "**Metric**: per-nameserver count of distinct sources in `dnshealth_ns_record`, aggregated `min by (zone) == 2`  \n" +
			"**Why FAIL matters**: At least one NS hostname appears on only one side — parent and zone disagree on the NS set, so resolvers picking either side may reach servers the other doesn't know about. The check is set-equality on **names**; IP-level glue disagreement is a separate concern (#37).  \n" +
			"**Investigate**: NS records (from parent) and NS records (from the zone) tables side-by-side — the hostname appearing in only one table is the discrepancy.",
	},
	// min-aggregation: PASS only if EVERY NS hostname is
	// syntactically valid; any one bad hostname fails the zone.
	{
		refId:        "E",
		expr:         `min by (zone) (dnshealth_ns_hostname_syntax_valid{zone="$zone"}) or on() vector(0)`,
		legendFormat: "All NS hostnames syntactically valid (LDH)",
		detail: "**Metric**: `min(dnshealth_ns_hostname_syntax_valid)`  \n" +
			"**Why FAIL matters**: Hostnames violating RFC 952 / 1123 (underscores, leading/trailing hyphens) may be rejected by strict resolvers.  \n" +
			"**Investigate**: NS records tables show each NS hostname — find the one with the invalid character.",
	},
	// max-aggregation inverted: PASS if no NS hostname is a
	// CNAME (RFC 2181 §10.3). A single CNAMEd NS fails the row.
	{
		refId:        "F",
		expr:         `((max by (zone) (dnshealth_ns_hostname_is_cname{zone="$zone"})) == bool 0) or on() vector(0)`,
		legendFormat: "No NS hostname is a CNAME (RFC 2181 §10.3)",
		detail: "**Metric**: `max(dnshealth_ns_hostname_is_cname) == 0`  \n" +
			"**Why FAIL matters**: NS records pointing at a CNAME violate RFC 2181 §10.3; resolver handling is inconsistent → intermittent resolution failures.  \n" +
			"**Investigate**: NS records tables show each NS hostname; check its CNAME chain externally.",
	},
	// Stealth NS detection (spec 007). PASS only if both
	// self-only and parent-only counts are zero — i.e., the
	// parent's NS set and the union of self-side views are
	// identical. FAIL otherwise OR when no series exist (the
	// `or on() vector(0)` fallback covers the "no data this
	// cycle" case so an outage doesn't silently read PASS).
	{
		refId:        "G",
		expr:         `(max by (zone) (dnshealth_ns_classification_count{classification="self-only",zone="$zone"}) == bool 0) and on(zone) (max by (zone) (dnshealth_ns_classification_count{classification="parent-only",zone="$zone"}) == bool 0) or on() vector(0)`,
		legendFormat: "No stealth NSes (parent and self agree on NS set)",
		detail: "**Metric**: `dnshealth_ns_classification_count{classification=\"self-only\"|\"parent-only\"}`  \n" +
			"**Why FAIL matters**: At least one NS hostname is asymmetric — the parent advertises an NS the zone's own authoritative servers don't list (parent-only), or an auth lists an NS the parent doesn't (self-only, the \"stealth\" case). Self-only divergence can be a legitimate hidden-master setup (NOTIFY-driven primary not in the public NS set, by design) — verify intent before alerting.  \n" +
			"**Scope limitation**: This row detects asymmetry between sources we can query. RFC 8499 \"stealth\" servers — those absent from EVERY public source — are not detectable by any single-vantage-point exporter and remain invisible to this check. See spec 007.  \n" +
			"**Investigate**: NS records (from parent) and NS records (from the zone) tables side-by-side — the hostname appearing in only one table is the asymmetric NS. For self-only cases, check `dnshealth_ns_stealth_reachable{nameserver=X}` — 1 = working hidden master, 0 = leaked listing.",
	},
}

// soaNoData is the shared N/A predicate for the SOA rows: 1 when the
// zone produced no SOA data this cycle (every NS failed to answer SOA,
// so the prober emits no dnshealth_soa_* series — e.g. lame-nameserver.
// demo. / missing-glue.demo.). Without this the rows fall through their
// `or vector(0)` tail to 0 = FAIL, falsely reporting an MNAME problem
// when the truth is "couldn't ask" (issue #49 / spec 008 audit D-9).
//
// absent() yields a 1-valued series exactly when the selector matches
// nothing, and empty when it matches — which scalarizeStatusPredicate
// turns into the 1/0 the four-state arithmetic needs. (A `count(...) ==
// bool 0` form does NOT work: with no series, count is empty, == bool 0
// is empty, and the N/A branch never fires — the same empty-vector trap
// as audit D-4.)
var soaNoData = `absent(dnshealth_soa_serial{zone="$zone"})`

var soaStatusChecks = []statusCheck{
	{
		refId:        "A",
		expr:         `((max by (zone) (dnshealth_soa_serial{zone="$zone"}) - min by (zone) (dnshealth_soa_serial{zone="$zone"})) == bool 0) and on(zone) (count by (zone) (dnshealth_soa_serial{zone="$zone"}) > bool 0) or on() vector(0)`,
		naExpr:       soaNoData,
		legendFormat: "All NSs report same SOA serial",
		detail: "**Metric**: `max(dnshealth_soa_serial) - min(dnshealth_soa_serial) == 0`  \n" +
			"**Why FAIL matters**: Secondaries haven't pulled the latest zone — resolvers may serve stale data depending on which NS they hit.  \n" +
			"**N/A**: no NS answered the SOA query this cycle — there are no serials to compare.  \n" +
			"**Investigate**: SOA per-nameserver table below — find the NS reporting an out-of-date serial.",
	},
	// MNAME validity (proposals S1). MNAME-in-set is checked with a
	// soft (WARN) verdict rather than FAIL: an MNAME outside the NS
	// set is a legitimate, common hidden-master pattern (NOTIFY-driven
	// primary deliberately kept out of the public NS set), not an
	// error — so it warrants "verify intent", not red. Row C still
	// hard-FAILs if the MNAME doesn't resolve, which is genuinely
	// broken NOTIFY. (issue #49)
	{
		refId: "B",
		// hard = "we have SOA data to evaluate" — this row never hard-
		// FAILs; its only adverse state is the WARN below. N/A (no data)
		// is handled by naExpr; PASS = MNAME in set; WARN = MNAME not in
		// set.
		expr:         `(count by (zone) (dnshealth_soa_serial{zone="$zone"}) >= bool 1) or on() vector(0)`,
		warnExpr:     `min by (zone) (dnshealth_soa_mname_in_ns_set{zone="$zone"}) == bool 0`,
		naExpr:       soaNoData,
		legendFormat: "SOA MNAME is in zone's NS RR set",
		detail: "**Metric**: `min(dnshealth_soa_mname_in_ns_set)`  \n" +
			"**WARN (not FAIL) matters**: The MNAME identifies the primary master. An MNAME outside the NS set is RFC-allowed and normal for hidden-master setups (NOTIFY-driven primary not in the public NS set) — so this is a soft 'verify intent' warning, not an error. If it's unintentional, NOTIFY may target the wrong server.  \n" +
			"**N/A**: no NS answered the SOA query this cycle.  \n" +
			"**Investigate**: SOA per-nameserver table — each row's `mname` label vs. the NS records tables.",
	},
	{
		refId:        "C",
		expr:         `min by (zone) (dnshealth_soa_mname_resolves{zone="$zone"}) or on() vector(0)`,
		naExpr:       soaNoData,
		legendFormat: "SOA MNAME hostname resolves to A or AAAA",
		detail: "**Metric**: `min(dnshealth_soa_mname_resolves)`  \n" +
			"**Why FAIL matters**: NOTIFY and dynamic updates target the MNAME hostname; if it doesn't resolve, both mechanisms break silently.  \n" +
			"**N/A**: no NS answered the SOA query this cycle — there is no MNAME to resolve.  \n" +
			"**Investigate**: SOA per-nameserver table shows the `mname` label — try resolving that hostname externally.",
	},
}

func parentStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("Parent — status", gridPos(0, subY(4, yOffset), 8, 10), parentStatusChecks)
}

func nsStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("NS — status", gridPos(8, subY(4, yOffset), 8, 10), nsStatusChecks)
}

func soaStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("SOA — status", gridPos(16, subY(4, yOffset), 8, 10), soaStatusChecks)
}

// MX status checks (spec 008). Rows B/C/D use the four-state convention
// (constitution Principle IX): they read N/A (gray) for zones with no MX
// records or with Null MX, since "do the MX targets resolve / avoid
// CNAMEs / parse" is a meaningless question when there are no real MX
// targets. The naExpr replaces the earlier clamp_max vacuous-PASS hack —
// N/A is the honest signal that the check didn't apply, rather than a
// green PASS that looks like the check ran and succeeded. Row A WARNs
// when the zone has exactly one real MX (works, but no backup).
//
// mxNoRealTargets is the shared "not applicable" predicate: 1 when the
// zone has no MX records at all, or publishes Null MX (`0 .`).
//
// Uses clamp_max(sum, 1), NOT `a or b`: both `== bool` comparisons
// yield a PRESENT series (value 0 or 1) for every configured zone,
// and PromQL's `or` returns the left operand whenever it exists —
// so `(count==0) or (null==1)` collapses to just `count==0` and the
// Null-MX branch never fires. Summing the two booleans and clamping
// to 1 ORs them honestly. Same trap documented in spec 008 audit D-4.
var mxNoRealTargets = `clamp_max((dnshealth_mx_count{zone="$zone"} == bool 0) + on(zone) (dnshealth_mx_null_mx{zone="$zone"} == bool 1), 1)`

var mxStatusChecks = []statusCheck{
	{
		refId: "A",
		expr:  `((dnshealth_mx_count{zone="$zone"} > bool 0) or on(zone) (dnshealth_mx_null_mx{zone="$zone"} == bool 1)) or on() vector(0)`,
		// WARN when exactly one real MX RR and not Null MX — mail works
		// but there is no backup MX, so a single MX outage = no inbound.
		warnExpr:     `(dnshealth_mx_count{zone="$zone"} == bool 1) * on(zone) (dnshealth_mx_null_mx{zone="$zone"} == bool 0)`,
		legendFormat: "Zone has MX records (or Null MX intentionally set)",
		detail: "**Metric**: `dnshealth_mx_count` + `dnshealth_mx_null_mx`  \n" +
			"**Why FAIL matters**: Zone publishes no MX records AND no Null MX declaration — all incoming email fails. Either publish MX records or declare Null MX (`0 .`) per RFC 7505 if no email is intended.  \n" +
			"**WARN at exactly one MX**: mail is deliverable but there is no backup MX — a single MX outage takes inbound mail down. Add a lower-priority backup MX for redundancy.  \n" +
			"**Investigate**: per-MX records table below shows what the zone advertises; check the auth's zone file for the MX RR set.",
	},
	{
		refId:        "B",
		expr:         `(dnshealth_mx_count{zone="$zone"} == bool dnshealth_mx_resolved_count{zone="$zone"}) or on() vector(0)`,
		naExpr:       mxNoRealTargets,
		legendFormat: "All MX targets resolve",
		detail: "**Metric**: `dnshealth_mx_count == dnshealth_mx_resolved_count`  \n" +
			"**Why FAIL matters**: At least one MX target's hostname doesn't resolve to A or AAAA. Inbound mail attempts for that target will fail.  \n" +
			"**N/A**: the zone has no MX records, or is Null MX (`0 .`) — there are no targets to resolve, so the check does not apply.  \n" +
			"**Investigate**: per-MX records table — `resolves=0` column identifies the offending target.",
	},
	{
		refId:        "C",
		expr:         `(dnshealth_mx_cname_count{zone="$zone"} == bool 0) or on() vector(0)`,
		naExpr:       mxNoRealTargets,
		legendFormat: "No MX target is a CNAME (RFC 2181 §10.3)",
		detail: "**Metric**: `dnshealth_mx_cname_count == 0`  \n" +
			"**Why FAIL matters**: At least one MX target is an alias (CNAME), violating RFC 2181 §10.3. Many MTAs handle this inconsistently; some refuse delivery outright.  \n" +
			"**N/A**: the zone has no MX records, or is Null MX — no targets to check.  \n" +
			"**Investigate**: per-MX records table — `is_cname=1` column identifies the offending target.",
	},
	{
		refId:        "D",
		expr:         `(dnshealth_mx_count{zone="$zone"} == bool dnshealth_mx_syntax_valid_count{zone="$zone"}) or on() vector(0)`,
		naExpr:       mxNoRealTargets,
		legendFormat: "All MX target hostnames syntactically valid (LDH)",
		detail: "**Metric**: `dnshealth_mx_count == dnshealth_mx_syntax_valid_count`  \n" +
			"**Why FAIL matters**: At least one MX target hostname has invalid syntax (underscore, leading/trailing hyphen, etc.) per RFC 952/1123. Some strict resolvers may reject.  \n" +
			"**N/A**: the zone has no MX records, or is Null MX — no hostnames to validate.  \n" +
			"**Investigate**: per-MX records table; the same LDH check is applied to NS hostnames in spec N6 — see that row for the validator details.",
	},
	{
		refId: "E",
		// FAIL iff a Null MX RR coexists with other MX RRs:
		// has_null_mx_rr==1 AND count>1. PASS = 1 - that product.
		// Phrased with multiplication (not `or`) for the same reason
		// as mxNoRealTargets above — the old `(has_null==0) or (count==1)`
		// form short-circuited on the always-present left series and
		// returned FAIL for the legitimate canonical Null MX zone
		// (count==1). Caught by live PromQL evaluation post-reboot;
		// smoke never exercised this predicate (issue #46).
		//
		// Trailing `or on() vector(0)`: this row is binary (no
		// naExpr/warnExpr) so composeStatusExpr returns it verbatim and
		// does NOT scalarize it. Without the fallback, absent series
		// (cold start before the first cycle, or a zone removed from
		// config) make `1 - <empty>` evaluate to empty → the row renders
		// blank instead of a value. Every other binary row carries the
		// same tail.
		expr:         `(1 - clamp_max((dnshealth_mx_has_null_mx_rr{zone="$zone"} == bool 1) * on(zone) (dnshealth_mx_count{zone="$zone"} > bool 1), 1)) or on() vector(0)`,
		legendFormat: "No conflict between Null MX and real MX records",
		detail: "**Metric**: derived from `dnshealth_mx_has_null_mx_rr` + `dnshealth_mx_count`  \n" +
			"**Why FAIL matters**: Zone publishes a Null MX RR (`0 .`) AND additional MX records. RFC 7505 §3 requires Null MX to be the SOLE MX record; coexistence is undefined and likely interpreted differently by different MTAs.  \n" +
			"**Investigate**: auth's zone file — remove either the Null MX RR (if you want to accept email) or the real MX records (if you don't). The distinction between `dnshealth_mx_null_mx` (canonical form only) and `dnshealth_mx_has_null_mx_rr` (any Null MX RR present, regardless of count) is what makes this row catchable — see spec 008 audit D-5.",
	},
}

// mxStatusTable is the left half (w=12) of the MX section row, paired
// with mxRecordsTable on the right. Height 10 to match the records
// table and to give each of the 5 status rows visual breathing room
// at CellHeight=Sm. The pair sits inside a collapsible "MX" row at
// Y=22 (header) + Y=23 (panels). yOffset shifts the section up
// cleanly when the markdown info-panel header is absent.
func mxStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("MX — status", gridPos(0, subY(25, yOffset), 12, 10), mxStatusChecks)
}
