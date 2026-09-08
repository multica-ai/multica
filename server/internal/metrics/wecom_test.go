package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// Authentication failures require an operator to fix the installation;
// connection failures usually recover. Keep them as independently actionable
// Prometheus series rather than allowing both methods to increment one counter.
func TestAuthAndConnectFailuresAreSeparateSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewWecomMetrics()
	for _, collector := range m.Collectors() {
		if err := reg.Register(collector); err != nil {
			t.Fatalf("register WeCom collector: %v", err)
		}
	}

	m.RecordAuthFailure()
	m.RecordAuthFailure()
	m.RecordConnectFailure()

	values := gatherWecomCounterValues(t, reg)
	if got := values["multica_wecom_auth_failures_total"]; got != 2 {
		t.Errorf("auth failures = %v, want 2", got)
	}
	if got := values["multica_wecom_connect_failures_total"]; got != 1 {
		t.Errorf("connect failures = %v, want 1", got)
	}
}

func gatherWecomCounterValues(t *testing.T, reg prometheus.Gatherer) map[string]float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather WeCom metrics: %v", err)
	}
	values := make(map[string]float64, len(families))
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			if counter := metric.GetCounter(); counter != nil {
				values[family.GetName()] += counter.GetValue()
			}
		}
	}
	return values
}
