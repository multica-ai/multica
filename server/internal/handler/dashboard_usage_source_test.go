package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Usage splits into manual runs and autopilot runs: a run-only autopilot's
// run on the task, and a create-issue autopilot through the issue it created.
// The fixture lives in its own project and every request filters on it.
func TestDashboardUsageBySource(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	pinDayWindowClock(t, time.Now())

	var runtimeID, agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID)
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	projectID := dbfx.Project(t, "usage by source")

	mkAutopilot := func(title, mode string) string {
		var id string
		dbfx.QueryRow(t, `
			INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
			VALUES ($1, $2, $3, $4, 'member', $5) RETURNING id
		`, testWorkspaceID, title, agentID, mode, testUserID).Scan(&id)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, id) })
		return id
	}
	runOnly := mkAutopilot("usage by source run-only", "run_only")
	createIssue := mkAutopilot("usage by source create-issue", "create_issue")
	var runID string
	dbfx.QueryRow(t, `
		INSERT INTO autopilot_run (autopilot_id, source, status) VALUES ($1, 'manual', 'completed') RETURNING id
	`, runOnly).Scan(&runID)

	mkIssue := func(title string, origin any) string {
		var originType any
		if origin != nil {
			originType = "autopilot"
		}
		var id string
		dbfx.QueryRow(t, `
			INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number, origin_type, origin_id)
			VALUES ($1, $2, $3, 'member', $4,
				(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1), $5, $6)
			RETURNING id
		`, testWorkspaceID, title, testUserID, projectID, originType, origin).Scan(&id)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}
	plainIssue := mkIssue("usage by source manual", nil)
	autopilotIssue := mkIssue("usage by source autopilot", createIssue)

	mkTask := func(issueID string, cols testutil.Cols, tokens int64) {
		cols["issue_id"] = issueID
		cols["runtime_id"] = runtimeID
		cols["status"] = "completed"
		taskID := dbfx.Task(t, agentID, cols)
		dbfx.Exec(t, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
			VALUES ($1, 'claude', 'usage-by-source-model', $2, 0, now())
		`, taskID, tokens)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	}
	mkTask(plainIssue, testutil.Cols{}, 300)
	mkTask(plainIssue, testutil.Cols{"autopilot_run_id": runID}, 500)
	mkTask(autopilotIssue, testutil.Cols{}, 200)

	dbfx.Exec(t, `SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')`)

	read := func() map[string]int64 {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageBySource(w, newRequest("GET",
			"/api/dashboard/usage/by-source?days=7&"+dashboardFixtureTZParam+"&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []DashboardUsageBySourceResponse
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{}
		for _, r := range rows {
			key := "manual"
			if r.AutopilotID != nil {
				key = *r.AutopilotID
			}
			out[key] += r.InputTokens
		}
		return out
	}

	if got := read(); got["manual"] != 300 || got[runOnly] != 500 || got[createIssue] != 200 {
		t.Fatalf("by source = %v, want manual 300, run-only 500, create-issue 200", got)
	}

	// Deleting the run-only autopilot removes its runs; their usage falls back
	// to manual instead of lingering under the deleted autopilot.
	dbfx.Exec(t, `DELETE FROM autopilot WHERE id = $1`, runOnly)
	dbfx.Exec(t, `SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')`)
	if got := read(); got["manual"] != 800 || got[runOnly] != 0 || got[createIssue] != 200 {
		t.Fatalf("after autopilot delete = %v, want manual 800, create-issue 200", got)
	}
}
