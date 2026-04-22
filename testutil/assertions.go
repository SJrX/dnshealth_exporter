package testutil

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// MetricOption configures a metric assertion.
type MetricOption func(*metricMatcher)

type metricMatcher struct {
	labels   map[string]string
	hasValue bool
	value    float64
}

// WithLabels specifies label key-value pairs to match.
// Pass as alternating key, value strings.
func WithLabels(pairs ...string) MetricOption {
	return func(m *metricMatcher) {
		if m.labels == nil {
			m.labels = make(map[string]string)
		}
		for i := 0; i+1 < len(pairs); i += 2 {
			m.labels[pairs[i]] = pairs[i+1]
		}
	}
}

// WithValue specifies the expected metric value.
func WithValue(v float64) MetricOption {
	return func(m *metricMatcher) {
		m.hasValue = true
		m.value = v
	}
}

// AssertGauge asserts that a gauge metric with the given name and
// matching labels exists in the registry with the expected value.
func AssertGauge(t testing.TB, registry *prometheus.Registry, name string, opts ...MetricOption) {
	t.Helper()
	m := &metricMatcher{}
	for _, opt := range opts {
		opt(m)
	}

	metric := findMetric(t, registry, name, m.labels)
	if metric == nil {
		t.Fatalf("metric %s with labels %v not found", name, m.labels)
	}

	if m.hasValue {
		got := metric.GetGauge().GetValue()
		if got != m.value {
			t.Errorf("metric %s with labels %v: got value %v, want %v",
				name, m.labels, got, m.value)
		}
	}
}

// AssertGaugeExists asserts that a gauge metric exists (any value).
func AssertGaugeExists(t testing.TB, registry *prometheus.Registry, name string, opts ...MetricOption) {
	t.Helper()
	m := &metricMatcher{}
	for _, opt := range opts {
		opt(m)
	}

	metric := findMetric(t, registry, name, m.labels)
	if metric == nil {
		t.Fatalf("metric %s with labels %v not found", name, m.labels)
	}
}

// AssertGaugeMissing asserts that no metric with the given name
// and labels exists.
func AssertGaugeMissing(t testing.TB, registry *prometheus.Registry, name string, opts ...MetricOption) {
	t.Helper()
	m := &metricMatcher{}
	for _, opt := range opts {
		opt(m)
	}

	metric := findMetric(t, registry, name, m.labels)
	if metric != nil {
		t.Fatalf("metric %s with labels %v should not exist, but found value %v",
			name, m.labels, metric.GetGauge().GetValue())
	}
}

func findMetric(t testing.TB, registry *prometheus.Registry, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}

	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if matchLabels(m, labels) {
				return m
			}
		}
	}
	return nil
}

func matchLabels(m *dto.Metric, want map[string]string) bool {
	if len(want) == 0 {
		return true
	}
	got := make(map[string]string)
	for _, lp := range m.GetLabel() {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

// DumpMetrics prints all metrics in the registry for debugging.
func DumpMetrics(t testing.TB, registry *prometheus.Registry) {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	for _, fam := range families {
		for _, m := range fam.GetMetric() {
			labels := ""
			for _, lp := range m.GetLabel() {
				if labels != "" {
					labels += ","
				}
				labels += fmt.Sprintf("%s=%q", lp.GetName(), lp.GetValue())
			}
			t.Logf("  %s{%s} = %v", fam.GetName(), labels, m.GetGauge().GetValue())
		}
	}
}
