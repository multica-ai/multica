package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	dto "github.com/prometheus/client_model/go"

	"github.com/multica-ai/multica/server/internal/daemonws"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestAgentRuntimeLookupSourcesAreDistinct drives the three entry points the
// investigation in MUL-6884 could not tell apart — the WebSocket heartbeat, the
// HTTP heartbeat fallback, and a browser poll — and asserts each lands on its
// own source.
//
// This is the whole point of the metric. All three issue the same statement, so
// pg_stat_statements sums them into one row; if they also shared a Prometheus
// label the counter would answer no question the database could not already
// answer, and the follow-up decision (raise the heartbeat interval, or back off
// the 500ms polls) would still be a guess.
func TestAgentRuntimeLookupSourcesAreDistinct(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	m := withTestMetrics(t)
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "Lookup source runtime", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"device_info":  "lookup source runtime",
	})

	before := lookupSnapshot(t, m)

	if _, err := testHandler.HandleDaemonWSHeartbeat(ctx,
		daemonws.ClientIdentity{WorkspaceID: testWorkspaceID}, runtimeID, false); err != nil {
		t.Fatalf("HandleDaemonWSHeartbeat: %v", err)
	}

	httpBeat := newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat",
		map[string]any{"runtime_id": runtimeID}, testWorkspaceID, "lookup-source-daemon")
	testutil.Call(t, testHandler.DaemonHeartbeat, httpBeat).Want(http.StatusOK)

	// A model poll for a runtime that no longer exists: the read-access gate
	// runs the lookup before any auth check, so this exercises the poll's
	// source and the not_found classification in one request.
	poll := withURLParam(httptest.NewRequest(http.MethodGet,
		"/api/runtimes/"+uuid.NewString()+"/models/req-1", nil), "runtimeId", uuid.NewString())
	testutil.Call(t, testHandler.GetModelListRequest, poll).Want(http.StatusNotFound)

	after := lookupSnapshot(t, m)
	for _, want := range []struct {
		source, result string
	}{
		{obsmetrics.RuntimeLookupSourceHeartbeatWS, obsmetrics.RuntimeLookupResultOK},
		{obsmetrics.RuntimeLookupSourceHeartbeatHTTP, obsmetrics.RuntimeLookupResultOK},
		{obsmetrics.RuntimeLookupSourceRuntimeModelPoll, obsmetrics.RuntimeLookupResultNotFound},
	} {
		key := want.source + "/" + want.result
		if got := after[key] - before[key]; got != 1 {
			t.Errorf("%s delta = %v, want 1", key, got)
		}
	}
	// The poll must not have been billed to the heartbeat, nor the heartbeats
	// to each other.
	for _, key := range []string{
		obsmetrics.RuntimeLookupSourceHeartbeatWS + "/" + obsmetrics.RuntimeLookupResultNotFound,
		obsmetrics.RuntimeLookupSourceHeartbeatHTTP + "/" + obsmetrics.RuntimeLookupResultNotFound,
		obsmetrics.RuntimeLookupSourceRuntimeModelPoll + "/" + obsmetrics.RuntimeLookupResultOK,
		obsmetrics.RuntimeLookupSourceOther + "/" + obsmetrics.RuntimeLookupResultOK,
	} {
		if got := after[key] - before[key]; got != 0 {
			t.Errorf("%s delta = %v, want 0", key, got)
		}
	}
}

// ---- helpers --------------------------------------------------------------

// withTestMetrics installs a fresh collector on the shared test handler for the
// duration of one test. testHandler.Metrics is nil by default, which every
// Record* call tolerates — so without this the counter would silently stay at
// zero and the assertions above would pass for the wrong reason.
func withTestMetrics(t *testing.T) *obsmetrics.BusinessMetrics {
	t.Helper()

	previous := testHandler.Metrics
	m := obsmetrics.NewBusinessMetrics()
	testHandler.Metrics = m
	t.Cleanup(func() { testHandler.Metrics = previous })
	return m
}

func lookupSnapshot(t *testing.T, m *obsmetrics.BusinessMetrics) map[string]float64 {
	t.Helper()

	fam := obsmetrics.GatherForTest(t, m)["multica_agent_runtime_lookup_total"]
	if fam == nil {
		t.Fatalf("multica_agent_runtime_lookup_total not registered")
	}
	out := map[string]float64{}
	for _, mtr := range fam.GetMetric() {
		out[metricLabel(mtr, "source")+"/"+metricLabel(mtr, "result")] = mtr.GetCounter().GetValue()
	}
	return out
}

func metricLabel(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}
