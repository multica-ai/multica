package daemon

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// TestReportModelListResult_RetriesOn500AndEventuallySucceeds pins the
// regression GPT-Boy flagged on PR #2022: handleModelList used to call
// d.client.ReportModelListResult directly and swallow any 5xx, leaving the
// pending request stranded in "running" until its 60s server-side timeout —
// which is exactly the failure mode the multi-node store fix was meant to
// eliminate. With the retry helper in place a transient store failure on
// the server side gets re-tried until it lands.
func TestReportModelListResult_RetriesOn500AndEventuallySucceeds(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	var hits int32
	d, calls := localSkillReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n <= 2 {
			http.Error(w, "{}", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	d.reportModelListResult(context.Background(), Runtime{ID: "rt-1"}, "req-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 3 {
		t.Fatalf("expected 3 attempts (2 failures + 1 success), got %d", got)
	}
}

// TestReportModelListResult_DoesNotRetryOn4xx pins that 4xx (e.g. the request
// expired or was cross-workspace) is treated as terminal — retrying just
// burns heartbeat cycles.
func TestReportModelListResult_DoesNotRetryOn4xx(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	d, calls := localSkillReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"request not found"}`, http.StatusNotFound)
	})

	d.reportModelListResult(context.Background(), Runtime{ID: "rt-1"}, "req-1", map[string]any{"status": "completed"})

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt (4xx is terminal), got %d", got)
	}
}

// TestReportModelListResult_SendsCorrectPath smoke-tests the URL the daemon
// posts to, so a future client refactor doesn't silently aim reports at the
// wrong endpoint.
func TestReportModelListResult_SendsCorrectPath(t *testing.T) {
	withFastLocalSkillReportBackoffs(t)

	var path string
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	d.reportModelListResult(context.Background(), Runtime{ID: "rt-a"}, "req-1", map[string]any{"status": "completed"})

	if !strings.HasSuffix(path, "/api/daemon/runtimes/rt-a/models/req-1/result") {
		t.Fatalf("model list report path = %q", path)
	}
}
