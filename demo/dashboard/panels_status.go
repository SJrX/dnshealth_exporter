package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/table"
)

// statusTable is the shared builder for the three "X — status" tables
// (Parent, NS, SOA). Each table runs one or more instant boolean
// PromQL targets, reduces seriesToRows (one row per legendFormat), and
// renames the resulting two columns to Test / Result. The Result column
// gets the standard PASS/FAIL color background. Test-column width
// differs per table (v1 uses 260 / 280 / 260).
//
// Each target supplies its `legendFormat` (the test name shown in
// the Test column) and its `expr` (the PromQL boolean check).
type statusCheck struct {
	refId, expr, legendFormat string
}

func statusTable(title string, x, yOffset uint32, checks []statusCheck) *table.PanelBuilder {
	b := table.NewPanelBuilder().
		Title(title).
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

func parentStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("Parent — status", 0, yOffset, []statusCheck{
		{"A",
			`(count by (zone) (dnshealth_ns_record{source="parent",zone="$zone"}) > bool 0) or on() vector(0)`,
			"Parent has NS records for the zone"},
	})
}

func nsStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("NS — status", 8, yOffset, []statusCheck{
		{"A",
			`(count by (zone) (count by (zone, nameserver) (dnshealth_query_success{check="soa",zone="$zone"})) >= bool 2) or on() vector(0)`,
			"Multiple authoritative nameservers (>=2)"},
		{"B",
			`min by (zone) (dnshealth_query_success{check="soa",zone="$zone"}) or on() vector(0)`,
			"All NSs answered SOA authoritatively"},
		{"C",
			`(max by (zone) (dnshealth_ns_recursion_available{zone="$zone"}) == bool 0) or on() vector(0)`,
			"No NS advertises recursion (RA=0)"},
		{"D",
			`((count by (zone) (group by (zone, nameserver) (dnshealth_ns_record{source="parent",zone="$zone"}))) == bool (count by (zone) (group by (zone, nameserver) (dnshealth_ns_record{source="self",zone="$zone"})))) and on(zone) (count by (zone) (dnshealth_ns_record{source="self",zone="$zone"}) > bool 0) or on() vector(0)`,
			"Parent and self report same NS records"},
	})
}

func soaStatusTable(yOffset uint32) *table.PanelBuilder {
	return statusTable("SOA — status", 16, yOffset, []statusCheck{
		{"A",
			`((max by (zone) (dnshealth_soa_serial{zone="$zone"}) - min by (zone) (dnshealth_soa_serial{zone="$zone"})) == bool 0) and on(zone) (count by (zone) (dnshealth_soa_serial{zone="$zone"}) > bool 0) or on() vector(0)`,
			"All NSs report same SOA serial"},
	})
}
