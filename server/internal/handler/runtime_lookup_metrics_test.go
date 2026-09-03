package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	dto "github.com/prometheus/client_model/go"

	"github.com/multica-ai/multica/server/internal/daemonws"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestAgentRuntimeLookupWSHotPathIsZeroRead drives 1,000 heartbeats through one
// connection lease and proves they do not increment the GetAgentRuntime metric.
// HTTP fallback and browser polling remain attributed as before.
func TestAgentRuntimeLookupWSHotPathIsZeroRead(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	m := withTestMetrics(t)
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "Lookup source runtime", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"device_info":  "lookup source runtime",
	})
	fake := &fakeLivenessStore{available: true, aliveOK: true}
	h := *testHandler
	h.LivenessStore = fake
	identity := daemonws.ClientIdentity{
		WorkspaceID: testWorkspaceID,
		RuntimeLeases: map[string]*daemonws.RuntimeLease{
			runtimeID: daemonws.NewRuntimeLease(testWorkspaceID, "online", time.Now(), true),
		},
	}

	before := lookupSnapshot(t, m)

	for i := 0; i < 1000; i++ {
		if _, err := h.HandleDaemonWSHeartbeat(ctx, identity, runtimeID, false); err != nil {
			t.Fatalf("HandleDaemonWSHeartbeat %d: %v", i, err)
		}
	}

	httpBeat := newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat",
		map[string]any{"runtime_id": runtimeID}, testWorkspaceID, "lookup-source-daemon")
	testutil.Call(t, h.DaemonHeartbeat, httpBeat).Want(http.StatusOK)

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
		obsmetrics.RuntimeLookupSourceHeartbeatWS + "/" + obsmetrics.RuntimeLookupResultOK,
		obsmetrics.RuntimeLookupSourceHeartbeatWS + "/" + obsmetrics.RuntimeLookupResultNotFound,
		obsmetrics.RuntimeLookupSourceHeartbeatHTTP + "/" + obsmetrics.RuntimeLookupResultNotFound,
		obsmetrics.RuntimeLookupSourceRuntimeModelPoll + "/" + obsmetrics.RuntimeLookupResultOK,
		obsmetrics.RuntimeLookupSourceOther + "/" + obsmetrics.RuntimeLookupResultOK,
	} {
		if got := after[key] - before[key]; got != 0 {
			t.Errorf("%s delta = %v, want 0", key, got)
		}
	}
	if got := fake.touchCount(); got != 1001 {
		t.Errorf("liveness touches = %d, want 1001 (1,000 WS + 1 HTTP)", got)
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
