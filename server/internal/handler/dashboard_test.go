package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestDashboardEndpoints covers the workspace-dashboard rollups:
//   - daily token usage with and without project filter
//   - per-agent token usage with and without project filter
//   - per-agent run time
//
// Asserts that (1) tasks belonging to a project show up under the workspace
// view, (2) the project filter excludes tasks tied to issues without a
// matching project_id, and (3) run-time aggregation accumulates the
// completed_at − started_at delta correctly.
func TestDashboardEndpoints(t *testing.T) {
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

	// Two issues: one bound to a project, one not.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'dashboard test project')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	// issue.number is `UNIQUE (workspace_id, number)` (migration 020) and
	// defaults to 0. Two inserts into the same workspace would collide on the
	// default; allocate `MAX(number) + 1` per row to stay sequential and
	// avoid stepping on rows other tests have left behind in the shared
	// fixture workspace.
	mkIssue := func(withProject bool) string {
		var id string
		var pid any
		if withProject {
			pid = projectID
		}
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
			VALUES (
				$1, 'dashboard test', $2, 'member', $3,
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
	started := now.Add(-30 * time.Minute)
	completed := started.Add(10 * time.Minute) // 600s run

	mkTaskWithUsage := func(issueID string, status string, tokens int64) {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			RETURNING id
		`, agentID, issueID, runtimeID, status, started, completed).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
			VALUES ($1, 'claude', 'claude-3-5-sonnet', $2, 0, now())
		`, taskID, tokens); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	}

	mkTaskWithUsage(projectIssueID, "completed", 1000)
	mkTaskWithUsage(otherIssueID, "completed", 500)

	type dailyRow struct {
		Date        string `json:"date"`
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}
	type byAgentRow struct {
		AgentID     string `json:"agent_id"`
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
	}
	type runtimeRow struct {
		AgentID      string `json:"agent_id"`
		TotalSeconds int64  `json:"total_seconds"`
		TaskCount    int32  `json:"task_count"`
	}

	// daily — workspace-wide
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("daily ws: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []dailyRow
		_ = json.NewDecoder(w.Body).Decode(&rows)
		var total int64
		for _, r := range rows {
			if r.Model == "claude-3-5-sonnet" {
				total += r.InputTokens
			}
		}
		if total < 1500 {
			t.Errorf("daily ws: expected >=1500 tokens (1000+500), got %d", total)
		}
	}

	// daily — project-scoped
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("daily project: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []dailyRow
		_ = json.NewDecoder(w.Body).Decode(&rows)
		var total int64
		for _, r := range rows {
			if r.Model == "claude-3-5-sonnet" {
				total += r.InputTokens
			}
		}
		// Project filter must exclude the 500-token "other" issue. Token total
		// for this project must be >= 1000 (our task) and < 1500 (would only
		// reach 1500 if filter leaked).
		if total < 1000 {
			t.Errorf("daily project: expected >=1000 tokens, got %d", total)
		}
		if total >= 1500 {
			t.Errorf("daily project: filter leaked — expected <1500 tokens, got %d", total)
		}
	}

	// by-agent — project-scoped
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByAgent(w, newRequest("GET", "/api/dashboard/usage/by-agent?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("by-agent project: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []byAgentRow
		_ = json.NewDecoder(w.Body).Decode(&rows)
		found := false
		for _, r := range rows {
			if r.AgentID == agentID && r.InputTokens >= 1000 {
				found = true
			}
		}
		if !found {
			t.Errorf("by-agent project: expected agent %s with >=1000 tokens; got %v", agentID, rows)
		}
	}

	// agent-runtime — project-scoped
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardAgentRunTime(w, newRequest("GET", "/api/dashboard/agent-runtime?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("agent-runtime: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []runtimeRow
		_ = json.NewDecoder(w.Body).Decode(&rows)
		var seconds int64
		var tasks int32
		for _, r := range rows {
			if r.AgentID == agentID {
				seconds += r.TotalSeconds
				tasks += r.TaskCount
			}
		}
		if tasks < 1 {
			t.Errorf("agent-runtime: expected >=1 task for agent, got %d", tasks)
		}
		if seconds < 600 {
			t.Errorf("agent-runtime: expected >=600s (one 10-minute run), got %d", seconds)
		}
	}

	// agent-runtime — invalid project_id rejected
	{
		w := httptest.NewRecorder()
		testHandler.GetDashboardAgentRunTime(w, newRequest("GET", "/api/dashboard/agent-runtime?project_id=not-a-uuid", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("agent-runtime: expected 400 for invalid uuid, got %d", w.Code)
		}
	}

	// Rollup path — run the dashboard window function, flip the feature
	// flag, and verify daily + by-agent reads come back with the same
	// project-filtered totals. The raw path above already passed, so this
	// validates that the rollup table mirrors the raw aggregation
	// (modulo project_id snapshot semantics, which match here since
	// nothing has changed since the rows were created).
	{
		// rollup the full window in one shot; same idempotent primitive
		// the cron path uses.
		if _, err := testPool.Exec(ctx, `
			SELECT rollup_task_usage_dashboard_daily_window('1970-01-01'::timestamptz, now() + interval '1 hour')
		`); err != nil {
			t.Fatalf("rollup window: %v", err)
		}
		origRollup := testHandler.cfg.UseDailyRollupForDashboard
		testHandler.cfg.UseDailyRollupForDashboard = true
		t.Cleanup(func() { testHandler.cfg.UseDailyRollupForDashboard = origRollup })

		// daily — project-scoped through rollup
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=1&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("rollup daily: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var dRows []dailyRow
		_ = json.NewDecoder(w.Body).Decode(&dRows)
		var dTotal int64
		for _, r := range dRows {
			if r.Model == "claude-3-5-sonnet" {
				dTotal += r.InputTokens
			}
		}
		if dTotal < 1000 {
			t.Errorf("rollup daily project: expected >=1000 tokens, got %d", dTotal)
		}
		if dTotal >= 1500 {
			t.Errorf("rollup daily project: filter leaked — expected <1500, got %d", dTotal)
		}

		// by-agent — workspace-wide through rollup
		w = httptest.NewRecorder()
		testHandler.GetDashboardUsageByAgent(w, newRequest("GET", "/api/dashboard/usage/by-agent?days=1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("rollup by-agent: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var aRows []byAgentRow
		_ = json.NewDecoder(w.Body).Decode(&aRows)
		var aTotal int64
		for _, r := range aRows {
			if r.AgentID == agentID && r.Model == "claude-3-5-sonnet" {
				aTotal += r.InputTokens
			}
		}
		if aTotal < 1500 {
			t.Errorf("rollup by-agent: expected >=1500 tokens across workspace, got %d", aTotal)
		}
	}
}

// TestDashboardRollupReattributesOnProjectChange verifies the trigger that
// fires on `UPDATE issue SET project_id` enqueues both old + new project
// buckets so the next rollup tick re-attributes the affected tokens.
// Uses the rollup window function directly to drain the dirty queue,
// then asserts the rollup table reflects the new project_id.
func TestDashboardRollupReattributesOnProjectChange(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	mkProject := func(name string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
		`, testWorkspaceID, name).Scan(&id); err != nil {
			t.Fatalf("create project: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, id) })
		return id
	}
	projectA := mkProject("dashboard reattr A")
	projectB := mkProject("dashboard reattr B")

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
		VALUES ($1, 'reattr issue', $2, 'member', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID, projectA).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
		VALUES ($1, $2, $3, 'completed', now()) RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'claude', 'claude-3-5-sonnet', 7777, 0, now())
	`, taskID); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}

	// First rollup pass: tokens attributed to project A.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_dashboard_daily_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup A: %v", err)
	}
	var aTokens int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_dashboard_daily
		WHERE workspace_id = $1 AND project_id = $2 AND agent_id = $3
	`, testWorkspaceID, projectA, agentID).Scan(&aTokens); err != nil {
		t.Fatalf("read A rollup: %v", err)
	}
	if aTokens < 7777 {
		t.Fatalf("project A: expected >=7777 tokens after first rollup, got %d", aTokens)
	}

	// Move the issue to project B. Trigger enqueues both A and B buckets.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectB, issueID); err != nil {
		t.Fatalf("reassign project: %v", err)
	}
	// Second rollup pass: A bucket drops to zero (deleted_empty), B
	// bucket gets the tokens.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_dashboard_daily_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup B: %v", err)
	}

	var bTokens, aTokensAfter int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_dashboard_daily
		WHERE workspace_id = $1 AND project_id = $2 AND agent_id = $3
	`, testWorkspaceID, projectB, agentID).Scan(&bTokens); err != nil {
		t.Fatalf("read B rollup: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_dashboard_daily
		WHERE workspace_id = $1 AND project_id = $2 AND agent_id = $3
	`, testWorkspaceID, projectA, agentID).Scan(&aTokensAfter); err != nil {
		t.Fatalf("read A rollup after move: %v", err)
	}
	if bTokens < 7777 {
		t.Errorf("project B: expected >=7777 tokens after reassign + rollup, got %d", bTokens)
	}
	if aTokensAfter != 0 {
		t.Errorf("project A: expected 0 tokens after reassign + rollup, got %d", aTokensAfter)
	}
}

// TestDashboardRollupClearsOnIssueDelete verifies that deleting an issue
// (which cascades to its tasks and task_usage rows) also clears the
// dashboard rollup row attributed to that issue's project. The
// `issue BEFORE DELETE` trigger has to fire ahead of the cascade so the
// dirty queue captures the original project_id while the issue row is
// still readable.
func TestDashboardRollupClearsOnIssueDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'dashboard cascade test') RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
		VALUES ($1, 'cascade issue', $2, 'member', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	// No t.Cleanup deleting the issue — that's what the test exercises.

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
		VALUES ($1, $2, $3, 'completed', now()) RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	// Don't bother cleaning up taskID either; cascade will take it.

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'claude', 'claude-3-5-sonnet', 4242, 0, now())
	`, taskID); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}

	// First rollup: project bucket exists with 4242 tokens.
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_dashboard_daily_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup before delete: %v", err)
	}
	var before int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_dashboard_daily
		WHERE workspace_id = $1 AND project_id = $2
	`, testWorkspaceID, projectID).Scan(&before); err != nil {
		t.Fatalf("read before: %v", err)
	}
	if before < 4242 {
		t.Fatalf("project bucket: expected >=4242 tokens before delete, got %d", before)
	}

	// Delete the issue. Cascade removes atq + task_usage. The issue
	// BEFORE DELETE trigger should have enqueued the project bucket
	// before the cascade started.
	if _, err := testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
		t.Fatalf("delete issue: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_dashboard_daily_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup after delete: %v", err)
	}
	var after int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_dashboard_daily
		WHERE workspace_id = $1 AND project_id = $2
	`, testWorkspaceID, projectID).Scan(&after); err != nil {
		t.Fatalf("read after: %v", err)
	}
	if after != 0 {
		t.Errorf("project bucket: expected 0 tokens after issue delete, got %d", after)
	}
}

// TestDashboardRollupReattributesOnLinkTaskToIssue verifies that
// `LinkTaskToIssue` (which UPDATEs `agent_task_queue.issue_id` from NULL
// to a real issue id) re-attributes existing rollup rows from the
// no-project bucket to the linked issue's project bucket. Mirrors the
// quick-create flow in `service.task.LinkTaskToIssue`.
func TestDashboardRollupReattributesOnLinkTaskToIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	// Quick-create task: issue_id is NULL at creation time.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, context, created_at)
		VALUES ($1, NULL, $2, 'completed', '{}'::jsonb, now()) RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert quick-create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
		VALUES ($1, 'claude', 'claude-3-5-sonnet', 1234, 0, now())
	`, taskID); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}

	// First rollup: tokens attributed to the no-project bucket (NULL).
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_dashboard_daily_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup pre-link: %v", err)
	}
	var nullBefore int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_dashboard_daily
		WHERE workspace_id = $1 AND project_id IS NULL AND agent_id = $2
	`, testWorkspaceID, agentID).Scan(&nullBefore); err != nil {
		t.Fatalf("read NULL bucket pre-link: %v", err)
	}
	if nullBefore < 1234 {
		t.Fatalf("NULL bucket: expected >=1234 tokens pre-link, got %d", nullBefore)
	}

	// Create a project + issue, then run the same UPDATE LinkTaskToIssue
	// uses. The atq trigger should enqueue OLD (NULL project) AND NEW
	// (the project's id) so the next rollup tick zeroes the NULL bucket
	// and populates the project bucket.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'dashboard link test') RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
		VALUES ($1, 'link test issue', $2, 'member', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// Mirror LinkTaskToIssue's UPDATE shape.
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue SET issue_id = $1 WHERE id = $2 AND issue_id IS NULL
	`, issueID, taskID); err != nil {
		t.Fatalf("link task to issue: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_dashboard_daily_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup post-link: %v", err)
	}

	var projectAfter, nullAfter int64
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_dashboard_daily
		WHERE workspace_id = $1 AND project_id = $2 AND agent_id = $3
	`, testWorkspaceID, projectID, agentID).Scan(&projectAfter); err != nil {
		t.Fatalf("read project bucket post-link: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0) FROM task_usage_dashboard_daily
		WHERE workspace_id = $1 AND project_id IS NULL AND agent_id = $2
	`, testWorkspaceID, agentID).Scan(&nullAfter); err != nil {
		t.Fatalf("read NULL bucket post-link: %v", err)
	}
	if projectAfter < 1234 {
		t.Errorf("project bucket: expected >=1234 tokens after link, got %d", projectAfter)
	}
	if nullAfter != 0 {
		t.Errorf("NULL bucket: expected 0 tokens after link, got %d", nullAfter)
	}
}
