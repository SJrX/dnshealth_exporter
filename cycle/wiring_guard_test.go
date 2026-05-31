package cycle

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sjr/dnshealth_exporter/cache"
)

// TestRunnerMetricsAreWiredToDashboard enforces constitution Principle IX
// ("every new dnshealth_* metric ships with Grafana wiring, or an explicit,
// documented decision that it is operator-query-only"). It turns that rule
// — previously only a memory note / review habit — into a CI failure.
//
// How: it captures every collector NewRunner registers (via a recording
// Registerer), extracts each metric's fqName from its Desc, and asserts the
// name appears in at least one committed dashboard JSON OR is listed in
// operatorOnlyMetrics below with a rationale. A new runner-owned metric that
// is neither surfaced nor explicitly excused fails this test.
//
// Scope: this guards RUNNER-OWNED metrics (the GaugeVec/CounterVec values
// NewRunner registers). Prober-emitted metrics (dnshealth_mx_info,
// _resolves, _is_cname, _syntax_valid, dnshealth_ns_record, etc.) flow
// through the per-cycle registry built by prober.BuildRegistry, not through
// NewRunner, so they are out of this test's reach — they're covered by the
// drift test + the live PromQL smoke check (issue #46) instead.
//
// Runs in the default build: it only constructs a Runner in-process and
// reads committed files — no DNS, network, or Docker.
func TestRunnerMetricsAreWiredToDashboard(t *testing.T) {
	// Fixture Setup — construct a Runner against a recording registerer so
	// we capture exactly the collectors it owns, even Vecs with no children
	// (which a plain registry.Gather() would omit).
	rec := &recordingRegisterer{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c := cache.NewDelegationCache(time.Minute)
	_ = NewRunner(c, logger, rec)

	runnerMetrics := rec.metricNames(t)
	if len(runnerMetrics) == 0 {
		t.Fatal("captured zero runner metrics — recordingRegisterer or NewRunner changed shape")
	}

	// Load both committed dashboards once.
	repoRoot := repoRootFromCycleTest(t)
	dashboardText := readDashboards(t, repoRoot)

	// operatorOnlyMetrics are runner-owned metrics intentionally NOT placed
	// on a status row or records table. Each MUST carry a rationale; the
	// stale-entry check below fails if one of these stops being a real
	// runner metric, so the allowlist can't rot.
	operatorOnlyMetrics := map[string]string{
		"dnshealth_probe_zones_total": "operational counter (zones probed per cycle); surfaced via ad-hoc queries, not a per-zone dashboard row",
		"dnshealth_dns_timeouts_total": "operational counter; the operator query-rate panel charts dnshealth_dns_queries_total, timeouts are a query-only drill-down",
		// The operator panel charts dnshealth_query_duration_seconds (the
		// prober-pipeline timing). The runner's own per-server
		// dnshealth_dns_query_duration_seconds_total sum is a lower-level
		// counter, intentionally query-only to avoid a near-duplicate panel.
		"dnshealth_dns_query_duration_seconds_total": "low-level per-server timing sum; operator panel uses dnshealth_query_duration_seconds instead",
	}

	// Verification 1 — every runner metric is surfaced or explicitly excused.
	for _, name := range runnerMetrics {
		if strings.Contains(dashboardText, name) {
			continue
		}
		if _, excused := operatorOnlyMetrics[name]; excused {
			continue
		}
		t.Errorf("runner metric %q is not referenced in any dashboard JSON and is not in "+
			"operatorOnlyMetrics — per constitution Principle IX, either add it to a "+
			"dashboard panel (run `make dashboards`) or add it to operatorOnlyMetrics "+
			"with a rationale", name)
	}

	// Verification 2 — no stale allowlist entries. Every operatorOnlyMetrics
	// key must still be a real runner metric, else the rationale is lying.
	owned := make(map[string]bool, len(runnerMetrics))
	for _, n := range runnerMetrics {
		owned[n] = true
	}
	for name := range operatorOnlyMetrics {
		if !owned[name] {
			t.Errorf("operatorOnlyMetrics lists %q, but NewRunner no longer registers it — "+
				"remove the stale allowlist entry", name)
		}
	}
}

// recordingRegisterer captures every collector passed to it without
// registering anything, so we can introspect a Runner's metric set —
// including label-vector metrics that have no children yet (those are
// invisible to registry.Gather()).
type recordingRegisterer struct {
	collectors []prometheus.Collector
}

func (r *recordingRegisterer) Register(c prometheus.Collector) error {
	r.collectors = append(r.collectors, c)
	return nil
}

func (r *recordingRegisterer) MustRegister(cs ...prometheus.Collector) {
	r.collectors = append(r.collectors, cs...)
}

func (r *recordingRegisterer) Unregister(prometheus.Collector) bool { return false }

// fqNameRE pulls the fully-qualified metric name out of a Desc's String()
// form: `Desc{fqName: "dnshealth_x", help: "...", ...}`.
var fqNameRE = regexp.MustCompile(`fqName: "([^"]+)"`)

// metricNames extracts the fqName of every captured collector via its Desc.
func (r *recordingRegisterer) metricNames(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, c := range r.collectors {
		ch := make(chan *prometheus.Desc, 8)
		go func(col prometheus.Collector) {
			col.Describe(ch)
			close(ch)
		}(c)
		for d := range ch {
			m := fqNameRE.FindStringSubmatch(d.String())
			if m == nil {
				t.Fatalf("could not parse fqName from Desc: %s", d.String())
			}
			names = append(names, m[1])
		}
	}
	return names
}

func readDashboards(t *testing.T, repoRoot string) string {
	t.Helper()
	var b strings.Builder
	for _, p := range []string{
		"demo/grafana/dashboards/dnshealth-overview.json",
		"demo/grafana/dashboards/dnshealth-overview-demo.json",
	} {
		data, err := os.ReadFile(filepath.Join(repoRoot, p))
		if err != nil {
			t.Fatalf("read dashboard %s: %v", p, err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

// repoRootFromCycleTest walks up from this test file to the repo root
// (verified by go.mod), so committed-file paths resolve regardless of cwd.
func repoRootFromCycleTest(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// thisFile is .../cycle/wiring_guard_test.go — repo root is one level up.
	root := filepath.Join(filepath.Dir(thisFile), "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repoRootFromCycleTest: expected go.mod at %s but: %v — has the test file moved?", root, err)
	}
	return root
}
