package main

import (
	"encoding/json"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

// infoPanelHeight is the H of the markdown info panel (see infoTextPanel).
// When the info panel is omitted (clean variant), every panel below it
// shifts up by this many grid rows so the dashboard has no empty band
// at the top. See FR-008 (b).
const infoPanelHeight uint32 = 4

// buildOverview is the single shared builder used to emit BOTH dashboard
// variants (default + demo). The only per-variant branches are:
//
//   - whether the markdown header panel is included (demo only)
//   - the yOffset passed to every other panel function (infoPanelHeight
//     for the default variant to compact upward, 0 for the demo
//     variant since the header occupies the top rows)
//
// Every panel function below is called exactly once. There are no
// parallel panel chains for default vs demo — adding a panel touches
// one place. See specs/005-dashboard-go-sdk/research.md R-5 and FR-008.
func buildOverview(uid, title string, includeInfoText bool) (dashboard.Dashboard, error) {
	var yOffset uint32
	if !includeInfoText {
		yOffset = infoPanelHeight
	}

	b := dashboard.NewDashboardBuilder(title).
		Uid(uid).
		Tags([]string{"dnshealth"}).
		Refresh("10s").
		Time("now-15m", "now").
		WithVariable(dsVariable()).
		WithVariable(zoneVariable())

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

	// Operator row — collapsed by default; contains four timeseries.
	b = b.WithRow(dashboard.NewRowBuilder("Operator / debug views").
		Collapsed(true).
		GridPos(dashboard.GridPos{X: 0, Y: subY(22, yOffset), W: 24, H: 1}).
		WithPanel(probeCycleDurationTimeseries(yOffset)).
		WithPanel(dnsQueryRateTimeseries(yOffset)).
		WithPanel(soaSerialsTimeseries(yOffset)).
		WithPanel(queryDurationTimeseries(yOffset)))

	return b.Build()
}

// marshalDashboard is the canonical serialisation path. main.go and the
// drift test (T014) MUST go through this function so that any future
// change to indent or trailing-newline handling stays in one place.
func marshalDashboard(d dashboard.Dashboard) ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
