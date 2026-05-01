package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
)

// TestReportTaskUsage_RollsUpBudgetState verifies that ReportTaskUsage both
// stores the raw token usage AND increments the agent + workspace
// budget_state rows for the current day and month windows. Confirms the
// JEH-327 wiring without depending on any cap-enforcement logic.
func TestReportTaskUsage_RollsUpBudgetState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'budget rollup test', 'todo', 'none', 'member', $2, 99001, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'running', 0)
		RETURNING id
	`, agentID, testRuntimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		// budget_state rows reference workspace_id, not the task — purge by
		// scope so the test is idempotent and doesn't drag values across runs.
		testPool.Exec(ctx, `DELETE FROM budget_state WHERE workspace_id = $1 AND scope_id IN ($2, $1)`, testWorkspaceID, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Sonnet at 1M input + 1M output = 300 + 1500 = 1800 cents per call.
	body := map[string]any{
		"usage": []map[string]any{
			{
				"provider":           "claude",
				"model":              "claude-sonnet-4-6",
				"input_tokens":       1_000_000,
				"output_tokens":      1_000_000,
				"cache_read_tokens":  0,
				"cache_write_tokens": 0,
			},
		},
	}
	req := newRequest("POST", "/api/daemon/tasks/"+taskID+"/usage", body)
	req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, "budget-test-daemon"))
	req = withURLParam(req, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.ReportTaskUsage(w, req)
	if w.Code != 200 {
		t.Fatalf("ReportTaskUsage: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	const expectedCents = 1800

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	checks := []struct {
		scopeType string
		scopeID   string
		windowT   string
		windowS   time.Time
	}{
		{"agent", agentID, "day", dayStart},
		{"agent", agentID, "month", monthStart},
		{"workspace", testWorkspaceID, "day", dayStart},
		{"workspace", testWorkspaceID, "month", monthStart},
	}
	for _, c := range checks {
		var got int64
		err := testPool.QueryRow(ctx, `
			SELECT cents_spent FROM budget_state
			WHERE workspace_id = $1 AND scope_type = $2 AND scope_id = $3
			  AND window_type = $4 AND window_start = $5
		`, testWorkspaceID, c.scopeType, c.scopeID, c.windowT, c.windowS).Scan(&got)
		if err != nil {
			t.Errorf("missing budget_state row for scope=%s window=%s: %v", c.scopeType, c.windowT, err)
			continue
		}
		if got != expectedCents {
			t.Errorf("scope=%s window=%s: cents_spent = %d, want %d", c.scopeType, c.windowT, got, expectedCents)
		}
	}

	// Send a second usage report — IncrementBudgetState should add to the
	// existing rows, not replace them. (Tests the ON CONFLICT … DO UPDATE.)
	w = httptest.NewRecorder()
	req2 := newRequest("POST", "/api/daemon/tasks/"+taskID+"/usage", body)
	req2 = req2.WithContext(middleware.WithDaemonContext(req2.Context(), testWorkspaceID, "budget-test-daemon"))
	req2 = withURLParam(req2, "taskId", taskID)
	testHandler.ReportTaskUsage(w, req2)
	if w.Code != 200 {
		t.Fatalf("second ReportTaskUsage: got %d: %s", w.Code, w.Body.String())
	}

	var doubled int64
	if err := testPool.QueryRow(ctx, `
		SELECT cents_spent FROM budget_state
		WHERE workspace_id = $1 AND scope_type = 'agent' AND scope_id = $2
		  AND window_type = 'day' AND window_start = $3
	`, testWorkspaceID, agentID, dayStart).Scan(&doubled); err != nil {
		t.Fatalf("re-read budget_state: %v", err)
	}
	if doubled != 2*expectedCents {
		t.Errorf("after second report cents_spent = %d, want %d", doubled, 2*expectedCents)
	}
}

// TestReportTaskUsage_UnknownModelStillRollsUp ensures the worst-case Opus
// fallback is applied (and budget_state still increments) when an unknown
// model name flows through. Critical safety property: a misnamed model must
// not silently bypass budget tracking.
func TestReportTaskUsage_UnknownModelStillRollsUp(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'budget unknown model test', 'todo', 'none', 'member', $2, 99002, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'running', 0)
		RETURNING id
	`, agentID, testRuntimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM budget_state WHERE workspace_id = $1 AND scope_id IN ($2, $1)`, testWorkspaceID, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	body := map[string]any{
		"usage": []map[string]any{
			{
				"provider":      "future",
				"model":         "model-not-yet-priced",
				"input_tokens":  100_000,
				"output_tokens": 100_000,
			},
		},
	}
	req := newRequest("POST", "/api/daemon/tasks/"+taskID+"/usage", body)
	req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, "budget-test-daemon"))
	req = withURLParam(req, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.ReportTaskUsage(w, req)
	if w.Code != 200 {
		t.Fatalf("ReportTaskUsage: got %d: %s", w.Code, w.Body.String())
	}

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	var got int64
	if err := testPool.QueryRow(ctx, `
		SELECT cents_spent FROM budget_state
		WHERE workspace_id = $1 AND scope_type = 'agent' AND scope_id = $2
		  AND window_type = 'day' AND window_start = $3
	`, testWorkspaceID, agentID, dayStart).Scan(&got); err != nil {
		t.Fatalf("read budget_state: %v", err)
	}

	// Opus pricing on 100k+100k = 150 + 750 = 900 cents — anything less means
	// we silently let the unknown model through with a smaller cost.
	const minCents = 900
	if got < minCents {
		t.Errorf("unknown model fallback didn't apply Opus pricing: cents_spent = %d, want >= %d", got, minCents)
	}
}
