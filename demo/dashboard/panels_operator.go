package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// classicColor returns the standard palette-classic FieldColor builder
// used by every operator timeseries panel.
func classicColor() *dashboard.FieldColorBuilder {
	return dashboard.NewFieldColorBuilder().
		Mode(dashboard.FieldColorModeIdPaletteClassic)
}

// listLegend / tableLegend are the two legend shapes the operator
// panels use. v1 uses list+singleTooltip for cycle duration and
// table+multiTooltip+calcs for the other three.
func listLegend() *common.VizLegendOptionsBuilder {
	return common.NewVizLegendOptionsBuilder().
		DisplayMode(common.LegendDisplayModeList).
		Placement(common.LegendPlacementBottom).
		ShowLegend(true)
}

func tableLegend(calcs ...string) *common.VizLegendOptionsBuilder {
	return common.NewVizLegendOptionsBuilder().
		DisplayMode(common.LegendDisplayModeTable).
		Placement(common.LegendPlacementBottom).
		ShowLegend(true).
		Calcs(calcs)
}

func singleTooltip() *common.VizTooltipOptionsBuilder {
	return common.NewVizTooltipOptionsBuilder().
		Mode(common.TooltipDisplayModeSingle).
		Sort(common.SortOrderNone)
}

func multiTooltip(sort common.SortOrder) *common.VizTooltipOptionsBuilder {
	return common.NewVizTooltipOptionsBuilder().
		Mode(common.TooltipDisplayModeMulti).
		Sort(sort)
}

// probeCycleDurationTimeseries — left half of operator row, line 1.
// One series: cycle duration in seconds.
func probeCycleDurationTimeseries(yOffset uint32) *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Probe cycle duration").
		Description("Wall time of each probe cycle, from dnshealth_probe_cycle_duration_seconds.").
		GridPos(gridPos(0, subY(36, yOffset), 12, 8)).
		Datasource(prometheusDS).
		Unit("s").
		ColorScheme(classicColor()).
		Legend(listLegend()).
		Tooltip(singleTooltip()).
		WithTarget(prometheus.NewDataqueryBuilder().
			RefId("A").
			Expr("dnshealth_probe_cycle_duration_seconds").
			LegendFormat("cycle duration"))
}

// dnsQueryRateTimeseries — left half of operator row, line 2.
// Two series: per-server query rate (left axis), cache hit ratio
// (right axis, percentunit).
func dnsQueryRateTimeseries(yOffset uint32) *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Query rate and delegation cache hit ratio").
		Description("Left axis: per-server query rate. Cache ratio = hits / (hits + misses).").
		GridPos(gridPos(12, subY(36, yOffset), 12, 8)).
		Datasource(prometheusDS).
		ColorScheme(classicColor()).
		Legend(tableLegend("mean")).
		Tooltip(multiTooltip(common.SortOrderNone)).
		WithTarget(prometheus.NewDataqueryBuilder().
			RefId("A").
			Expr("rate(dnshealth_dns_queries_total[1m])").
			LegendFormat("queries/s — {{server}}")).
		WithTarget(prometheus.NewDataqueryBuilder().
			RefId("B").
			Expr("rate(dnshealth_delegation_cache_hits_total[5m]) / clamp_min((rate(dnshealth_delegation_cache_hits_total[5m]) + rate(dnshealth_delegation_cache_misses_total[5m])), 1e-9)").
			LegendFormat("cache hit ratio")).
		OverrideByName("cache hit ratio", []dashboard.DynamicConfigValue{
			{Id: "unit", Value: "percentunit"},
			{Id: "custom.axisPlacement", Value: "right"},
		})
}

// soaSerialsTimeseries — right half of operator row, line 1.
// One series per nameserver showing SOA serial value over time, with
// stepAfter line interpolation and visible points to make divergence
// pop visually.
func soaSerialsTimeseries(yOffset uint32) *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("SOA serials per nameserver over time — ${zone}").
		Description("Each line is one nameserver for the selected zone. Lines overlap when primaries agree; divergence appears as parallel non-overlapping lines.").
		GridPos(gridPos(0, subY(44, yOffset), 12, 8)).
		Datasource(prometheusDS).
		ColorScheme(classicColor()).
		LineInterpolation(common.LineInterpolationStepAfter).
		ShowPoints(common.VisibilityModeAlways).
		Legend(tableLegend("lastNotNull")).
		Tooltip(multiTooltip(common.SortOrderNone)).
		WithTarget(prometheus.NewDataqueryBuilder().
			RefId("A").
			Expr(`dnshealth_soa_serial{zone="$zone"}`).
			LegendFormat("{{nameserver}}"))
}

// queryDurationTimeseries — right half of operator row, line 2.
// One series per (check, nameserver) showing query duration.
func queryDurationTimeseries(yOffset uint32) *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Query duration (per check / nameserver) — ${zone}").
		Description("dnshealth_query_duration_seconds for the selected zone — useful for spotting slow nameservers or check types.").
		GridPos(gridPos(12, subY(44, yOffset), 12, 8)).
		Datasource(prometheusDS).
		Unit("s").
		ColorScheme(classicColor()).
		Legend(tableLegend("max")).
		Tooltip(multiTooltip(common.SortOrderDescending)).
		WithTarget(prometheus.NewDataqueryBuilder().
			RefId("A").
			Expr(`dnshealth_query_duration_seconds{zone="$zone"}`).
			LegendFormat("{{check}} via {{nameserver}}"))
}
