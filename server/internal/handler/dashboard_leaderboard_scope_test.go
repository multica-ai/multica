package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestGetDashboardUsageByModel covers the new per-model aggregation endpoint
// that powers the "Model" scope of the leaderboard: tokens are grouped by
// model (not agent), the project filter excludes out-of-scope tasks, and an
// invalid project_id UUID is rejected with 400.
func TestGetDashboardUsageByModel(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'by-model scope test') RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	mkIssue := func(withProject bool) string {
		var id string
		var pid any
		if withProject {
			pid = projectID
		}
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
			VALUES (
				$1, 'by-model test issue', $2, 'member', $3,
				(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
			)
			RETURNING id
		`, testWorkspaceID, testUserID, pid).Scan(&id); err != nil {
			t.Fatalf("insert issue: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}
	projectIssueID := mkIssue(true)
	otherIssueID := mkIssue(false)

	now := time.Now().UTC()
	started := now.Add(-15 * time.Minute)
	completed := started.Add(5 * time.Minute)

	mkTask := func(issueID, model string, tokens int64) {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
			VALUES ($1, $2, $3, 'completed', $4, $5, now())
			RETURNING id
		`, agentID, issueID, runtimeID, started, completed).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
			VALUES ($1, 'claude', $2, $3, 0, now())
		`, taskID, model, tokens); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	}

	// Project issue uses model "by-model-gpt4"; other issue uses "by-model-sonnet".
	mkTask(projectIssueID, "by-model-gpt4", 2000)
	mkTask(otherIssueID, "by-model-sonnet", 900)

	// Aggregate raw task_usage rows into task_usage_hourly so the endpoint
	// can read them (production uses a cron ticker; tests drive it directly).
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup: %v", err)
	}

	type modelRow struct {
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}

	t.Run("workspace-wide returns both models", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByModel(w, newRequest("GET", "/api/dashboard/usage/by-model?days=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []modelRow
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		totals := map[string]int64{}
		for _, r := range rows {
			totals[r.Model] += r.InputTokens
		}
		if totals["by-model-gpt4"] < 2000 {
			t.Errorf("workspace gpt4: expected >=2000 tokens, got %d", totals["by-model-gpt4"])
		}
		if totals["by-model-sonnet"] < 900 {
			t.Errorf("workspace sonnet: expected >=900 tokens, got %d", totals["by-model-sonnet"])
		}
	})

	// Project filter: only tokens from projectIssueID should appear.
	t.Run("project-scoped returns only project model", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByModel(w, newRequest("GET", "/api/dashboard/usage/by-model?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []modelRow
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, r := range rows {
			if r.Model == "by-model-sonnet" {
				t.Errorf("project filter leaked: by-model-sonnet (other issue) should not appear")
			}
		}
		totals := map[string]int64{}
		for _, r := range rows {
			totals[r.Model] += r.InputTokens
		}
		if totals["by-model-gpt4"] < 2000 {
			t.Errorf("project scope gpt4: expected >=2000 tokens, got %d", totals["by-model-gpt4"])
		}
	})

	// Invalid project_id UUID must produce a 400.
	t.Run("invalid project_id rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByModel(w, newRequest("GET", "/api/dashboard/usage/by-model?project_id=not-a-uuid", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for malformed uuid, got %d", w.Code)
		}
	})
}

// TestGetDashboardRuntimeRunTime covers the new per-runtime aggregation endpoint
// that powers the "Runtime" scope of the leaderboard: run-time seconds and task
// counts are grouped by runtime_id, the project filter excludes out-of-scope
// tasks, and an invalid project_id UUID returns 400.
func TestGetDashboardRuntimeRunTime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'runtime-runtime scope test') RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	mkIssue := func(withProject bool) string {
		var id string
		var pid any
		if withProject {
			pid = projectID
		}
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
			VALUES (
				$1, 'runtime-runtime test issue', $2, 'member', $3,
				(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
			)
			RETURNING id
		`, testWorkspaceID, testUserID, pid).Scan(&id); err != nil {
			t.Fatalf("insert issue: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}
	projectIssueID := mkIssue(true)
	otherIssueID := mkIssue(false)

	now := time.Now().UTC()
	mkTask := func(issueID string, durationSeconds int) {
		started := now.Add(-time.Duration(durationSeconds+5) * time.Minute)
		completed := started.Add(time.Duration(durationSeconds) * time.Second)
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
			VALUES ($1, $2, $3, 'completed', $4, $5, now())
			RETURNING id
		`, agentID, issueID, runtimeID, started, completed).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	}

	// Project task: 300 s run. Other task: 120 s run.
	mkTask(projectIssueID, 300)
	mkTask(otherIssueID, 120)

	type rtRow struct {
		RuntimeID    string `json:"runtime_id"`
		TotalSeconds int64  `json:"total_seconds"`
		TaskCount    int32  `json:"task_count"`
		FailedCount  int32  `json:"failed_count"`
	}

	t.Run("workspace-wide returns run time for our runtime", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardRuntimeRunTime(w, newRequest("GET", "/api/dashboard/runtime-runtime?days=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []rtRow
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var seconds int64
		var tasks int32
		for _, r := range rows {
			if r.RuntimeID == runtimeID {
				seconds += r.TotalSeconds
				tasks += r.TaskCount
			}
		}
		// Both tasks ran on this runtime; combined >=420 s and >=2 tasks.
		if seconds < 420 {
			t.Errorf("workspace-wide: expected >=420s total, got %d", seconds)
		}
		if tasks < 2 {
			t.Errorf("workspace-wide: expected >=2 tasks, got %d", tasks)
		}
	})

	// Project filter: only the 300 s project task should count.
	t.Run("project-scoped returns only project runtime", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardRuntimeRunTime(w, newRequest("GET", "/api/dashboard/runtime-runtime?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []rtRow
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var seconds int64
		var tasks int32
		for _, r := range rows {
			if r.RuntimeID == runtimeID {
				seconds += r.TotalSeconds
				tasks += r.TaskCount
			}
		}
		// Project filter must include the 300 s task and exclude the 120 s one.
		if seconds < 300 {
			t.Errorf("project scope: expected >=300s, got %d", seconds)
		}
		// Combined seconds must be < 420 to prove the non-project task was excluded.
		if seconds >= 420 {
			t.Errorf("project filter leaked: expected <420s (other task excluded), got %d", seconds)
		}
		if tasks < 1 {
			t.Errorf("project scope: expected >=1 task, got %d", tasks)
		}
	})

	// Invalid project_id UUID must produce a 400.
	t.Run("invalid project_id rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardRuntimeRunTime(w, newRequest("GET", "/api/dashboard/runtime-runtime?project_id=not-a-uuid", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for malformed uuid, got %d", w.Code)
		}
	})
}
