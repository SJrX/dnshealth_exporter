package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

// prometheusDS is the datasource ref used by every panel and by the
// $zone template variable. The uid is a Grafana datasource template
// variable reference (${prometheus}, declared by dsVariable below)
// rather than a hardcoded uid, so the dashboard works both in the
// demo (auto-resolves to the demo's provisioned datasource since
// there's only one Prometheus available) and when imported into any
// other Grafana (Grafana prompts the user to pick a Prometheus, or
// auto-selects if they only have one). Fixes #18.
//
// Note: the pinned SDK version exposes DataSourceRef under the
// dashboard package (not common); confirmed against the module cache
// during Phase 2 implementation.
var prometheusDS = dashboard.DataSourceRef{
	Type: cog.ToPtr("prometheus"),
	Uid:  cog.ToPtr("${prometheus}"),
}

// dsVariable returns the dashboard's datasource template variable.
// Conventionally named "prometheus" so panels reference it as
// `${prometheus}`. Filtered by Regex to only allow Prometheus
// datasources (the dashboard's panels only make sense against a
// Prometheus datasource).
//
// In the demo stack there's exactly one Prometheus, so Grafana
// auto-resolves on dashboard load with no UI prompt. When the
// dashboard is imported into a Grafana with multiple datasources,
// Grafana shows a picker on first load.
func dsVariable() *dashboard.DatasourceVariableBuilder {
	return dashboard.NewDatasourceVariableBuilder("prometheus").
		Label("Prometheus").
		Type("prometheus")
}

// zoneVariable returns the $zone template variable, query-driven from
// Prometheus. defaultZone is the initially-selected value: a non-empty
// zone (the demo variant) pins a known-good zone so the bundled stack
// opens on something meaningful; an empty defaultZone (the public
// variant) emits no `current`, so Grafana auto-selects the importing
// user's first real zone on load instead of a demo name they don't have.
//
// The pinned SDK version has no .Definition() builder method, so the
// emitted JSON omits the `definition` field present in v1. The
// `definition` field is a UI hint; the actual query lives in
// `query.query` (set below via StringOrMap). Functional behaviour
// preserved; gap documented in audit.md D3 (the drift test cannot
// catch this since both committed and generated JSON omit it).
func zoneVariable(defaultZone string) *dashboard.QueryVariableBuilder {
	const promQuery = "label_values(dnshealth_query_success, zone)"
	b := dashboard.NewQueryVariableBuilder("zone").
		Label("Zone").
		Datasource(prometheusDS).
		Query(dashboard.StringOrMap{Map: map[string]any{
			"qryType": 1,
			"query":   promQuery,
			"refId":   "PrometheusVariableQueryEditor-VariableQuery",
		}}).
		Refresh(dashboard.VariableRefreshOnDashboardLoad).
		Sort(dashboard.VariableSortAlphabeticalAsc)
	if defaultZone != "" {
		b = b.Current(dashboard.VariableOption{
			Selected: cog.ToPtr(true),
			Text:     dashboard.StringOrArrayOfString{String: cog.ToPtr(defaultZone)},
			Value:    dashboard.StringOrArrayOfString{String: cog.ToPtr(defaultZone)},
		})
	}
	return b
}
