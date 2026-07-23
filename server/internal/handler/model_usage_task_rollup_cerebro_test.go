package handler

import (
	"context"
	"testing"
)

// CEREBRO-PATCH(model-usage-task-rollup-db-test): FIR-3337 proves that the
// compatibility seam uses the latest cumulative event and never double-counts
// the legacy shadow row, while historical tasks still remain visible.
func TestModelUsageTaskRollupCanonicalAndHistoricalTasks(t *testing.T) {
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
		VALUES ($1, 'model usage rollup test', 'todo', 'none', 'member', $2, 99011, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	createTask := func() string {
		t.Helper()
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
			VALUES ($1, $2, $3, 'running', 0)
			RETURNING id
		`, agentID, testRuntimeID, issueID).Scan(&taskID); err != nil {
			t.Fatalf("create task: %v", err)
		}
		return taskID
	}
	insertLegacy := func(taskID string, input, output int64) {
		t.Helper()
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (
				task_id, provider, model, input_tokens, output_tokens,
				cache_read_tokens, cache_write_tokens, cost_cents
			) VALUES ($1, 'openai', 'gpt-rollup', $2, $3, 4, 5, 6)
		`, taskID, input, output); err != nil {
			t.Fatalf("insert legacy usage: %v", err)
		}
	}

	canonicalTaskID := createTask()
	insertLegacy(canonicalTaskID, 999, 999)
	if err := insertScopeReconciliationEvent(ctx, canonicalTaskID, "rollup-initial", 1,
		"openai", "gpt-rollup", "cumulative", 30, 4, 1, 2, 3, 4, 40); err != nil {
		t.Fatalf("insert initial event: %v", err)
	}
	if err := insertScopeReconciliationEvent(ctx, canonicalTaskID, "rollup-final", 2,
		"openai", "gpt-rollup", "cumulative", 50, 7, 2, 8, 9, 10, 60); err != nil {
		t.Fatalf("insert final event: %v", err)
	}

	historicalTaskID := createTask()
	insertLegacy(historicalTaskID, 70, 8)

	mixedTaskID := createTask()
	insertLegacy(mixedTaskID, 80, 9)
	if err := insertScopeReconciliationEvent(ctx, mixedTaskID, "rollup-mixed-canonical", 1,
		"anthropic", "claude-rollup", "delta", 40, 5, 0, 3, 0, 7, 50); err != nil {
		t.Fatalf("insert mixed canonical event: %v", err)
	}

	assertRollup := func(taskID, provider, model string, wantInput, wantOutput, wantCacheRead, wantCost int64) {
		t.Helper()
		var input, output, cacheRead, cost int64
		if err := testPool.QueryRow(ctx, `
			SELECT input_tokens, output_tokens, cache_read_tokens, cost_cents
			FROM model_usage_task_rollup
			WHERE task_id = $1 AND provider = $2 AND model = $3
		`, taskID, provider, model).Scan(&input, &output, &cacheRead, &cost); err != nil {
			t.Fatalf("read rollup: %v", err)
		}
		if input != wantInput || output != wantOutput || cacheRead != wantCacheRead || cost != wantCost {
			t.Fatalf("rollup = input %d output %d cache %d cost %d, want %d/%d/%d/%d",
				input, output, cacheRead, cost, wantInput, wantOutput, wantCacheRead, wantCost)
		}
	}

	assertRollup(canonicalTaskID, "openai", "gpt-rollup", 50, 9, 8, 10)
	assertRollup(historicalTaskID, "openai", "gpt-rollup", 70, 8, 4, 6)
	assertRollup(mixedTaskID, "anthropic", "claude-rollup", 40, 5, 3, 7)
	assertRollup(mixedTaskID, "openai", "gpt-rollup", 80, 9, 4, 6)
}
