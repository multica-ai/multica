package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// SumAllCounters returns the running sum across every counter sample
// currently registered with this BusinessMetrics receiver. Used by the lint
// test in business_pairing_test.go to confirm that a synthetic event causes
// AT LEAST ONE counter to advance — i.e. that the IncForEvent dispatch
// covered the case. Production code never calls this.
//
// Histograms and gauges are deliberately excluded so prewarmed buckets
// (e.g. failure_reason 0-counts) don't make every event "pass" trivially.
func SumAllCounters(m *BusinessMetrics) float64 {
	if m == nil {
		return 0
	}
	reg := prometheus.NewPedanticRegistry()
	for _, c := range m.Collectors() {
		// MustRegister panics on duplicate; we use a fresh registry each call.
		reg.MustRegister(c)
	}
	families, err := reg.Gather()
	if err != nil {
		return 0
	}
	var total float64
	for _, fam := range families {
		if fam.GetType() != dto.MetricType_COUNTER {
			continue
		}
		for _, mtr := range fam.GetMetric() {
			if c := mtr.GetCounter(); c != nil {
				total += c.GetValue()
			}
		}
	}
	return total
}

// GatherForTest registers every collector on a fresh registry and returns
// the resulting metric families keyed by name. Test-only — the production
// /metrics endpoint reads from the shared registry constructed in
// NewRegistry. Any Gather error is reported via t.Fatalf so callers can
// dereference the result without nil checks.
func GatherForTest(t *testing.T, m *BusinessMetrics) map[string]*dto.MetricFamily {
	t.Helper()
	if m == nil {
		t.Fatalf("GatherForTest: nil BusinessMetrics")
	}
	reg := prometheus.NewPedanticRegistry()
	for _, c := range m.Collectors() {
		reg.MustRegister(c)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("GatherForTest: gather failed: %v", err)
	}
	out := make(map[string]*dto.MetricFamily, len(families))
	for _, fam := range families {
		out[fam.GetName()] = fam
	}
	return out
}
