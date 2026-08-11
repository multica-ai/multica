package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Every collector has to be registrable together. A duplicate name or a
// malformed help string only surfaces at MustRegister, which in production is
// process start.
func TestWecomMetricsRegisterCleanly(t *testing.T) {
	reg := prometheus.NewRegistry()
	for _, c := range NewWecomMetrics().Collectors() {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
}

// The adapter's Metrics interface and this implementation must not drift. The
// compile-time check lives in the adapter; this one catches a method that
// exists but does nothing.
func TestEveryWecomCounterActuallyCounts(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewWecomMetrics()
	for _, c := range m.Collectors() {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	m.RecordConnectFailure()
	m.RecordAuthFailure()
	m.RecordCallbackQueued()
	m.RecordCallbackQueueBlocked()

	seen := gatherWecomValues(t, reg)
	for _, want := range []string{
		"multica_wecom_connect_failures_total",
		"multica_wecom_auth_failures_total",
		"multica_wecom_inbound_callbacks_total",
		"multica_wecom_inbound_queue_blocked_total",
	} {
		if seen[want] != 1 {
			t.Errorf("%s = %v, want 1 — the counter is wired to nothing", want, seen[want])
		}
	}
}

// The whole point of two connection counters is that an operator can tell a
// wrong secret from a dead network. Sharing a series would put them back to
// guessing.
func TestAuthAndConnectFailuresAreSeparateSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewWecomMetrics()
	for _, c := range m.Collectors() {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	m.RecordAuthFailure()
	m.RecordAuthFailure()
	m.RecordConnectFailure()

	seen := gatherWecomValues(t, reg)
	if got := seen["multica_wecom_auth_failures_total"]; got != 2 {
		t.Errorf("auth failures = %v, want 2", got)
	}
	if got := seen["multica_wecom_connect_failures_total"]; got != 1 {
		t.Errorf("connect failures = %v, want 1", got)
	}
}

// No metric here may carry an unbounded identifier as a label — the same rule
// labels.go enforces for the rest of the codebase. installation_id is the one
// that would be tempting to add, and it belongs in the logs instead. Those
// carry it for the two connection failures, which reach the Supervisor as a
// returned error; the two inbound counters have no log line beside them, so
// leaving the label off really does cost the answer to "which bot" on those
// two. That is the trade this test pins, not a free win.
func TestWecomMetricsCarryNoUnboundedLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewWecomMetrics()
	for _, c := range m.Collectors() {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	m.RecordConnectFailure()
	m.RecordAuthFailure()
	m.RecordCallbackQueued()
	m.RecordCallbackQueueBlocked()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		for _, metric := range f.GetMetric() {
			for _, l := range metric.GetLabel() {
				if _, forbidden := forbiddenMetricLabels[l.GetName()]; forbidden {
					t.Errorf("%s carries the unbounded label %q", f.GetName(), l.GetName())
				}
				if l.GetName() == "installation_id" {
					t.Errorf("%s labels by installation_id; that belongs in the logs", f.GetName())
				}
			}
		}
	}
}

// The registry is what production actually scrapes; a collector that is built
// but never registered exports nothing.
func TestTheRegistryExposesTheWecomCounters(t *testing.T) {
	r := NewRegistry(RegistryOptions{})
	if r.Wecom == nil {
		t.Fatal("Registry.Wecom is nil; nothing can report through it")
	}
	r.Wecom.RecordAuthFailure()

	families, err := r.Gatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() == "multica_wecom_auth_failures_total" {
			return
		}
	}
	t.Fatal("multica_wecom_auth_failures_total is not on the registry the metrics server scrapes")
}

func gatherWecomValues(t *testing.T, reg prometheus.Gatherer) map[string]float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	out := map[string]float64{}
	for _, f := range families {
		for _, metric := range f.GetMetric() {
			out[f.GetName()] = wecomValueOf(metric)
		}
	}
	return out
}

func wecomValueOf(m *dto.Metric) float64 {
	if c := m.GetCounter(); c != nil {
		return c.GetValue()
	}
	if g := m.GetGauge(); g != nil {
		return g.GetValue()
	}
	return 0
}
