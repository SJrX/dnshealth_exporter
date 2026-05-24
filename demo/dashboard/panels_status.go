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
type statusCheck struct {
	refId, expr, legendFormat, detail string
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
			{Id: "mappings", Value: passFailMappings()},
			{Id: "custom.cellOptions", Value: cellOptionsColorBackground()},
			// Narrow column — only ever holds "PASS" or "FAIL".
			{Id: "custom.width", Value: 80},
		})

	for _, c := range checks {
		b = b.WithTarget(prometheus.NewDataqueryBuilder().
			RefId(c.refId).
			Expr(c.expr).
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
	{"A",
		`(count by (zone) (dnshealth_ns_record{source="parent",zone="$zone"}) > bool 0) or on() vector(0)`,
		"Parent has NS records for the zone",
		"**Metric**: `dnshealth_ns_record{source=\"parent\"}`  \n" +
			"**Why FAIL matters**: The zone may be unreachable — resolvers can't find the authoritative NSs without a delegation from the parent.  \n" +
			"**Investigate**: NS records (from parent) panel below; check the TLD's view of the zone.",
	},
	// dnshealth_parent_delegation fires the same cycle the
	// delegation walk fails, whereas the row above only flips
	// when the resulting per-NS series go to zero. Kept as a
	// distinct row rather than a replacement — surfaces the
	// gauge directly so an operator can correlate it with the
	// query-counter panels without translating predicates.
	{"B",
		`dnshealth_parent_delegation{zone="$zone"} or on() vector(0)`,
		"Parent delegation walk succeeded",
		"**Metric**: `dnshealth_parent_delegation`  \n" +
			"**Why FAIL matters**: The delegation walk from root failed entirely — every downstream check has no data for this zone this cycle.  \n" +
			"**Investigate**: Exporter logs (`delegation walk failed`); root-server reachability.",
	},
}

var nsStatusChecks = []statusCheck{
	{"A",
		`(count by (zone) (count by (zone, nameserver) (dnshealth_query_success{check="soa",zone="$zone"})) >= bool 2) or on() vector(0)`,
		"Multiple authoritative nameservers (>=2)",
		"**Metric**: distinct (zone, nameserver) tuples in `dnshealth_query_success{check=\"soa\"}`  \n" +
			"**Why FAIL matters**: A single NS is a single point of failure (RFC 2182). One outage = the whole zone goes dark.  \n" +
			"**Investigate**: NS records panels below; add at least one more NS.",
	},
	{"B",
		`min by (zone) (dnshealth_query_success{check="soa",zone="$zone"}) or on() vector(0)`,
		"All NSs answered SOA authoritatively",
		"**Metric**: `min(dnshealth_query_success{check=\"soa\"})`  \n" +
			"**Why FAIL matters**: At least one NS isn't responding correctly; further failures take the zone dark.  \n" +
			"**Investigate**: NS records (from the zone) — `Responded` column highlights which NS failed.",
	},
	{"C",
		`(max by (zone) (dnshealth_ns_recursion_available{zone="$zone"}) == bool 0) or on() vector(0)`,
		"No NS advertises recursion (RA=0)",
		"**Metric**: `max(dnshealth_ns_recursion_available) == 0`  \n" +
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
	{"D",
		`(min by (zone) (count by (zone, nameserver) (group by (zone, nameserver, source) (dnshealth_ns_record{zone="$zone"}))) == bool 2) or on() vector(0)`,
		"Parent and self report same NS records",
		"**Metric**: per-nameserver count of distinct sources in `dnshealth_ns_record`, aggregated `min by (zone) == 2`  \n" +
			"**Why FAIL matters**: At least one NS hostname appears on only one side — parent and zone disagree on the NS set, so resolvers picking either side may reach servers the other doesn't know about. The check is set-equality on **names**; IP-level glue disagreement is a separate concern (#37).  \n" +
			"**Investigate**: NS records (from parent) and NS records (from the zone) tables side-by-side — the hostname appearing in only one table is the discrepancy.",
	},
	// min-aggregation: PASS only if EVERY NS hostname is
	// syntactically valid; any one bad hostname fails the zone.
	{"E",
		`min by (zone) (dnshealth_ns_hostname_syntax_valid{zone="$zone"}) or on() vector(0)`,
		"All NS hostnames syntactically valid (LDH)",
		"**Metric**: `min(dnshealth_ns_hostname_syntax_valid)`  \n" +
			"**Why FAIL matters**: Hostnames violating RFC 952 / 1123 (underscores, leading/trailing hyphens) may be rejected by strict resolvers.  \n" +
			"**Investigate**: NS records tables show each NS hostname — find the one with the invalid character.",
	},
	// max-aggregation inverted: PASS if no NS hostname is a
	// CNAME (RFC 2181 §10.3). A single CNAMEd NS fails the row.
	{"F",
		`((max by (zone) (dnshealth_ns_hostname_is_cname{zone="$zone"})) == bool 0) or on() vector(0)`,
		"No NS hostname is a CNAME (RFC 2181 §10.3)",
		"**Metric**: `max(dnshealth_ns_hostname_is_cname) == 0`  \n" +
			"**Why FAIL matters**: NS records pointing at a CNAME violate RFC 2181 §10.3; resolver handling is inconsistent → intermittent resolution failures.  \n" +
			"**Investigate**: NS records tables show each NS hostname; check its CNAME chain externally.",
	},
	// Stealth NS detection (spec 007). PASS only if both
	// self-only and parent-only counts are zero — i.e., the
	// parent's NS set and the union of self-side views are
	// identical. FAIL otherwise OR when no series exist (the
	// `or on() vector(0)` fallback covers the "no data this
	// cycle" case so an outage doesn't silently read PASS).
	{"G",
		`(max by (zone) (dnshealth_ns_classification_count{classification="self-only",zone="$zone"}) == bool 0) and on(zone) (max by (zone) (dnshealth_ns_classification_count{classification="parent-only",zone="$zone"}) == bool 0) or on() vector(0)`,
		"No stealth NSes (parent and self agree on NS set)",
		"**Metric**: `dnshealth_ns_classification_count{classification=\"self-only\"|\"parent-only\"}`  \n" +
			"**Why FAIL matters**: At least one NS hostname is asymmetric — the parent advertises an NS the zone's own authoritative servers don't list (parent-only), or an auth lists an NS the parent doesn't (self-only, the \"stealth\" case). Self-only divergence can be a legitimate hidden-master setup (NOTIFY-driven primary not in the public NS set, by design) — verify intent before alerting.  \n" +
			"**Scope limitation**: This row detects asymmetry between sources we can query. RFC 8499 \"stealth\" servers — those absent from EVERY public source — are not detectable by any single-vantage-point exporter and remain invisible to this check. See spec 007.  \n" +
			"**Investigate**: NS records (from parent) and NS records (from the zone) tables side-by-side — the hostname appearing in only one table is the asymmetric NS. For self-only cases, check `dnshealth_ns_stealth_reachable{nameserver=X}` — 1 = working hidden master, 0 = leaked listing.",
	},
}

var soaStatusChecks = []statusCheck{
	{"A",
		`((max by (zone) (dnshealth_soa_serial{zone="$zone"}) - min by (zone) (dnshealth_soa_serial{zone="$zone"})) == bool 0) and on(zone) (count by (zone) (dnshealth_soa_serial{zone="$zone"}) > bool 0) or on() vector(0)`,
		"All NSs report same SOA serial",
		"**Metric**: `max(dnshealth_soa_serial) - min(dnshealth_soa_serial) == 0`  \n" +
			"**Why FAIL matters**: Secondaries haven't pulled the latest zone — resolvers may serve stale data depending on which NS they hit.  \n" +
			"**Investigate**: SOA per-nameserver table below — find the NS reporting an out-of-date serial.",
	},
	// MNAME validity (proposals S1). min-aggregation: PASS
	// only if every NS reports a MNAME that is in the NS set
	// and resolves; a single dissenting NS fails the row.
	{"B",
		`min by (zone) (dnshealth_soa_mname_in_ns_set{zone="$zone"}) or on() vector(0)`,
		"SOA MNAME is in zone's NS RR set",
		"**Metric**: `min(dnshealth_soa_mname_in_ns_set)`  \n" +
			"**Why FAIL matters**: The MNAME identifies the primary master. If it's outside the NS set, NOTIFY may target the wrong server (hidden-master setups legitimately read 0 here — verify intent before alerting).  \n" +
			"**Investigate**: SOA per-nameserver table — each row's `mname` label vs. the NS records tables.",
	},
	{"C",
		`min by (zone) (dnshealth_soa_mname_resolves{zone="$zone"}) or on() vector(0)`,
		"SOA MNAME hostname resolves to A or AAAA",
		"**Metric**: `min(dnshealth_soa_mname_resolves)`  \n" +
			"**Why FAIL matters**: NOTIFY and dynamic updates target the MNAME hostname; if it doesn't resolve, both mechanisms break silently.  \n" +
			"**Investigate**: SOA per-nameserver table shows the `mname` label — try resolving that hostname externally.",
	},
}

func parentStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("Parent — status", gridPos(0, subY(4, yOffset), 8, 8), parentStatusChecks)
}

func nsStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("NS — status", gridPos(8, subY(4, yOffset), 8, 8), nsStatusChecks)
}

func soaStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("SOA — status", gridPos(16, subY(4, yOffset), 8, 8), soaStatusChecks)
}

// MX status checks (spec 008). Row B includes the Null-MX
// suppression branch per /speckit-analyze C1 remediation —
// without it, Null-MX zones spuriously FAIL because the `.`
// target intentionally has no `mx_resolves` emission.
var mxStatusChecks = []statusCheck{
	{"A",
		`((dnshealth_mx_count{zone="$zone"} > bool 0) or on(zone) (dnshealth_mx_null_mx{zone="$zone"} == bool 1)) or on() vector(0)`,
		"Zone has MX records (or Null MX intentionally set)",
		"**Metric**: `dnshealth_mx_count` + `dnshealth_mx_null_mx`  \n" +
			"**Why FAIL matters**: Zone publishes no MX records AND no Null MX declaration — all incoming email fails. Either publish MX records or declare Null MX (`0 .`) per RFC 7505 if no email is intended.  \n" +
			"**Investigate**: per-MX records table below shows what the zone advertises; check the auth's zone file for the MX RR set.",
	},
	{"B",
		`clamp_max((dnshealth_mx_count{zone="$zone"} == bool 0) + on(zone) (dnshealth_mx_null_mx{zone="$zone"} == bool 1) + on(zone) ((dnshealth_mx_count{zone="$zone"} > bool 0) * on(zone) (dnshealth_mx_count{zone="$zone"} == bool dnshealth_mx_resolved_count{zone="$zone"})), 1) or on() vector(0)`,
		"All MX targets resolve",
		"**Metric**: count comparison `dnshealth_mx_count` vs `dnshealth_mx_resolved_count`, with explicit pass-by-default for zones that have no MX targets to check (no MX records OR Null MX)  \n" +
			"**Why FAIL matters**: At least one MX target's hostname doesn't resolve to A or AAAA. Inbound mail attempts for that target will fail.  \n" +
			"**Investigate**: per-MX records table — `resolves=0` column identifies the offending target. Vacuously PASSes when the zone has no MX records (row A surfaces that case) or when the zone is Null MX (no targets to resolve by definition).",
	},
	{"C",
		`(dnshealth_mx_cname_count{zone="$zone"} == bool 0) or on() vector(0)`,
		"No MX target is a CNAME (RFC 2181 §10.3)",
		"**Metric**: `dnshealth_mx_cname_count == 0`  \n" +
			"**Why FAIL matters**: At least one MX target is an alias (CNAME), violating RFC 2181 §10.3. Many MTAs handle this inconsistently; some refuse delivery outright.  \n" +
			"**Investigate**: per-MX records table — `is_cname=1` column identifies the offending target.",
	},
	{"D",
		`clamp_max((dnshealth_mx_count{zone="$zone"} == bool 0) + on(zone) ((dnshealth_mx_count{zone="$zone"} > bool 0) * on(zone) (dnshealth_mx_count{zone="$zone"} == bool dnshealth_mx_syntax_valid_count{zone="$zone"})), 1) or on() vector(0)`,
		"All MX target hostnames syntactically valid (LDH)",
		"**Metric**: count comparison `dnshealth_mx_count` vs `dnshealth_mx_syntax_valid_count`, with vacuous PASS when no MX records exist for the zone (count==0)  \n" +
			"**Why FAIL matters**: At least one MX target hostname has invalid syntax (underscore, leading/trailing hyphen, etc.) per RFC 952/1123. Some strict resolvers may reject.  \n" +
			"**Investigate**: per-MX records table; the same LDH check is applied to NS hostnames in spec N6 — see that row for the validator details. Vacuously PASSes when the zone has no MX records — row A surfaces that case.",
	},
	{"E",
		`((dnshealth_mx_has_null_mx_rr{zone="$zone"} == bool 0) or on(zone) (dnshealth_mx_count{zone="$zone"} == bool 1)) or on() vector(0)`,
		"No conflict between Null MX and real MX records",
		"**Metric**: derived from `dnshealth_mx_has_null_mx_rr` + `dnshealth_mx_count`  \n" +
			"**Why FAIL matters**: Zone publishes a Null MX RR (`0 .`) AND additional MX records. RFC 7505 §3 requires Null MX to be the SOLE MX record; coexistence is undefined and likely interpreted differently by different MTAs.  \n" +
			"**Investigate**: auth's zone file — remove either the Null MX RR (if you want to accept email) or the real MX records (if you don't). The distinction between `dnshealth_mx_null_mx` (canonical form only) and `dnshealth_mx_has_null_mx_rr` (any Null MX RR present, regardless of count) is what makes this row catchable — see spec 008 audit D-5.",
	},
}

// mxStatusTable goes BELOW the existing records row, full-width
// (24 grid units), positioned right before the per-MX records table.
// Height 6 to comfortably fit 5 rows at CellHeight=Sm. The yOffset
// shift the caller passes is applied via subY so the panel slides
// up cleanly when the markdown info-panel header is absent (matches
// the existing trio's behavior).
func mxStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("MX — status", gridPos(0, subY(22, yOffset), 24, 6), mxStatusChecks)
}
