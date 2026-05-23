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

func statusTable(title string, x, yOffset uint32, checks []statusCheck) *table.PanelBuilder {
	b := table.NewPanelBuilder().
		Title(title).
		Description(buildStatusPanelDescription(checks)).
		GridPos(gridPos(x, subY(4, yOffset), 8, 8)).
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
	return statusTable("Parent — status", 0, yOffset, parentStatusChecks)
}

func nsStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("NS — status", 8, yOffset, nsStatusChecks)
}

func soaStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("SOA — status", 16, yOffset, soaStatusChecks)
}
