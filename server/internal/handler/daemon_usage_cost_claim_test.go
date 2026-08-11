package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestReportCursorUsageCost_SameTaskRetryAndCrossTaskConflict covers the claim
// path that previously aborted the Postgres transaction on unique violations
// (23505 → subsequent statements 25P02). Same-task retries must succeed;
// cross-task conflicts must return 409 while leaving the winner's claim intact.
func TestReportCursorUsageCost_SameTaskRetryAndCrossTaskConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'cursor cost claim fixture', 'in_progress', 'none', $2, 'member',
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1), 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	createTask := func(name string) string {
		t.Helper()
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
			VALUES ($1, $2, $3, 'completed', 0, now())
			RETURNING id
		`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
			t.Fatalf("setup: create %s task: %v", name, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM cursor_usage_event_claim WHERE task_id = $1`, taskID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage WHERE task_id = $1`, taskID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		})
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cost_usd_ticks)
			VALUES ($1, 'cursor', 'composer-1', 10, 5, 100)
		`, taskID); err != nil {
			t.Fatalf("setup: create %s usage: %v", name, err)
		}
		return taskID
	}

	taskA := createTask("winner")
	taskB := createTask("loser")
	accountKey := "cursor-acct-claim-test"
	occurrence := "occ-claim-conflict-1"
	concurrentOccurrence := "occ-claim-conflict-2"
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM cursor_usage_event_claim
			WHERE account_key = $1 AND occurrence_key = ANY($2::text[])
		`, accountKey, []string{occurrence, concurrentOccurrence})
	})

	postCost := func(taskID, occurrenceKey string) *httptest.ResponseRecorder {
		t.Helper()
		body := map[string]any{
			"account_key": accountKey,
			"corrections": []map[string]any{{
				"model":           "composer-1",
				"cost_usd_ticks":  250,
				"occurrence_keys": []string{occurrenceKey},
			}},
		}
		w := httptest.NewRecorder()
		req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/usage/cost", body, testWorkspaceID, "legit-daemon")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("taskId", taskID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		testHandler.ReportCursorUsageCost(w, req)
		return w
	}

	if w := postCost(taskA, occurrence); w.Code != http.StatusOK {
		t.Fatalf("first claim: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := postCost(taskA, occurrence); w.Code != http.StatusOK {
		t.Fatalf("same-task retry: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := postCost(taskB, occurrence); w.Code != http.StatusConflict {
		t.Fatalf("cross-task conflict: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	start := make(chan struct{})
	results := make(chan int, 2)
	for _, taskID := range []string{taskA, taskB} {
		go func() {
			<-start
			results <- postCost(taskID, concurrentOccurrence).Code
		}()
	}
	close(start)
	codeA, codeB := <-results, <-results
	if !((codeA == http.StatusOK && codeB == http.StatusConflict) ||
		(codeA == http.StatusConflict && codeB == http.StatusOK)) {
		t.Fatalf("concurrent claims returned %d and %d, want one 200 and one 409", codeA, codeB)
	}

	var ownerTaskID string
	if err := testPool.QueryRow(ctx, `
		SELECT task_id::text
		FROM cursor_usage_event_claim
		WHERE account_key = $1 AND occurrence_key = $2
	`, accountKey, occurrence).Scan(&ownerTaskID); err != nil {
		t.Fatalf("load claim: %v", err)
	}
	if ownerTaskID != taskA {
		t.Fatalf("claim owner = %s, want winner %s", ownerTaskID, taskA)
	}
	var costTicks *int64
	if err := testPool.QueryRow(ctx, `
		SELECT cost_usd_ticks FROM task_usage
		WHERE task_id = $1 AND provider = 'cursor' AND model = 'composer-1'
	`, taskA).Scan(&costTicks); err != nil {
		t.Fatalf("load winner cost: %v", err)
	}
	if costTicks == nil || *costTicks != 250 {
		raw, _ := json.Marshal(costTicks)
		t.Fatalf("winner cost_usd_ticks = %s, want 250", raw)
	}
}
