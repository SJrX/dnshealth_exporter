package main

import (
	"encoding/json"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

// infoPanelHeight is the H of the markdown info panel (see infoTextPanel).
// When the info panel is omitted (clean variant), every panel below it
// shifts up by this many grid rows so the dashboard has no empty band
// at the top. See FR-008 (b).
const infoPanelHeight uint32 = 4

// dashboardVersion is the top-level Grafana `version` for both emitted
// variants. grafana.com's dashboard catalog requires each uploaded
// revision to carry a version strictly greater than the last published
// one (it need not be exactly +1), so this is bumped BY HAND when a new
// revision is published to grafana.com — see demo/README.md "Publishing
// to grafana.com". Publishing is rare and manual, so there is no
// auto-increment machinery (issue #16): the value must be a committed
// literal anyway, or the dashboard drift test (which byte-compares the
// committed JSON against fresh generator output) could never stay green.
const dashboardVersion uint32 = 1

// buildOverview is the single shared builder used to emit BOTH dashboard
// variants (default + demo). The only per-variant branches are:
//
//   - whether the markdown header panel is included (demo only)
//   - the yOffset passed to every other panel function (infoPanelHeight
//     for the default variant to compact upward, 0 for the demo
//     variant since the header occupies the top rows)
//
// defaultZone is the $zone the dashboard opens on. The demo variant pins
// a known-good demo zone so the bundled stack lands on something
// meaningful; the public variant passes "" so Grafana auto-selects the
// importing user's first real zone instead of a demo name they don't have.
//
// Every panel function below is called exactly once. There are no
// parallel panel chains for default vs demo — adding a panel touches
// one place. See specs/005-dashboard-go-sdk/research.md R-5 and FR-008.
func buildOverview(uid, title string, includeInfoText bool, defaultZone string) (dashboard.Dashboard, error) {
	var yOffset uint32
	if !includeInfoText {
		yOffset = infoPanelHeight
	}

	b := dashboard.NewDashboardBuilder(title).
		Uid(uid).
		Version(dashboardVersion).
		Tags([]string{"dnshealth"}).
		Refresh("10s").
		Time("now-15m", "now").
		Annotations(builtinAnnotations()).
		WithVariable(dsVariable()).
		WithVariable(zoneVariable(defaultZone))

	if includeInfoText {
		b = b.WithPanel(infoTextPanel())
	}

	// Status row — three tables side-by-side.
	b = b.
		WithPanel(parentStatusTable(yOffset)).
		WithPanel(nsStatusTable(yOffset)).
		WithPanel(soaStatusTable(yOffset))

	// Records row — three tables side-by-side.
	b = b.
		WithPanel(parentNSRecordsTable(yOffset)).
		WithPanel(selfNSRecordsTable(yOffset)).
		WithPanel(soaSerialsTable(yOffset))

	// Mail section (specs 008/009/010) — one collapsible row holding a
	// 2-per-row, 3-row grid of status + records panels: MX (Y=25), SPF
	// (Y=33), DMARC (Y=41). Header at Y=24. Folds the former separate MX
	// and Email-auth sections together so all mail-delivery health (the
	// records that govern whether a domain can send/receive mail and
	// resist spoofing) lives in one place. Expanded by default.
	b = b.WithRow(dashboard.NewRowBuilder("Mail — MX / SPF / DMARC").
		Collapsed(false).
		GridPos(dashboard.GridPos{X: 0, Y: subY(mailHeaderY, yOffset), W: 24, H: 1}).
		WithPanel(mxStatusTable(yOffset)).
		WithPanel(mxRecordsTable(yOffset)).
		WithPanel(spfStatusTable(yOffset)).
		WithPanel(spfRecordsTable(yOffset)).
		WithPanel(dmarcStatusTable(yOffset)).
		WithPanel(dmarcRecordsTable(yOffset)))

	// Operator row — collapsed by default; contains four timeseries.
	// Y=49: below the 3-row Mail grid (header 24 + rows at 25/33/41, each
	// h=8, ending at 49).
	b = b.WithRow(dashboard.NewRowBuilder("Operator / debug views").
		Collapsed(true).
		GridPos(dashboard.GridPos{X: 0, Y: subY(mailRowY(3), yOffset), W: 24, H: 1}).
		WithPanel(probeCycleDurationTimeseries(yOffset)).
		WithPanel(dnsQueryRateTimeseries(yOffset)).
		WithPanel(soaSerialsTimeseries(yOffset)).
		WithPanel(queryDurationTimeseries(yOffset)))

	return b.Build()
}

// builtinAnnotations returns the single default "Annotations & Alerts"
// annotation that Grafana itself injects into every dashboard on save.
// The SDK's model builder does NOT add it (it only emits `annotations`
// when the list is non-empty), so a generated dashboard serialises
// `annotations: {}` — which reads as the pre-`annotations.list` 2.x/3.0
// shape and makes grafana.com's catalog reject the upload as "Old
// Dashboard JSON Format". Emitting the builtin entry restores the modern
// `annotations.list` form. Values mirror Grafana's own default.
func builtinAnnotations() []cog.Builder[dashboard.AnnotationQuery] {
	return []cog.Builder[dashboard.AnnotationQuery]{
		dashboard.NewAnnotationQueryBuilder().
			Name("Annotations & Alerts").
			Datasource(common.DataSourceRef{
				Type: cog.ToPtr("grafana"),
				Uid:  cog.ToPtr("-- Grafana --"),
			}).
			Enable(true).
			Hide(true).
			IconColor("rgba(0, 211, 255, 1)").
			Type("dashboard").
			BuiltIn(1),
	}
}

// dashboardRequires is the `__requires` manifest: the plugins + Grafana
// version a consumer needs to render this dashboard. It is NOT part of
// the dashboard model — Grafana adds it during "export for sharing
// externally", and grafana.com's catalog keys on its presence. The
// `version` for each panel plugin is intentionally "" (no minimum); the
// Grafana floor is set deliberately low so the catalog doesn't gate
// importers on a newer release than the dashboard actually needs.
var dashboardRequires = []map[string]string{
	{"type": "grafana", "id": "grafana", "name": "Grafana", "version": "11.0.0"},
	{"type": "datasource", "id": "prometheus", "name": "Prometheus", "version": "1.0.0"},
	{"type": "panel", "id": "table", "name": "Table", "version": ""},
	{"type": "panel", "id": "timeseries", "name": "Time series", "version": ""},
}

// marshalDashboard is the canonical serialisation path. main.go and the
// drift test (T014) MUST go through this function so that any future
// change to indent or trailing-newline handling stays in one place.
//
// It injects `__requires` (see dashboardRequires) as the leading
// top-level key via struct embedding rather than a map round-trip:
// dashboard.Dashboard has no custom MarshalJSON, so its fields promote
// inline after `__requires`, preserving the model's field order exactly.
func marshalDashboard(d dashboard.Dashboard) ([]byte, error) {
	catalog := struct {
		Requires []map[string]string `json:"__requires"`
		dashboard.Dashboard
	}{
		Requires:  dashboardRequires,
		Dashboard: d,
	}
	b, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
