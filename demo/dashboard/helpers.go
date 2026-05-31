package main

import (
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
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

// statusMappings is the canonical four-state color mapping for every
// "status" table Result cell (constitution Principle IX):
//
//	0 → FAIL  (red)
//	1 → PASS  (green)
//	2 → N/A   (gray)    — check does not apply to this zone
//	3 → WARN  (yellow)  — passes the hard check but a soft concern applies
//
// Rows that only ever emit 0/1 render identically to the old two-state
// mapping — the 2/3 entries are inert for them. The composition lives
// in composeStatusExpr (panels_status.go); this is purely the display
// side. "text" is Grafana's neutral gray, distinct from a real PASS/FAIL.
func statusMappings() []any {
	return []any{
		map[string]any{
			"type": "value",
			"options": map[string]any{
				"0": map[string]any{"text": "FAIL", "color": "red", "index": 0},
				"1": map[string]any{"text": "PASS", "color": "green", "index": 1},
				"2": map[string]any{"text": "N/A", "color": "text", "index": 2},
				"3": map[string]any{"text": "WARN", "color": "yellow", "index": 3},
			},
		},
	}
}

// nullNAMapping renders a missing cell (null — e.g. an outer-join gap
// where one query had no series for that row) as a gray "n/a" instead
// of letting it fall through to the base threshold colour, which paints
// a textless GREEN cell that reads as a spurious pass. Appended to the
// yes/no cell helpers below. Two cases hit this:
//   - NS records (from the zone): a self-reported NS that the parent
//     does not advertise has no SOA/recursion probe data, so its
//     Responded/Recursion cells are null (e.g. ns-mismatch.demo.).
//   - per-MX records: a Null MX "." sentinel emits no resolves/is-cname
//     metric, so those cells are null.
// "index": 2 keeps it after the 0/1 value entries; matching is by the
// special "null" match, not index.
func nullNAMapping() map[string]any {
	return map[string]any{
		"type": "special",
		"options": map[string]any{
			"match": "null",
			"result": map[string]any{"text": "n/a", "color": "text", "index": 2},
		},
	}
}

// respondedYesNoMappings: 0/1 → no/yes (red/green) for the "Responded"
// column of the NS records (from the zone) table; null → gray "n/a".
func respondedYesNoMappings() []any {
	return []any{
		map[string]any{
			"type": "value",
			"options": map[string]any{
				"0": map[string]any{"text": "no", "color": "red", "index": 0},
				"1": map[string]any{"text": "yes", "color": "green", "index": 1},
			},
		},
		nullNAMapping(),
	}
}

// recursionYesNoMappings: 0/1 → no/RA=1 (green/red — green when RA is
// off, red when RA=1) for the "Recursion" column of the NS records
// (from the zone) table. The "RA=1" text is deliberately NS-table
// jargon (Recursion Available flag); reusing this helper on other
// columns leaks that text inappropriately — use cnameYesNoMappings
// or a sibling helper for non-NS contexts.
func recursionYesNoMappings() []any {
	return []any{
		map[string]any{
			"type": "value",
			"options": map[string]any{
				"0": map[string]any{"text": "no", "color": "green", "index": 0},
				"1": map[string]any{"text": "RA=1", "color": "red", "index": 1},
			},
		},
		nullNAMapping(),
	}
}

// cnameYesNoMappings: 0/1 → no/yes (green/red — green when target is
// not a CNAME, red when it is) for the "Is CNAME" column of the per-MX
// records table. Same inverted polarity as recursionYesNoMappings but
// with neutral "yes"/"no" text — RFC 2181 §10.3 makes a CNAMEd MX
// target a config error, hence red on the truthy side.
func cnameYesNoMappings() []any {
	return []any{
		map[string]any{
			"type": "value",
			"options": map[string]any{
				"0": map[string]any{"text": "no", "color": "green", "index": 0},
				"1": map[string]any{"text": "yes", "color": "red", "index": 1},
			},
		},
		nullNAMapping(),
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

// sortByAsc returns a single-entry sortBy spec suitable for
// table.PanelBuilder.SortBy(). Sorts ascending by the named column
// (DisplayName matches the post-rename header text, not the raw
// metric label).
func sortByAsc(displayName string) []cog.Builder[common.TableSortByFieldState] {
	return []cog.Builder[common.TableSortByFieldState]{
		common.NewTableSortByFieldStateBuilder().DisplayName(displayName),
	}
}

// mxRoleMappings: 0/1 → backup/primary mapping for the per-MX
// records table's Role column (spec 008). Primary (1) is green
// (the lowest-priority MX is the desired delivery target); backup
// (0) is text-colored (neutral, neither warning nor error — backups
// are normal and expected).
func mxRoleMappings() []any {
	return []any{
		map[string]any{
			"type": "value",
			"options": map[string]any{
				"0": map[string]any{"text": "backup", "color": "text", "index": 0},
				"1": map[string]any{"text": "primary", "color": "green", "index": 1},
			},
		},
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
