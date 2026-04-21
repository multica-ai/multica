package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestProjectUsage_AggregatesTokensAcrossIssues creates two issues in a project,
// each with a task that reports token usage, and verifies the project-level
// usage endpoint sums tokens across both issues and splits daily buckets.
func TestProjectUsage_AggregatesTokensAcrossIssues(t *testing.T) {
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
		INSERT INTO project (workspace_id, title, priority)
		VALUES ($1, 'project usage test', 'none')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
	})

	insertIssueTaskUsage := func(usageAt time.Time, inputTokens, outputTokens int64) string {
		var issueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
			VALUES ($1, 'project usage issue', $2, 'member', $3,
				COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
			RETURNING id
		`, testWorkspaceID, testUserID, projectID).Scan(&issueID); err != nil {
			t.Fatalf("create issue: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		})

		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
			VALUES ($1, $2, $3, 'completed', $4)
			RETURNING id
		`, agentID, issueID, runtimeID, usageAt).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
			VALUES ($1, 'claude', 'claude-3-5-sonnet', $2, $3, $4)
		`, taskID, inputTokens, outputTokens, usageAt); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		})
		return issueID
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
	yesterday := today.Add(-20 * time.Hour)

	insertIssueTaskUsage(today, 1000, 200)
	insertIssueTaskUsage(yesterday, 3000, 400)

	// Happy path.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+projectID+"/usage?days=2", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.GetProjectUsage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProjectUsage: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Summary []struct {
			Model             string `json:"model"`
			TotalInputTokens  int64  `json:"total_input_tokens"`
			TotalOutputTokens int64  `json:"total_output_tokens"`
			TaskCount         int32  `json:"task_count"`
		} `json:"summary"`
		ByDay []struct {
			Date             string `json:"date"`
			Model            string `json:"model"`
			TotalInputTokens int64  `json:"total_input_tokens"`
		} `json:"by_day"`
		Total struct {
			TotalInputTokens  int64 `json:"total_input_tokens"`
			TotalOutputTokens int64 `json:"total_output_tokens"`
			TaskCount         int32 `json:"task_count"`
		} `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Total.TotalInputTokens < 4000 {
		t.Errorf("total input expected >=4000, got %d", resp.Total.TotalInputTokens)
	}
	if resp.Total.TotalOutputTokens < 600 {
		t.Errorf("total output expected >=600, got %d", resp.Total.TotalOutputTokens)
	}
	if resp.Total.TaskCount < 2 {
		t.Errorf("total task_count expected >=2, got %d", resp.Total.TaskCount)
	}

	var summaryInput int64
	for _, s := range resp.Summary {
		if s.Model == "claude-3-5-sonnet" {
			summaryInput += s.TotalInputTokens
		}
	}
	if summaryInput < 4000 {
		t.Errorf("summary input expected >=4000, got %d", summaryInput)
	}

	byDate := make(map[string]int64)
	for _, r := range resp.ByDay {
		byDate[r.Date] += r.TotalInputTokens
	}
	todayKey := today.Format("2006-01-02")
	yesterdayKey := yesterday.Format("2006-01-02")
	if byDate[todayKey] < 1000 {
		t.Errorf("by_day today bucket expected >=1000, got %d (map: %v)", byDate[todayKey], byDate)
	}
	if byDate[yesterdayKey] < 3000 {
		t.Errorf("by_day yesterday bucket expected >=3000, got %d (map: %v)", byDate[yesterdayKey], byDate)
	}

	// Cross-workspace isolation: another workspace must not see this project.
	var otherWs string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug) VALUES ('other ws usage', 'other-ws-usage-'||gen_random_uuid()::text) RETURNING id
	`).Scan(&otherWs); err != nil {
		t.Fatalf("create other ws: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, otherWs)
	})

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+projectID+"/usage", nil)
	req.Header.Set("X-Workspace-ID", otherWs)
	req = withURLParam(req, "id", projectID)
	testHandler.GetProjectUsage(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
