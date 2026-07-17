package handler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CEREBRO-PATCH(model-usage-event-scope-reconciliation-test): FIR-3337 locks
// session and issue shadow totals across independent agent/provider/model rows.
func TestModelUsageEventScopeReconciliationPreservesDimensions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var primaryAgentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`,
		testWorkspaceID,
	).Scan(&primaryAgentID); err != nil {
		t.Fatalf("fetch primary agent: %v", err)
	}

	var secondaryAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode)
		VALUES ($1, 'scope-reconciliation-agent-' || gen_random_uuid(), 'local')
		RETURNING id
	`, testWorkspaceID).Scan(&secondaryAgentID); err != nil {
		t.Fatalf("create secondary agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'scope reconciliation test', 'todo', 'none', 'member', $2, 99010, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	comment := func(parent *string, content string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, content, parent).Scan(&id); err != nil {
			t.Fatalf("create comment %q: %v", content, err)
		}
		return id
	}
	rootOne := comment(nil, "session one")
	replyOne := comment(&rootOne, "session one reply")
	nestedReplyOne := comment(&replyOne, "session one nested reply")
	rootTwo := comment(nil, "session two")

	type fixture struct {
		agentID, triggerID, provider, model, semantics  string
		input, output, reasoning, cacheRead, cacheWrite int64
		cost, contextTokens                             int64
	}
	fixtures := []fixture{
		{primaryAgentID, rootOne, "anthropic", "claude-sonnet-4-6", "delta", 100, 20, 5, 30, 10, 7, 140},
		{secondaryAgentID, nestedReplyOne, "openai", "gpt-5.6", "cumulative", 300, 40, 10, 60, 20, 11, 390},
		{primaryAgentID, rootTwo, "openai", "gpt-5.6", "delta", 500, 80, 20, 100, 30, 17, 630},
	}

	for i, f := range fixtures {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority)
			VALUES ($1, $2, $3, $4, 'running', 0)
			RETURNING id
		`, f.agentID, testRuntimeID, issueID, f.triggerID).Scan(&taskID); err != nil {
			t.Fatalf("create task %d: %v", i, err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (
				task_id, provider, model, input_tokens, output_tokens,
				cache_read_tokens, cache_write_tokens, cost_cents
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, taskID, f.provider, f.model, f.input, f.output+f.reasoning, f.cacheRead, f.cacheWrite, f.cost); err != nil {
			t.Fatalf("create legacy task usage %d: %v", i, err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO cerebro_task_context_footprint (task_id, model, input_tokens, cache_read_tokens)
			VALUES ($1, $2, $3, $4)
		`, taskID, f.model, f.contextTokens, f.cacheRead); err != nil {
			t.Fatalf("create legacy context footprint %d: %v", i, err)
		}

		sequence := int64(1)
		if f.semantics == "cumulative" {
			if err := insertScopeReconciliationEvent(ctx, taskID, fmt.Sprintf("scope-%d-initial", i), sequence,
				f.provider, f.model, f.semantics, f.input/3, f.output/2, f.reasoning/2,
				f.cacheRead/2, f.cacheWrite/2, f.cost/2, f.contextTokens/2); err != nil {
				t.Fatalf("insert initial cumulative event %d: %v", i, err)
			}
			sequence++
		}
		if err := insertScopeReconciliationEvent(ctx, taskID, fmt.Sprintf("scope-%d-final", i), sequence,
			f.provider, f.model, f.semantics, f.input, f.output, f.reasoning,
			f.cacheRead, f.cacheWrite, f.cost, f.contextTokens); err != nil {
			t.Fatalf("insert final event %d: %v", i, err)
		}
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, secondaryAgentID)
	})

	sessionRows, err := testHandler.Queries.GetModelUsageEventSessionReconciliation(ctx,
		db.GetModelUsageEventSessionReconciliationParams{
			IssueID:              parseUUID(issueID),
			SessionRootCommentID: parseUUID(rootOne),
			IsFirst:              false,
		})
	if err != nil {
		t.Fatalf("session reconciliation: %v", err)
	}
	var sessionAssertions []scopeReconciliationAssertion
	for _, row := range sessionRows {
		sessionAssertions = append(sessionAssertions, scopeReconciliationAssertion{
			key:        uuidToString(row.AgentID) + "/" + row.Provider + "/" + row.Model,
			eventCount: row.EventCount, input: row.InputTokenDrift, output: row.OutputTokenDrift,
			cacheRead: row.CacheReadTokenDrift, cacheWrite: row.CacheWriteTokenDrift,
			cost: row.CostCentsDrift, context: row.ContextTokenDrift,
		})
	}
	assertScopeReconciliationRows(t, sessionAssertions, 3, []string{
		primaryAgentID + "/anthropic/claude-sonnet-4-6",
		secondaryAgentID + "/openai/gpt-5.6",
	})

	issueRows, err := testHandler.Queries.GetModelUsageEventIssueReconciliation(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatalf("issue reconciliation: %v", err)
	}
	var issueAssertions []scopeReconciliationAssertion
	for _, row := range issueRows {
		issueAssertions = append(issueAssertions, scopeReconciliationAssertion{
			key:        uuidToString(row.AgentID) + "/" + row.Provider + "/" + row.Model,
			eventCount: row.EventCount, input: row.InputTokenDrift, output: row.OutputTokenDrift,
			cacheRead: row.CacheReadTokenDrift, cacheWrite: row.CacheWriteTokenDrift,
			cost: row.CostCentsDrift, context: row.ContextTokenDrift,
		})
	}
	assertScopeReconciliationRows(t, issueAssertions, 4, []string{
		primaryAgentID + "/anthropic/claude-sonnet-4-6",
		secondaryAgentID + "/openai/gpt-5.6",
		primaryAgentID + "/openai/gpt-5.6",
	})
}

// CEREBRO-PATCH(model-usage-event-non-issue-scope-test): FIR-3337 protects
// canonical usage for chat and run-only Autopilot tasks that have no issue.
func TestModelUsageEventIngestionPreservesNonIssueScopes(t *testing.T) {
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

	var chatSessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'model usage chat scope')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&chatSessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	var chatTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority)
		VALUES ($1, $2, $3, 'running', 0)
		RETURNING id
	`, agentID, testRuntimeID, chatSessionID).Scan(&chatTaskID); err != nil {
		t.Fatalf("create chat task: %v", err)
	}
	if err := insertScopeReconciliationEvent(ctx, chatTaskID, "non-issue-chat", 1,
		"openai", "gpt-non-issue", "delta", 10, 2, 0, 1, 0, 3, 12); err != nil {
		t.Fatalf("insert chat usage event: %v", err)
	}

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id
		)
		VALUES ($1, 'model usage run-only scope', $2, 'run_only', 'member', $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	var autopilotRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'running')
		RETURNING id
	`, autopilotID).Scan(&autopilotRunID); err != nil {
		t.Fatalf("create autopilot run: %v", err)
	}
	var autopilotTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, autopilot_run_id, status, priority)
		VALUES ($1, $2, $3, 'running', 0)
		RETURNING id
	`, agentID, testRuntimeID, autopilotRunID).Scan(&autopilotTaskID); err != nil {
		t.Fatalf("create autopilot task: %v", err)
	}
	if err := insertScopeReconciliationEvent(ctx, autopilotTaskID, "non-issue-autopilot", 1,
		"anthropic", "claude-non-issue", "delta", 20, 4, 0, 2, 0, 5, 24); err != nil {
		t.Fatalf("insert autopilot usage event: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id IN ($1, $2)`, chatTaskID, autopilotTaskID)
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})

	assertScopedEvent := func(taskID, scopeColumn, scopeID string) {
		t.Helper()
		var count int
		query := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM model_usage_event
			WHERE task_id = $1
			  AND workspace_id = $2
			  AND issue_id IS NULL
			  AND %s = $3
		`, scopeColumn)
		if err := testPool.QueryRow(ctx, query, taskID, testWorkspaceID, scopeID).Scan(&count); err != nil {
			t.Fatalf("count %s usage event: %v", scopeColumn, err)
		}
		if count != 1 {
			t.Fatalf("%s scoped event count = %d, want 1", scopeColumn, count)
		}
	}
	assertScopedEvent(chatTaskID, "chat_session_id", chatSessionID)
	assertScopedEvent(autopilotTaskID, "autopilot_run_id", autopilotRunID)
}

func insertScopeReconciliationEvent(
	ctx context.Context,
	taskID, eventID string,
	sequence int64,
	provider, model, semantics string,
	input, output, reasoning, cacheRead, cacheWrite, cost, contextTokens int64,
) error {
	_, err := testHandler.Queries.InsertModelUsageEvent(ctx, db.InsertModelUsageEventParams{
		SchemaVersion:       "1",
		EventID:             eventID,
		ProviderSessionID:   "provider-session-" + taskID,
		CallID:              eventID,
		Sequence:            sequence,
		ObservedAt:          pgtype.Timestamptz{Time: time.Date(2026, 7, 16, 12, 0, int(sequence), 0, time.UTC), Valid: true},
		Provider:            provider,
		Model:               model,
		InputTokens:         input,
		OutputTokens:        output,
		ReasoningTokens:     reasoning,
		CacheReadTokens:     cacheRead,
		CacheWriteTokens:    cacheWrite,
		CostCents:           cost,
		ContextTokens:       contextTokens,
		ContextWindowTokens: 1_050_000,
		CompactionKind:      "",
		Source:              "final_response",
		Completeness:        "complete",
		CounterSemantics:    semantics,
		TaskID:              parseUUID(taskID),
	})
	return err
}

type scopeReconciliationAssertion struct {
	key                                  string
	eventCount                           int64
	input, output, cacheRead, cacheWrite int64
	cost, context                        int64
}

func assertScopeReconciliationRows(t *testing.T, rows []scopeReconciliationAssertion, wantEvents int, wantKeys []string) {
	t.Helper()
	if len(rows) != len(wantKeys) {
		t.Fatalf("reconciliation rows = %d, want %d", len(rows), len(wantKeys))
	}
	var events int64
	gotKeys := make(map[string]bool, len(rows))
	for _, row := range rows {
		gotKeys[row.key] = true
		events += row.eventCount
		if row.input != 0 || row.output != 0 || row.cacheRead != 0 || row.cacheWrite != 0 ||
			row.cost != 0 || row.context != 0 {
			t.Fatalf("scope reconciliation drifted: %+v", row)
		}
	}
	if events != int64(wantEvents) {
		t.Fatalf("event count = %d, want %d", events, wantEvents)
	}
	for _, key := range wantKeys {
		if !gotKeys[key] {
			t.Errorf("missing reconciliation dimension %q; got %v", key, gotKeys)
		}
	}
}
