package main

import (
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

// gridPos is a typo-friendly wrapper for the four-int GridPos literal.
func gridPos(x, y, w, h uint32) dashboard.GridPos {
	return dashboard.GridPos{X: x, Y: y, W: w, H: h}
}

// subY subtracts off from base for a panel's Y coord, panicking on
// uint32 underflow rather than wrapping silently. Used by every panel
// function to compute its Y position as `baseY - yOffset` (yOffset is
// the layout-reflow shift applied when the info panel is removed).
// Panic is the right failure mode here: silent wrap would produce a
// dashboard with Y in the billions, which Grafana would render as
// "off-screen" — invisible breakage.
func subY(base, off uint32) uint32 {
	if off > base {
		panic(fmt.Sprintf("subY underflow: base=%d off=%d — a base Y must be >= every yOffset value passed by buildOverview", base, off))
	}
	return base - off
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

// emptyGlueMapping: empty-string → "(not provided)" gray mapping used
// by the "Glue IP" column of the NS records (from parent) table.
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
