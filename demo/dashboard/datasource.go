package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

// prometheusDS is the datasource ref used by every panel and by the
// $zone template variable. The uid matches
// demo/grafana/provisioning/datasources/datasources.yml.
//
// Note: the pinned SDK version exposes DataSourceRef under the
// dashboard package (not common); confirmed against the module cache
// during Phase 2 implementation.
var prometheusDS = dashboard.DataSourceRef{
	Type: cog.ToPtr("prometheus"),
	Uid:  cog.ToPtr("dnshealth-prometheus"),
}

// zoneVariable returns the $zone template variable, query-driven from
// Prometheus, defaulting to healthy.demo.. Reproduces the v1 dashboard's
// templating.list[0] entry.
//
// The pinned SDK version has no .Definition() builder method, so the
// emitted JSON omits the `definition` field present in v1. The
// `definition` field is a UI hint; the actual query lives in
// `query.query` (set below via StringOrMap). Functional behaviour
// preserved; gap documented in audit.md D3 (the drift test cannot
// catch this since both committed and generated JSON omit it).
func zoneVariable() *dashboard.QueryVariableBuilder {
	const promQuery = "label_values(dnshealth_query_success, zone)"
	return dashboard.NewQueryVariableBuilder("zone").
		Label("Zone").
		Datasource(prometheusDS).
		Query(dashboard.StringOrMap{Map: map[string]any{
			"qryType": 1,
			"query":   promQuery,
			"refId":   "PrometheusVariableQueryEditor-VariableQuery",
		}}).
		Refresh(dashboard.VariableRefreshOnDashboardLoad).
		Sort(dashboard.VariableSortAlphabeticalAsc).
		Current(dashboard.VariableOption{
			Selected: cog.ToPtr(true),
			Text:     dashboard.StringOrArrayOfString{String: cog.ToPtr("healthy.demo.")},
			Value:    dashboard.StringOrArrayOfString{String: cog.ToPtr("healthy.demo.")},
		})
}
