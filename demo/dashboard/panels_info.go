package main

import (
	"github.com/grafana/grafana-foundation-sdk/go/text"
)

// infoTextPanel reproduces the markdown header panel from the v1
// dashboard. Included only in the "full" variant; the "clean" variant
// omits it (FR-008) and reflows the layout (see dashboard.go's
// compactGridY helper).
func infoTextPanel() *text.PanelBuilder {
	const content = "Per-zone health snapshot inspired by intodns.com. " +
		"Use the **Zone** selector at the top to switch zones.\n\n" +
		"**Top row** is boolean status checks — each row's Result renders `1` as **PASS** (green) and `0` as **FAIL** (red). " +
		"**Middle row** is informational — the actual records / values returned by the queries, with no PASS/FAIL coloring. " +
		"**Operator / debug views** at the bottom (collapsed) include `dnshealth_soa_serial` over time, probe cycle duration, " +
		"query rate, cache hit ratio, and per-zone query duration.\n\n" +
		"**Demo zones in this stack:** `healthy.demo.` (all checks pass), " +
		"`soa-serial-mismatch.demo.` (SOA serial divergence between primaries), " +
		"`lame-nameserver.demo.` (auth NS doesn't actually serve the zone), " +
		"`ns-mismatch.demo.` (parent advertises 1 NS but auth reports 2 different NSs), " +
		"`missing-glue.demo.` (parent NS without glue, delegation walk fails entirely — won't appear in the selector since it produces no `dnshealth_query_success` series; the absence is the signal)."

	return text.NewPanelBuilder().
		Title("DNS health report — ${zone}").
		GridPos(gridPos(0, 0, 24, 4)).
		Mode(text.TextModeMarkdown).
		Content(content)
}
