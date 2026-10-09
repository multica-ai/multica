package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestGetDashboardModelRunTime covers the per-model run-time endpoint:
// duration is derived by joining task_usage with agent_task_queue on task_id,
// the project filter excludes out-of-scope tasks, and an invalid project_id
// is rejected with 400.
func TestGetDashboardModelRunTime(t *testing.T) {
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
		INSERT INTO project (workspace_id, title) VALUES ($1, 'model-runtime scope test') RETURNING id
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
				$1, 'model-runtime test issue', $2, 'member', $3,
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
	// mkTask creates a completed task with the given duration and a task_usage
	// row for the given model. The query joins on task_id, so the usage row
	// must exist for the run-time aggregation to pick it up.
	//
	// Completion is anchored a minute back rather than `now - (duration+5)
	// minutes`: these rollups take an EXACT N-day cutoff, so a fixture that
	// lands on the previous calendar day falls out of a days=1 window and the
	// assertions quietly start measuring the wrong thing.
	mkTask := func(issueID, model string, durationSeconds int) {
		completed := now.Add(-1 * time.Minute)
		started := completed.Add(-time.Duration(durationSeconds) * time.Second)
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
			VALUES ($1, 'claude', $2, 100, 50, now())
		`, taskID, model); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	}

	// project task uses model "mr-gpt4" (300 s); other task uses "mr-sonnet" (120 s).
	mkTask(projectIssueID, "mr-gpt4", 300)
	mkTask(otherIssueID, "mr-sonnet", 120)

	type modelRTRow struct {
		Model        string `json:"model"`
		TotalSeconds int64  `json:"total_seconds"`
		TaskCount    int32  `json:"task_count"`
	}

	// workspace-wide: both models must appear with their respective run times.
	t.Run("workspace-wide returns both models with run time", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardModelRunTime(w, newRequest("GET", "/api/dashboard/model-runtime?days=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []modelRTRow
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		seconds := map[string]int64{}
		counts := map[string]int32{}
		for _, r := range rows {
			seconds[r.Model] += r.TotalSeconds
			counts[r.Model] += r.TaskCount
		}
		if seconds["mr-gpt4"] < 300 {
			t.Errorf("workspace mr-gpt4: expected >=300s, got %d", seconds["mr-gpt4"])
		}
		if seconds["mr-sonnet"] < 120 {
			t.Errorf("workspace mr-sonnet: expected >=120s, got %d", seconds["mr-sonnet"])
		}
		if counts["mr-gpt4"] < 1 {
			t.Errorf("workspace mr-gpt4: expected >=1 task, got %d", counts["mr-gpt4"])
		}
	})

	// project-scoped: only the 300 s project task should appear; mr-sonnet must be absent.
	t.Run("project-scoped excludes other project model", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardModelRunTime(w, newRequest("GET", "/api/dashboard/model-runtime?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []modelRTRow
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, r := range rows {
			if r.Model == "mr-sonnet" {
				t.Errorf("project filter leaked: mr-sonnet (other issue) should not appear")
			}
		}
		seconds := map[string]int64{}
		for _, r := range rows {
			seconds[r.Model] += r.TotalSeconds
		}
		if seconds["mr-gpt4"] < 300 {
			t.Errorf("project scope mr-gpt4: expected >=300s, got %d", seconds["mr-gpt4"])
		}
	})

	// malformed project_id must be rejected with 400.
	t.Run("invalid project_id rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardModelRunTime(w, newRequest("GET", "/api/dashboard/model-runtime?project_id=not-a-uuid", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for malformed uuid, got %d", w.Code)
		}
	})

	// multi-provider rows for the same (task_id, model) must be collapsed by the
	// DISTINCT subquery so total_seconds equals the task's actual duration, not 2×.
	t.Run("multi-provider rows for same model deduplicated to task duration", func(t *testing.T) {
		const dedupModel = "dedup-test-model"
		const durationSeconds = 180

		var dedupIssueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
			VALUES (
				$1, 'dedup-test issue', $2, 'member',
				(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
			)
			RETURNING id
		`, testWorkspaceID, testUserID).Scan(&dedupIssueID); err != nil {
			t.Fatalf("insert dedup issue: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, dedupIssueID) })

		started := now.Add(-10 * time.Minute)
		completed := started.Add(time.Duration(durationSeconds) * time.Second)
		var dedupTaskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
			VALUES ($1, $2, $3, 'completed', $4, $5, now())
			RETURNING id
		`, agentID, dedupIssueID, runtimeID, started, completed).Scan(&dedupTaskID); err != nil {
			t.Fatalf("insert dedup task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, dedupTaskID) })

		// Two usage rows: same task + model, different providers (allowed by UNIQUE (task_id, provider, model)).
		for _, provider := range []string{"anthropic", "bedrock"} {
			if _, err := testPool.Exec(ctx, `
				INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
				VALUES ($1, $2, $3, 100, 50, now())
			`, dedupTaskID, provider, dedupModel); err != nil {
				t.Fatalf("insert task_usage (%s): %v", provider, err)
			}
		}

		w := httptest.NewRecorder()
		testHandler.GetDashboardModelRunTime(w, newRequest("GET", "/api/dashboard/model-runtime?days=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []modelRTRow
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var total int64
		var count int32
		for _, r := range rows {
			if r.Model == dedupModel {
				total += r.TotalSeconds
				count += r.TaskCount
			}
		}
		if total != durationSeconds {
			t.Errorf("dedup: expected total_seconds=%d for %q, got %d (multi-provider rows not deduplicated)", durationSeconds, dedupModel, total)
		}
		// The same collapse has to hold for the count. total_seconds alone can
		// look right while COUNT(DISTINCT) still regresses, and the Tasks
		// column is what reads this field.
		if count != 1 {
			t.Errorf("dedup: expected task_count=1 for %q, got %d (one task, two provider rows)", dedupModel, count)
		}
	})
}

// TestGetDashboardUsageByRuntime covers the per-(runtime_id, model) token
// aggregation endpoint: tokens are grouped by runtime + model, the project
// filter excludes out-of-scope tasks, and an invalid project_id returns 400.
func TestGetDashboardUsageByRuntime(t *testing.T) {
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
		INSERT INTO project (workspace_id, title) VALUES ($1, 'runtime-usage scope test') RETURNING id
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
				$1, 'runtime-usage test issue', $2, 'member', $3,
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
	// This endpoint reads `task_usage_hourly` (migration 101 keys it on
	// runtime_id), the same rollup the Agent and Model scopes use, so the
	// fixture seeds the rollup directly — same shortcut as
	// TestDashboardPerAgentRollupsUseExactWindow. The terminal task still has
	// to exist because it carries the project_id the scoping assertion needs.
	mkTask := func(issueID, model string, inputTokens int64) {
		completed := now.Add(-1 * time.Minute)
		started := completed.Add(-5 * time.Minute)
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
			VALUES ($1, $2, $3, 'completed', $4, $5, now())
			RETURNING id
		`, agentID, issueID, runtimeID, started, completed).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

		var projectID any
		if err := testPool.QueryRow(ctx, `SELECT project_id FROM issue WHERE id = $1`, issueID).Scan(&projectID); err != nil {
			t.Fatalf("read issue project: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage_hourly (
				bucket_hour, workspace_id, runtime_id, agent_id, project_id,
				provider, model,
				input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
				event_count, task_count
			)
			VALUES (
				date_trunc('hour', $1::timestamptz), $2, $3, $4, $5,
				'claude', $6,
				$7, 0, 0, 0,
				1, 1
			)
			ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_key DO UPDATE
			SET input_tokens = task_usage_hourly.input_tokens + EXCLUDED.input_tokens,
			    event_count = task_usage_hourly.event_count + EXCLUDED.event_count
		`, completed, testWorkspaceID, runtimeID, agentID, projectID, model, inputTokens); err != nil {
			t.Fatalf("insert task_usage_hourly: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM task_usage_hourly WHERE provider = 'claude' AND model = $1`, model)
		})
	}

	// project task: 3000 input tokens on model "ru-gpt4"
	// other task: 1500 input tokens on model "ru-sonnet"
	mkTask(projectIssueID, "ru-gpt4", 3000)
	mkTask(otherIssueID, "ru-sonnet", 1500)

	type usageByRuntimeRow struct {
		RuntimeID   string `json:"runtime_id"`
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}

	// workspace-wide: both (runtimeID, model) rows must appear.
	t.Run("workspace-wide returns tokens for both models", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByRuntime(w, newRequest("GET", "/api/dashboard/usage/by-runtime?days=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []usageByRuntimeRow
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		tokens := map[string]int64{}
		for _, r := range rows {
			if r.RuntimeID == runtimeID {
				tokens[r.Model] += r.InputTokens
			}
		}
		if tokens["ru-gpt4"] < 3000 {
			t.Errorf("workspace ru-gpt4: expected >=3000 tokens, got %d", tokens["ru-gpt4"])
		}
		if tokens["ru-sonnet"] < 1500 {
			t.Errorf("workspace ru-sonnet: expected >=1500 tokens, got %d", tokens["ru-sonnet"])
		}
	})

	// project-scoped: only the project task's model should appear.
	t.Run("project-scoped returns only project runtime tokens", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByRuntime(w, newRequest("GET", "/api/dashboard/usage/by-runtime?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []usageByRuntimeRow
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, r := range rows {
			if r.RuntimeID == runtimeID && r.Model == "ru-sonnet" {
				t.Errorf("project filter leaked: ru-sonnet (other issue) should not appear")
			}
		}
		tokens := map[string]int64{}
		for _, r := range rows {
			if r.RuntimeID == runtimeID {
				tokens[r.Model] += r.InputTokens
			}
		}
		if tokens["ru-gpt4"] < 3000 {
			t.Errorf("project scope ru-gpt4: expected >=3000 tokens, got %d", tokens["ru-gpt4"])
		}
	})

	// malformed project_id must be rejected with 400.
	t.Run("invalid project_id rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByRuntime(w, newRequest("GET", "/api/dashboard/usage/by-runtime?project_id=not-a-uuid", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for malformed uuid, got %d", w.Code)
		}
	})
}
