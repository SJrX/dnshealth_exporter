package main

// Typed wrappers around the four Grafana table transformations used in the
// v1 dashboard. The SDK's dashboard.DataTransformerConfig.Options field is
// `any`, with no per-transformation typed builder upstream — see
// specs/005-dashboard-go-sdk/research.md R-3. These helpers give us
// compile-time checks on the option field names so a renamed/typoed
// field is a build error rather than a Grafana-load-time error.

import (
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

const (
	TransformJoinByField   = "joinByField"
	TransformOrganize      = "organize"
	TransformFilterByValue = "filterByValue"
	TransformReduce        = "reduce"
)

// JoinByFieldOptions matches the joinByField transformation options
// emitted by Grafana 11.x. byField names the column to join on; mode is
// "outer" or "inner".
type JoinByFieldOptions struct {
	ByField string `json:"byField"`
	Mode    string `json:"mode"`
}

func JoinByField(opts JoinByFieldOptions) dashboard.DataTransformerConfig {
	return dashboard.DataTransformerConfig{
		Id:      TransformJoinByField,
		Options: opts,
	}
}

// OrganizeOptions matches the organize transformation. renameByName maps
// original field name → display name; indexByName maps original field
// name → ordinal column position; excludeByName hides columns when true.
type OrganizeOptions struct {
	RenameByName  map[string]string `json:"renameByName"`
	IndexByName   map[string]int    `json:"indexByName"`
	ExcludeByName map[string]bool   `json:"excludeByName,omitempty"`
}

func Organize(opts OrganizeOptions) dashboard.DataTransformerConfig {
	return dashboard.DataTransformerConfig{
		Id:      TransformOrganize,
		Options: opts,
	}
}

// FilterByValueFilter matches one entry in the filterByValue.filters list.
// The Config.Options sub-map varies per matcher id; we type only the outer
// shape and leave the inner options as map[string]any to avoid a full
// matcher-id taxonomy.
type FilterByValueFilter struct {
	FieldName string                  `json:"fieldName"`
	Config    FilterByValueMatcherCfg `json:"config"`
}

type FilterByValueMatcherCfg struct {
	Id      string         `json:"id"`
	Options map[string]any `json:"options"`
}

type FilterByValueOptions struct {
	Filters []FilterByValueFilter `json:"filters"`
	Type    string                `json:"type"`
	Match   string                `json:"match"`
}

func FilterByValue(opts FilterByValueOptions) dashboard.DataTransformerConfig {
	return dashboard.DataTransformerConfig{
		Id:      TransformFilterByValue,
		Options: opts,
	}
}

// ReduceOptions matches the reduce transformation. Reducers names the list
// of reducer functions ("last", "max", "mean", ...). Mode is typically
// "seriesToRows" for our table panels.
type ReduceOptions struct {
	Reducers []string `json:"reducers"`
	Mode     string   `json:"mode"`
}

func Reduce(opts ReduceOptions) dashboard.DataTransformerConfig {
	return dashboard.DataTransformerConfig{
		Id:      TransformReduce,
		Options: opts,
	}
}
