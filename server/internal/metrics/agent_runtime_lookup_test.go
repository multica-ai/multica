package metrics_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

const runtimeLookupMetric = "multica_agent_runtime_lookup_total"

// TestAgentRuntimeLookupPrewarmsEverySeries asserts the counter ships every
// source x result combination at zero. An absent series and a zero series look
// the same on a dashboard but behave differently: rate() over an absent series
// returns nothing at all, so a source that has genuinely never fired would be
// indistinguishable from one nobody instrumented.
func TestAgentRuntimeLookupPrewarmsEverySeries(t *testing.T) {
	t.Parallel()

	m := metrics.NewBusinessMetrics()
	fam := metrics.GatherForTest(t, m)[runtimeLookupMetric]
	if fam == nil {
		t.Fatalf("%s not registered", runtimeLookupMetric)
	}

	want := map[string]struct{}{}
	for _, source := range metrics.AllRuntimeLookupSources() {
		for _, result := range metrics.AllRuntimeLookupResults() {
			want[source+"/"+result] = struct{}{}
		}
	}
	for _, mtr := range fam.GetMetric() {
		delete(want, labelPair(mtr, "source")+"/"+labelPair(mtr, "result"))
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for k := range want {
			missing = append(missing, k)
		}
		sort.Strings(missing)
		t.Errorf("prewarm missed %d series: %s", len(missing), strings.Join(missing, ", "))
	}
}

// TestRecordAgentRuntimeLookupNormalizesLabels asserts a call site that passes
// an unclassified source or an unknown result cannot mint a new series. The
// unknown result deliberately lands on "error" rather than "ok": a lookup
// nobody classified is not evidence that the read succeeded.
func TestRecordAgentRuntimeLookupNormalizesLabels(t *testing.T) {
	t.Parallel()

	m := metrics.NewBusinessMetrics()
	m.RecordAgentRuntimeLookup(metrics.RuntimeLookupSourceHeartbeatWS, metrics.RuntimeLookupResultOK)
	m.RecordAgentRuntimeLookup(metrics.RuntimeLookupSourceHeartbeatWS, metrics.RuntimeLookupResultNotFound)
	m.RecordAgentRuntimeLookup("  HEARTBEAT_WS  ", metrics.RuntimeLookupResultOK)
	m.RecordAgentRuntimeLookup("brand_new_call_site", "who_knows")

	counts := runtimeLookupCounts(t, m)
	for _, tc := range []struct {
		source, result string
		want           float64
	}{
		{metrics.RuntimeLookupSourceHeartbeatWS, metrics.RuntimeLookupResultOK, 2},
		{metrics.RuntimeLookupSourceHeartbeatWS, metrics.RuntimeLookupResultNotFound, 1},
		{metrics.RuntimeLookupSourceOther, metrics.RuntimeLookupResultError, 1},
	} {
		if got := counts[tc.source+"/"+tc.result]; got != tc.want {
			t.Errorf("%s/%s = %v, want %v", tc.source, tc.result, got, tc.want)
		}
	}

	var total float64
	for _, v := range counts {
		total += v
	}
	if total != 4 {
		t.Errorf("total samples = %v, want 4 (an unknown label must reuse a series, not add one)", total)
	}
}

// ---- helpers --------------------------------------------------------------

func runtimeLookupCounts(t *testing.T, m *metrics.BusinessMetrics) map[string]float64 {
	t.Helper()

	fam := metrics.GatherForTest(t, m)[runtimeLookupMetric]
	if fam == nil {
		t.Fatalf("%s not registered", runtimeLookupMetric)
	}
	out := map[string]float64{}
	for _, mtr := range fam.GetMetric() {
		out[labelPair(mtr, "source")+"/"+labelPair(mtr, "result")] = mtr.GetCounter().GetValue()
	}
	return out
}

func labelPair(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}
