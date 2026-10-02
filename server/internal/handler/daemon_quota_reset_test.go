package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestFailTaskQuotaResetHintCreatesOneDeferredRetry(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "quota-reset-runtime")
	agentID := dbfx.Agent(t, "quota-reset-agent", runtimeID)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_config='{"quota_auto_resume":true}'::jsonb WHERE id=$1`, agentID); err != nil {
		t.Fatal(err)
	}
	issueID := dbfx.Issue(t, "quota reset retry", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": runtimeID, "status": "running",
		"attempt": 1, "max_attempts": 2, "session_id": "quota-session",
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id=$1`, taskID)
	})
	reset := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	call := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/fail", map[string]any{
			"error": "quota window exhausted", "failure_reason": "agent_error.provider_quota_limit",
			"session_id": "quota-session", "quota_reset_at": reset.Format(time.RFC3339),
		}, testWorkspaceID, "quota-reset-daemon")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("taskId", taskID)
		testHandler.FailTask(w, req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx)))
		return w
	}
	for i := 0; i < 2; i++ {
		w := call()
		if w.Code != http.StatusOK {
			t.Fatalf("fail callback %d: status=%d body=%s", i+1, w.Code, w.Body.String())
		}
	}
	var n int
	var status string
	var fireAt time.Time
	if err := testPool.QueryRow(ctx, `SELECT count(*), COALESCE(max(status),''), max(fire_at) FROM agent_task_queue WHERE parent_task_id=$1`, taskID).Scan(&n, &status, &fireAt); err != nil {
		t.Fatal(err)
	}
	if n != 1 || status != "deferred" || fireAt.Before(reset) {
		t.Errorf("retry count=%d status=%q fire_at=%s, want one deferred retry after %s", n, status, fireAt, reset)
	}
}
