package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/table"
)

// parentNSRecordsTable shows what the parent referral lists as the
// zone's NSs, with the glue IP. One row per NS.
func parentNSRecordsTable(yOffset uint32) *table.PanelBuilder {
	return table.NewPanelBuilder().
		Title("NS records — from parent").
		Description(`What the parent referral lists as the zone's NSs. Glue IP empty = parent did not include A glue (exporter resolved separately).`).
		GridPos(gridPos(0, subY(14, yOffset), 8, 10)).
		Datasource(prometheusDS).
		ShowHeader(true).
		CellHeight(common.TableCellHeightSm).
		WithTarget(prometheus.NewDataqueryBuilder().
			RefId("A").
			Expr(`dnshealth_ns_record{source="parent",zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).
			Instant()).
		WithTransformation(Organize(OrganizeOptions{
			RenameByName: map[string]string{
				"nameserver": "Nameserver",
				"ip":         "Glue IP",
			},
			IndexByName: map[string]int{
				"nameserver": 0,
				"ip":         1,
			},
			ExcludeByName: map[string]bool{
				"Time":     true,
				"source":   true,
				"zone":     true,
				"Value":    true,
				"__name__": true,
				"instance": true,
				"job":      true,
			},
		})).
		OverrideByName("Glue IP", []dashboard.DynamicConfigValue{
			{Id: "mappings", Value: emptyGlueMapping()},
		}).
		SortBy(sortByAsc("Nameserver"))
}

// selfNSRecordsTable joins three queries (self NS records, SOA query
// success, recursion available) by nameserver and presents one row per
// auth-reported NS, with Responded/Recursion status columns.
//
// The filter keeps only rows where Value #A (self NS record presence)
// is non-null — drops nameservers seen only via SOA/recursion but not
// in the auth's own NS RR set.
func selfNSRecordsTable(yOffset uint32) *table.PanelBuilder {
	return table.NewPanelBuilder().
		Title("NS records — from the zone").
		Description(`What the auth servers themselves report as the zone's NSs (source="self" rows from the glue prober), joined with each NS's probe response (Responded = SOA query succeeded, Recursion = RA flag set). Empty Responded/Recursion cells mean the exporter has no probe data for that NS — happens when the auth's self-reported NS list does not match what the parent advertised, since SOA/recursion checks only run against parent-listed NSs.`).
		GridPos(gridPos(8, subY(14, yOffset), 8, 10)).
		Datasource(prometheusDS).
		ShowHeader(true).
		CellHeight(common.TableCellHeightSm).
		WithTarget(prometheus.NewDataqueryBuilder().
			RefId("A").
			Expr(`dnshealth_ns_record{source="self",zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).
			Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().
			RefId("B").
			Expr(`dnshealth_query_success{check="soa",zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).
			Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().
			RefId("C").
			Expr(`dnshealth_ns_recursion_available{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).
			Instant()).
		WithTransformation(JoinByField(JoinByFieldOptions{
			ByField: "nameserver",
			Mode:    "outer",
		})).
		WithTransformation(FilterByValue(FilterByValueOptions{
			Filters: []FilterByValueFilter{{
				FieldName: "Value #A",
				Config: FilterByValueMatcherCfg{
					Id:      "isNotNull",
					Options: map[string]any{},
				},
			}},
			Type:  "include",
			Match: "all",
		})).
		WithTransformation(Organize(OrganizeOptions{
			RenameByName: map[string]string{
				"nameserver": "Nameserver",
				"ip 1":       "IP",
				"Value #B":   "Responded",
				"Value #C":   "Recursion",
			},
			IndexByName: map[string]int{
				"nameserver": 0,
				"ip 1":       1,
				"Value #B":   2,
				"Value #C":   3,
			},
			ExcludeByName: map[string]bool{
				"Time 1": true, "Time 2": true, "Time 3": true,
				"source 1":   true,
				"check":      true,
				"zone 1":     true, "zone 2": true, "zone 3": true,
				"ip 2": true, "ip 3": true,
				"instance 1": true, "instance 2": true, "instance 3": true,
				"job 1": true, "job 2": true, "job 3": true,
				"__name__ 1": true, "__name__ 2": true, "__name__ 3": true,
				"Value #A": true,
			},
		})).
		OverrideByName("Responded", []dashboard.DynamicConfigValue{
			{Id: "mappings", Value: respondedYesNoMappings()},
			{Id: "custom.cellOptions", Value: cellOptionsColorBackground()},
			// Narrow: values are short ("yes"/"no") and the colour
			// background makes the cell easy to scan. Wider than Result
			// (80) because "Responded" is a slightly longer label.
			{Id: "custom.width", Value: 100},
		}).
		OverrideByName("Recursion", []dashboard.DynamicConfigValue{
			{Id: "mappings", Value: recursionYesNoMappings()},
			{Id: "custom.cellOptions", Value: cellOptionsColorBackground()},
			{Id: "custom.width", Value: 100},
		}).
		// Widen IP — needs to fit canonical IPv6 form (up to ~25 chars
		// for typical addresses; the address-override pattern in spec
		// 006 makes v6 entries common on this panel).
		OverrideByName("IP", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 240},
		}).
		SortBy(sortByAsc("Nameserver"))
}

// soaSerialsTable shows one row per nameserver with SOA serial,
// refresh, retry, expire, and minimum TTL values, joined by nameserver.
func soaSerialsTable(yOffset uint32) *table.PanelBuilder {
	return table.NewPanelBuilder().
		Title("SOA — per-nameserver values").
		Description("One row per nameserver. Empty/missing rows mean the SOA query failed for that NS.").
		GridPos(gridPos(16, subY(14, yOffset), 8, 10)).
		Datasource(prometheusDS).
		ShowHeader(true).
		CellHeight(common.TableCellHeightSm).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("A").
			Expr(`dnshealth_soa_serial{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("B").
			Expr(`dnshealth_soa_refresh_seconds{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("C").
			Expr(`dnshealth_soa_retry_seconds{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("D").
			Expr(`dnshealth_soa_expire_seconds{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("E").
			Expr(`dnshealth_soa_minimum_seconds{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTransformation(JoinByField(JoinByFieldOptions{
			ByField: "nameserver",
			Mode:    "outer",
		})).
		WithTransformation(Organize(OrganizeOptions{
			RenameByName: map[string]string{
				"nameserver": "Nameserver",
				"ip 1":       "IP",
				"Value #A":   "Serial",
				"Value #B":   "Refresh (s)",
				"Value #C":   "Retry (s)",
				"Value #D":   "Expire (s)",
				"Value #E":   "Min TTL (s)",
			},
			IndexByName: map[string]int{
				"nameserver": 0,
				"ip 1":       1,
				"Value #A":   2,
				"Value #B":   3,
				"Value #C":   4,
				"Value #D":   5,
				"Value #E":   6,
			},
			ExcludeByName: map[string]bool{
				"Time 1": true, "Time 2": true, "Time 3": true, "Time 4": true, "Time 5": true,
				"zone 1": true, "zone 2": true, "zone 3": true, "zone 4": true, "zone 5": true,
				"ip 2": true, "ip 3": true, "ip 4": true, "ip 5": true,
				"__name__ 1": true, "__name__ 2": true, "__name__ 3": true, "__name__ 4": true, "__name__ 5": true,
				"instance 1": true, "instance 2": true, "instance 3": true, "instance 4": true, "instance 5": true,
				"job 1": true, "job 2": true, "job 3": true, "job 4": true, "job 5": true,
			},
		})).
		// Widen IP — needs to fit canonical IPv6 form (up to ~25 chars
		// for typical addresses).
		OverrideByName("IP", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 240},
		}).
		// Narrow the SOA timer columns — values are small integers
		// (typically 30 to 86400 seconds) and the labels fit
		// comfortably in ~110px. Keeps Nameserver / IP / Serial wide
		// since those carry longer / more variable content.
		OverrideByName("Refresh (s)", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 110},
		}).
		OverrideByName("Retry (s)", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 100},
		}).
		OverrideByName("Expire (s)", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 110},
		}).
		OverrideByName("Min TTL (s)", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 110},
		}).
		SortBy(sortByAsc("Nameserver"))
}

// mxRecordsTable is the right half (w=12) of the MX section row,
// paired with mxStatusTable on the left at the same Y. Lists per-MX
// details: target, priority, resolves yes/no, is-CNAME yes/no,
// syntax-valid yes/no, role (primary/backup). Joined by `target`
// field across 5 queries.
//
// Note on Null MX zones: the `.` target appears with priority=0
// and `resolves`/`is_cname` cells empty (those metrics aren't
// emitted for the sentinel `.` target per spec 008 R-6 / data-model
// edge-case table). Detail text on the panel explains.
func mxRecordsTable(yOffset uint32) *table.PanelBuilder {
	return table.NewPanelBuilder().
		Title("MX records — per zone").
		Description(`Per-MX details for the selected zone: target hostname, priority, resolution status, CNAME status, syntax validity, role (primary = lowest-priority MX; ties at minimum priority all read "primary"). Empty cells in resolves/is-CNAME columns indicate Null MX's "." sentinel target — those checks intentionally don't apply per RFC 7505. SMTP-level reachability is out of scope; use blackbox_exporter with an SMTP prober for that.`).
		GridPos(gridPos(12, subY(25, yOffset), 12, 10)).
		Datasource(prometheusDS).
		ShowHeader(true).
		CellHeight(common.TableCellHeightSm).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("A").
			Expr(`dnshealth_mx_info{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("B").
			Expr(`dnshealth_mx_resolves{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("C").
			Expr(`dnshealth_mx_is_cname{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("D").
			Expr(`dnshealth_mx_syntax_valid{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTarget(prometheus.NewDataqueryBuilder().RefId("E").
			Expr(`dnshealth_mx_is_primary{zone="$zone"}`).
			Format(prometheus.PromQueryFormatTable).Instant()).
		WithTransformation(JoinByField(JoinByFieldOptions{
			ByField: "target",
			Mode:    "outer",
		})).
		WithTransformation(Organize(OrganizeOptions{
			RenameByName: map[string]string{
				"target":     "Target",
				"priority":   "Priority",
				"Value #B":   "Resolves",
				"Value #C":   "Is CNAME",
				"Value #D":   "Syntax valid",
				"Value #E":   "Role",
			},
			IndexByName: map[string]int{
				"target":     0,
				"priority":   1,
				"Value #B":   2,
				"Value #C":   3,
				"Value #D":   4,
				"Value #E":   5,
			},
			ExcludeByName: map[string]bool{
				"Time 1": true, "Time 2": true, "Time 3": true, "Time 4": true, "Time 5": true,
				"zone 1": true, "zone 2": true, "zone 3": true, "zone 4": true, "zone 5": true,
				"check 1": true, "check 2": true, "check 3": true, "check 4": true, "check 5": true,
				"ip 1": true, "ip 2": true, "ip 3": true, "ip 4": true, "ip 5": true,
				// nameserver labels are emitted as empty strings by the
				// MX prober (it's per-zone data, no NS fan-out). Excluded
				// to keep the table tight; the columns would otherwise
				// show up as 5 blank columns after the JoinByField.
				"nameserver 1": true, "nameserver 2": true, "nameserver 3": true, "nameserver 4": true, "nameserver 5": true,
				"__name__ 1": true, "__name__ 2": true, "__name__ 3": true, "__name__ 4": true, "__name__ 5": true,
				"instance 1": true, "instance 2": true, "instance 3": true, "instance 4": true, "instance 5": true,
				"job 1": true, "job 2": true, "job 3": true, "job 4": true, "job 5": true,
				"Value #A": true,
			},
		})).
		OverrideByName("Priority", []dashboard.DynamicConfigValue{
			{Id: "custom.width", Value: 80},
		}).
		OverrideByName("Resolves", []dashboard.DynamicConfigValue{
			{Id: "mappings", Value: respondedYesNoMappings()},
			{Id: "custom.cellOptions", Value: cellOptionsColorBackground()},
			{Id: "custom.width", Value: 100},
		}).
		OverrideByName("Is CNAME", []dashboard.DynamicConfigValue{
			// Inverted yes/no: 1 (yes = target is a CNAME, RFC 2181
			// §10.3 violation) renders RED; 0 (not a CNAME) GREEN.
			{Id: "mappings", Value: cnameYesNoMappings()},
			{Id: "custom.cellOptions", Value: cellOptionsColorBackground()},
			{Id: "custom.width", Value: 100},
		}).
		OverrideByName("Syntax valid", []dashboard.DynamicConfigValue{
			{Id: "mappings", Value: respondedYesNoMappings()},
			{Id: "custom.cellOptions", Value: cellOptionsColorBackground()},
			{Id: "custom.width", Value: 110},
		}).
		OverrideByName("Role", []dashboard.DynamicConfigValue{
			{Id: "mappings", Value: mxRoleMappings()},
			{Id: "custom.cellOptions", Value: cellOptionsColorBackground()},
			{Id: "custom.width", Value: 110},
		}).
		SortBy(sortByAsc("Priority"))
}
