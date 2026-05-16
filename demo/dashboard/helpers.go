package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

// gridPos is a typo-friendly wrapper for the four-int GridPos literal.
func gridPos(x, y, w, h uint32) dashboard.GridPos {
	return dashboard.GridPos{X: x, Y: y, W: w, H: h}
}

// passFailMappings is the standard 0/1 → FAIL/PASS color mapping used by
// every "status" table in the v1 dashboard. Wrapped in a single-element
// list because that's the shape Grafana's `mappings` field expects.
func passFailMappings() []any {
	return []any{
		map[string]any{
			"type": "value",
			"options": map[string]any{
				"0": map[string]any{"text": "FAIL", "color": "red", "index": 0},
				"1": map[string]any{"text": "PASS", "color": "green", "index": 1},
			},
		},
	}
}

// respondedYesNoMappings: 0/1 → no/yes (red/green) for the "Responded"
// column of the NS records (from the zone) table.
func respondedYesNoMappings() []any {
	return []any{
		map[string]any{
			"type": "value",
			"options": map[string]any{
				"0": map[string]any{"text": "no", "color": "red", "index": 0},
				"1": map[string]any{"text": "yes", "color": "green", "index": 1},
			},
		},
	}
}

// recursionYesNoMappings: 0/1 → no/RA=1 (green/red — green when RA is
// off, red when RA=1) for the "Recursion" column of the NS records
// (from the zone) table.
func recursionYesNoMappings() []any {
	return []any{
		map[string]any{
			"type": "value",
			"options": map[string]any{
				"0": map[string]any{"text": "no", "color": "green", "index": 0},
				"1": map[string]any{"text": "RA=1", "color": "red", "index": 1},
			},
		},
	}
}

// cellOptionsColorBackground is the {type:"color-background", mode:"basic"}
// custom.cellOptions value used by every PASS/FAIL/Responded/Recursion
// column to paint the cell with the mapped color.
func cellOptionsColorBackground() any {
	return map[string]any{
		"type": "color-background",
		"mode": "basic",
	}
}

// tableDefaultsAutoAlign is the standard custom.{align,filterable}
// fieldConfig.defaults block used by every table panel in v1.
// We attach via OverrideByName / fieldConfig isn't directly exposed by
// the SDK panel builder, so we inject this via the panel's defaults
// when needed.
//
// In practice the SDK's CellHeight and ShowHeader cover the panel-
// level options; the per-column "auto align / non-filterable" defaults
// in v1 land in the panel's fieldConfig.defaults.custom map which the
// SDK does not expose a typed setter for. They will appear in the
// regenerated JSON only if Grafana auto-fills them. Acceptable
// cosmetic drift — table behavior is unaffected.

// emptyMapping: empty-string → "(not provided)" gray mapping used by
// the "Glue IP" column of the NS records (from parent) table.
func emptyGlueMapping() []any {
	return []any{
		map[string]any{
			"type": "value",
			"options": map[string]any{
				"": map[string]any{"text": "(not provided)", "color": "text", "index": 0},
			},
		},
	}
}
