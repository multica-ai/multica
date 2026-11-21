package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func rateLimitFixture(t *testing.T, label string) (runtimeID, taskID string) {
	t.Helper()
	name := "rate-limits " + label
	runtimeID = dbfx.Runtime(t, name+" runtime")
	agentID := dbfx.Agent(t, name+" agent", runtimeID)
	issueID := dbfx.Issue(t, name+" issue")
	taskID = dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running"})
	dbfx.Cleanup(t, `DELETE FROM runtime_rate_limit WHERE runtime_id = $1`, runtimeID)
	return runtimeID, taskID
}

func sampleRateLimits(observedAt time.Time, sessionUsed float64) map[string]any {
	return map[string]any{
		"plan_type": "pro",
		"status":    "warning",
		"windows": []map[string]any{
			{"kind": "session", "used_percent": sessionUsed, "window_minutes": 300, "resets_at": "2026-11-16T06:00:00Z", "status": "warning"},
			{"kind": "weekly", "used_percent": 41, "window_minutes": 10080, "resets_at": "2026-11-19T01:00:00Z"},
			// A window kind this server cannot label is dropped, not rejected.
			{"kind": "monthly_future", "used_percent": 3},
		},
		"credits":     map[string]any{"has_credits": true, "unlimited": false, "balance": "240"},
		"observed_at": observedAt.UTC().Format(time.RFC3339Nano),
		// A field this server does not know must not reach the column.
		"account_email": "someone@example.com",
	}
}

func reportRateLimits(t *testing.T, taskID string, snapshot any) *testutil.Response {
	t.Helper()
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/rate-limits",
		map[string]any{"rate_limits": snapshot}), "taskId", taskID)
	return testutil.Call(t, testHandler.ReportTaskRateLimits, req)
}

type rateLimitsResponse struct {
	RateLimits *runtimeRateLimitSnapshot `json:"rate_limits"`
}

func getRateLimits(t *testing.T, runtimeID string) rateLimitsResponse {
	t.Helper()
	req := withURLParam(newRequest(http.MethodGet, "/api/runtimes/"+runtimeID+"/rate-limits", nil), "runtimeId", runtimeID)
	var resp rateLimitsResponse
	testutil.Call(t, testHandler.GetRuntimeRateLimits, req).Want(http.StatusOK).JSON(&resp)
	return resp
}

func TestRuntimeRateLimits_ReportThenRead(t *testing.T) {
	runtimeID, taskID := rateLimitFixture(t, "report")

	// Before any report the reader gets an explicit null.
	if got := getRateLimits(t, runtimeID); got.RateLimits != nil {
		t.Fatalf("rate limits before report = %+v, want null", got.RateLimits)
	}

	observed := time.Now().Add(-time.Minute).Truncate(time.Second)
	reportRateLimits(t, taskID, sampleRateLimits(observed, 72)).Want(http.StatusNoContent)

	got := getRateLimits(t, runtimeID).RateLimits
	if got == nil || got.PlanType != "pro" || got.Status != "warning" || len(got.Windows) != 2 {
		t.Fatalf("rate limits = %+v", got)
	}
	if w := got.Windows[0]; w.Kind != "session" || w.UsedPercent == nil || *w.UsedPercent != 72 || w.WindowMinutes != 300 || w.ResetsAt == nil {
		t.Fatalf("session window = %+v", w)
	}
	if got.Credits == nil || got.Credits.Balance != "240" || !got.ObservedAt.Equal(observed) {
		t.Fatalf("credits=%+v observed_at=%v", got.Credits, got.ObservedAt)
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT snapshot::text FROM runtime_rate_limit WHERE runtime_id = $1 AND workspace_id = $2`, runtimeID, testWorkspaceID,
	).Scan(&stored); err != nil {
		t.Fatalf("load stored snapshot: %v", err)
	}
	if strings.Contains(stored, "someone@example.com") || strings.Contains(stored, "monthly_future") {
		t.Fatalf("unknown data reached storage: %s", stored)
	}

	// A late report from an older run cannot roll the snapshot back.
	reportRateLimits(t, taskID, sampleRateLimits(observed.Add(-time.Hour), 10)).Want(http.StatusNoContent)
	if w := getRateLimits(t, runtimeID).RateLimits.Windows[0]; *w.UsedPercent != 72 {
		t.Fatalf("older observation replaced the snapshot: %+v", w)
	}

	// A newer one replaces it in place.
	reportRateLimits(t, taskID, sampleRateLimits(observed.Add(30*time.Second), 80)).Want(http.StatusNoContent)
	if w := getRateLimits(t, runtimeID).RateLimits.Windows[0]; *w.UsedPercent != 80 {
		t.Fatalf("newer observation did not replace the snapshot: %+v", w)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM runtime_rate_limit WHERE runtime_id = $1`, runtimeID); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
}

func TestRuntimeRateLimits_FutureObservationIsClamped(t *testing.T) {
	runtimeID, taskID := rateLimitFixture(t, "clock skew")
	reportRateLimits(t, taskID, sampleRateLimits(time.Now().Add(48*time.Hour), 50)).Want(http.StatusNoContent)
	got := getRateLimits(t, runtimeID).RateLimits
	if got == nil || got.ObservedAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("observed_at not clamped: %+v", got)
	}
	// The clamp keeps the runtime updatable by the next honest report.
	reportRateLimits(t, taskID, sampleRateLimits(time.Now().Add(time.Second), 60)).Want(http.StatusNoContent)
	if w := getRateLimits(t, runtimeID).RateLimits.Windows[0]; *w.UsedPercent != 60 {
		t.Fatalf("report after a skewed one was ignored: %+v", w)
	}
}

func TestRuntimeRateLimits_RejectsMalformed(t *testing.T) {
	_, taskID := rateLimitFixture(t, "malformed")
	now := time.Now()

	noTime := sampleRateLimits(now, 10)
	delete(noTime, "observed_at")
	reportRateLimits(t, taskID, noTime).Want(http.StatusBadRequest)

	badStatus := sampleRateLimits(now, 10)
	badStatus["status"] = "throttled"
	reportRateLimits(t, taskID, badStatus).Want(http.StatusBadRequest)

	reportRateLimits(t, taskID, sampleRateLimits(now, maxRateLimitPercent+1)).Want(http.StatusBadRequest)
	reportRateLimits(t, taskID, sampleRateLimits(now, -1)).Want(http.StatusBadRequest)

	longPlan := sampleRateLimits(now, 10)
	longPlan["plan_type"] = strings.Repeat("p", maxRateLimitString+1)
	reportRateLimits(t, taskID, longPlan).Want(http.StatusBadRequest)

	reportRateLimits(t, taskID, nil).Want(http.StatusBadRequest)
}

func TestRuntimeRateLimits_DeletedWithRuntime(t *testing.T) {
	runtimeID, taskID := rateLimitFixture(t, "delete")
	reportRateLimits(t, taskID, sampleRateLimits(time.Now(), 10)).Want(http.StatusNoContent)

	// The runtime-delete handlers unbind agents first; do the same here.
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET runtime_id = NULL WHERE runtime_id = $1`, runtimeID); err != nil {
		t.Fatalf("unbind agent: %v", err)
	}
	if err := testHandler.Queries.DeleteAgentRuntime(context.Background(), parseUUID(runtimeID)); err != nil {
		t.Fatalf("delete runtime: %v", err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM runtime_rate_limit WHERE runtime_id = $1`, runtimeID); n != 0 {
		t.Fatalf("rate-limit rows after runtime delete = %d, want 0", n)
	}
}
