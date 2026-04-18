package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWorkspaceUsage_BucketsByUsageTime mirrors the runtime usage test for
// the workspace-level aggregations: a task that queues one calendar day and
// reports usage the next must attribute to the day tokens were produced, and
// `?days=N` must cover the full earliest day, not a rolling window starting
// at "now minus N days".
func TestWorkspaceUsage_BucketsByUsageTime(t *testing.T) {
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

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type)
		VALUES ($1, 'workspace usage test', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterdayLate := today.Add(-2 * time.Minute)
	todayEarly := today.Add(5 * time.Minute)
	yesterdayMorning := today.Add(-19 * time.Hour)

	insertTaskWithUsage := func(enqueueAt, usageAt time.Time, inputTokens int64) {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, created_at)
			VALUES ($1, $2, $3, 'completed', $4)
			RETURNING id
		`, agentID, issueID, runtimeID, enqueueAt).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, created_at)
			VALUES ($1, 'claude', 'claude-3-5-sonnet', $2, 0, $3)
		`, taskID, inputTokens, usageAt); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		})
	}

	insertTaskWithUsage(yesterdayLate, todayEarly, 1000)         // cross-midnight
	insertTaskWithUsage(yesterdayMorning, yesterdayMorning, 2000) // full-day yesterday

	// /api/usage/daily — daily breakdown.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/usage/daily?days=1", nil)
	testHandler.GetWorkspaceUsageByDay(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetWorkspaceUsageByDay: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	type dailyRow struct {
		Date             string `json:"date"`
		Model            string `json:"model"`
		TotalInputTokens int64  `json:"total_input_tokens"`
	}
	var dailyResp []dailyRow
	if err := json.NewDecoder(w.Body).Decode(&dailyResp); err != nil {
		t.Fatalf("decode daily: %v", err)
	}
	byDate := make(map[string]int64)
	for _, r := range dailyResp {
		byDate[r.Date] += r.TotalInputTokens
	}
	todayKey := today.Format("2006-01-02")
	yesterdayKey := today.Add(-24 * time.Hour).Format("2006-01-02")
	if byDate[todayKey] < 1000 {
		t.Errorf("daily: today bucket expected >=1000 input tokens (cross-midnight task), got %d (full map: %v)", byDate[todayKey], byDate)
	}
	if byDate[yesterdayKey] < 2000 {
		t.Errorf("daily: yesterday bucket expected >=2000 input tokens (yesterday morning task), got %d (full map: %v)", byDate[yesterdayKey], byDate)
	}

	// /api/usage/summary — aggregate across the full window. Both rows must
	// be included; if the cutoff were a rolling window, yesterday morning's
	// 2000 would be missing depending on time of day.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/usage/summary?days=1", nil)
	testHandler.GetWorkspaceUsageSummary(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetWorkspaceUsageSummary: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	type summaryRow struct {
		Model            string `json:"model"`
		TotalInputTokens int64  `json:"total_input_tokens"`
		TaskCount        int32  `json:"task_count"`
	}
	var summaryResp []summaryRow
	if err := json.NewDecoder(w.Body).Decode(&summaryResp); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	var totalInput int64
	var totalTasks int32
	for _, r := range summaryResp {
		if r.Model == "claude-3-5-sonnet" {
			totalInput += r.TotalInputTokens
			totalTasks += r.TaskCount
		}
	}
	if totalInput < 3000 {
		t.Errorf("summary: claude-3-5-sonnet input tokens expected >=3000 (1000 + 2000), got %d (full resp: %v)", totalInput, summaryResp)
	}
	if totalTasks < 2 {
		t.Errorf("summary: claude-3-5-sonnet task_count expected >=2, got %d (full resp: %v)", totalTasks, summaryResp)
	}
}
