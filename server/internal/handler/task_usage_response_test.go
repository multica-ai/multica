package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListTasksByIssueHydratesUsage guards the per-run token figure on the
// execution log: the sidebar row, its header total, and the usage dialog all
// read `usage` off this endpoint, so a task that recorded usage must come back
// carrying it — at the (provider, model) grain, because cost is priced
// per-model on the client and a collapsed row cannot be priced at all.
//
// It also pins the distinction the whole feature rests on: a run with no
// recorded usage returns NO usage field, not a zeroed one. The UI renders the
// former as an em dash and the latter as "0 tokens / $0.00", and a run from
// before usage reporting was not free.
func TestListTasksByIssueHydratesUsage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "UsageListAgent", []byte("[]"))

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'usage-list-issue', 'todo', 'medium', $2, 'member', 92778, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	newTask := func(status string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id)
			VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 0, $3)
			RETURNING id
		`, agentID, status, issueID).Scan(&id); err != nil {
			t.Fatalf("create task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, id) })
		return id
	}

	// A run that spilled across two models — the case the per-model grain
	// exists for.
	pricedTask := newTask("completed")
	// A run that predates usage reporting.
	unpricedTask := newTask("completed")

	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd_ticks)
		VALUES ($1, 'anthropic', 'claude-opus-5',  96000, 34000, 712000, 50000, NULL),
		       ($1, 'openai',    'gpt-5.6-terra',  31000, 12000, 158000, 11000, 3310000000)
	`, pricedTask); err != nil {
		t.Fatalf("insert task usage: %v", err)
	}

	req := newRequest("GET", "/api/issues/"+issueID+"/task-runs", nil)
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.ListTasksByIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []AgentTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode task list: %v", err)
	}

	byID := make(map[string]AgentTaskResponse, len(resp))
	for _, task := range resp {
		byID[task.ID] = task
	}

	priced, ok := byID[pricedTask]
	if !ok {
		t.Fatalf("priced task %s missing from response: %s", pricedTask, w.Body.String())
	}
	if len(priced.Usage) != 2 {
		t.Fatalf("usage rows = %d, want 2 (one per model): %+v", len(priced.Usage), priced.Usage)
	}

	byModel := make(map[string]TaskUsageData, len(priced.Usage))
	for _, u := range priced.Usage {
		byModel[u.Model] = u
	}

	opus, ok := byModel["claude-opus-5"]
	if !ok {
		t.Fatalf("claude-opus-5 row missing: %+v", priced.Usage)
	}
	if opus.Provider != "anthropic" {
		t.Errorf("opus provider = %q, want anthropic", opus.Provider)
	}
	if opus.InputTokens != 96000 || opus.OutputTokens != 34000 ||
		opus.CacheReadTokens != 712000 || opus.CacheWriteTokens != 50000 {
		t.Errorf("opus token counts = %+v, want 96000/34000/712000/50000", opus)
	}
	// The provider reported no cost for this row, so the client must estimate
	// it — a 0 here would tell the client the provider said "free".
	if opus.CostUsdTicks != nil {
		t.Errorf("opus cost_usd_ticks = %d, want nil (provider reported none)", *opus.CostUsdTicks)
	}

	terra, ok := byModel["gpt-5.6-terra"]
	if !ok {
		t.Fatalf("gpt-5.6-terra row missing: %+v", priced.Usage)
	}
	if terra.CostUsdTicks == nil || *terra.CostUsdTicks != 3310000000 {
		t.Errorf("terra cost_usd_ticks = %v, want 3310000000", terra.CostUsdTicks)
	}

	unpriced, ok := byID[unpricedTask]
	if !ok {
		t.Fatalf("unpriced task %s missing from response", unpricedTask)
	}
	if len(unpriced.Usage) != 0 {
		t.Errorf("task with no recorded usage carries %d usage rows, want none: %+v",
			len(unpriced.Usage), unpriced.Usage)
	}
	// omitempty must keep the key off the wire entirely, so the client sees
	// `undefined` rather than an empty array it might read as "hydrated, zero".
	var raw []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw task list: %v", err)
	}
	for _, task := range raw {
		if task["id"] != unpricedTask {
			continue
		}
		if _, present := task["usage"]; present {
			t.Errorf("unpriced task serialises a `usage` key; want it omitted: %v", task["usage"])
		}
	}
}
