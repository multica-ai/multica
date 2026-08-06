package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestWecomAdapterMetricsRejectsHighCardinalityLabels is the cardinality
// canary for the WeCom adapter. The adapter's call sites all know an
// installation id and the temptation is to label with it; forbiddenMetricLabels
// exists precisely because that id is unbounded.
func TestWecomAdapterMetricsRejectsHighCardinalityLabels(t *testing.T) {
	m := NewWecomAdapterMetrics()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(m.Collectors()...)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("no wecom metric families registered")
	}
	for _, fam := range families {
		if !strings.HasPrefix(fam.GetName(), "multica_wecom_") {
			t.Errorf("metric %q is not namespaced under multica_wecom_", fam.GetName())
		}
		for _, metric := range fam.GetMetric() {
			for _, label := range metric.GetLabel() {
				if _, forbidden := forbiddenMetricLabels[label.GetName()]; forbidden {
					t.Errorf("metric %q carries forbidden high-cardinality label %q",
						fam.GetName(), label.GetName())
				}
			}
		}
	}
}

// TestWecomAdapterMetricsCollapsesUnknownLabelValues: the adapter and the
// metrics package are separate enums. A value the allow-list has not seen must
// land in an "other" bucket rather than mint a new series per call.
func TestWecomAdapterMetricsCollapsesUnknownLabelValues(t *testing.T) {
	m := NewWecomAdapterMetrics()

	seeded := testutil.CollectAndCount(m.OutboundEnqueued, "multica_wecom_outbound_enqueued_total")
	for i := 0; i < 50; i++ {
		m.RecordOutboundEnqueued("path-from-a-future-refactor", "source-kind-"+string(rune('a'+i%26)))
	}
	if got := testutil.CollectAndCount(m.OutboundEnqueued, "multica_wecom_outbound_enqueued_total"); got != seeded+1 {
		t.Fatalf("outbound_enqueued series = %d, want %d (50 unknown pairs collapsed into one)", got, seeded+1)
	}
	if got := testutil.ToFloat64(m.OutboundEnqueued.WithLabelValues("other", "other")); got != 50 {
		t.Fatalf("other/other counter = %v, want 50", got)
	}

	seeded = testutil.CollectAndCount(m.OutboundDelivery, "multica_wecom_outbound_delivery_total")
	for i := 0; i < 20; i++ {
		m.RecordOutboundDelivery("outcome-" + string(rune('a'+i)))
	}
	if got := testutil.CollectAndCount(m.OutboundDelivery, "multica_wecom_outbound_delivery_total"); got != seeded+1 {
		t.Fatalf("outbound_delivery series = %d, want %d", got, seeded+1)
	}
}

// TestWecomAdapterMetricsSeedsEnqueuePathZeros keeps the reconcile-path alert
// usable: rate() needs a zero baseline from process start, otherwise a freshly
// restarted server reports "no data" instead of "fast path healthy".
func TestWecomAdapterMetricsSeedsEnqueuePathZeros(t *testing.T) {
	m := NewWecomAdapterMetrics()
	for _, path := range []string{WecomEnqueuePathFast, WecomEnqueuePathReconcile} {
		for _, kind := range []string{"chat_done", "task_failed"} {
			if got := testutil.ToFloat64(m.OutboundEnqueued.WithLabelValues(path, kind)); got != 0 {
				t.Fatalf("seeded series %s/%s = %v, want 0", path, kind, got)
			}
		}
	}
	if got := testutil.CollectAndCount(m.OutboundEnqueued, "multica_wecom_outbound_enqueued_total"); got != 4 {
		t.Fatalf("seeded enqueue series = %d, want 4", got)
	}
	if got := testutil.CollectAndCount(m.OutboundDelivery, "multica_wecom_outbound_delivery_total"); got != 5 {
		t.Fatalf("seeded delivery series = %d, want 5", got)
	}
}

// TestWecomAdapterMetricsInstallResultsMatchAdapterReasons: the install result
// allow-list here and the InstallError* constants in the adapter are two copies
// of one enum. A reason missing here silently lands in "other", which turns a
// specific install failure into an unattributable blip.
func TestWecomAdapterMetricsInstallResultsMatchAdapterReasons(t *testing.T) {
	// Mirrors internal/integrations/wecom/install.go.
	adapterReasons := []string{
		"expired",
		"generate_failed",
		"integration_unconfigured",
		"installation_conflict",
		"wecom_protocol_error",
		"internal_error",
	}
	m := NewWecomAdapterMetrics()
	for _, reason := range adapterReasons {
		m.RecordInstallSessionTerminal(reason)
		if got := testutil.ToFloat64(m.InstallSessions.WithLabelValues(reason)); got != 1 {
			t.Errorf("install reason %q was not recorded under its own label (got %v); "+
				"add it to knownWecomInstallResults", reason, got)
		}
	}
	m.RecordInstallSessionTerminal("succeeded")
	if got := testutil.ToFloat64(m.InstallSessions.WithLabelValues("succeeded")); got != 1 {
		t.Errorf("succeeded = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.InstallSessions.WithLabelValues("other")); got != 0 {
		t.Errorf("other bucket = %v, want 0 — a known reason leaked into it", got)
	}
}

// TestRegistryExposesWecomMetrics guards the wiring: the sink is useless if it
// is constructed but never registered on the gatherer /metrics reads from.
func TestRegistryExposesWecomMetrics(t *testing.T) {
	reg := NewRegistry(RegistryOptions{Version: "test", Commit: "abc"})
	if reg.Wecom == nil {
		t.Fatal("registry did not build the wecom metrics sink")
	}
	reg.Wecom.RecordOutboundEnqueued(WecomEnqueuePathReconcile, "chat_done")

	families, err := reg.Gatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, fam := range families {
		if fam.GetName() != "multica_wecom_outbound_enqueued_total" {
			continue
		}
		for _, metric := range fam.GetMetric() {
			labels := map[string]string{}
			for _, l := range metric.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["path"] == WecomEnqueuePathReconcile && labels["source_kind"] == "chat_done" {
				if got := metric.GetCounter().GetValue(); got != 1 {
					t.Fatalf("reconcile/chat_done = %v, want 1", got)
				}
				return
			}
		}
	}
	t.Fatal("multica_wecom_outbound_enqueued_total{path=\"reconcile\",source_kind=\"chat_done\"} not exposed by the registry")
}
