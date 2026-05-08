package handler

// CEREBRO-PATCH(chat-handler-chat-complete-test): cerebro modification of upstream file

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestCompleteChatTask_PersistsExactlyOneAssistantMessage is the regression
// guard for JEH-720: CompleteTask used to insert the assistant chat_message
// twice (once inside the runInTx block alongside MarkUserMessagesRespondedBefore,
// and once again after the tx returned), so every successful chat turn produced
// two ord-for-ord identical bubbles in the UI. The fix consolidated to a single
// in-tx insert; this test pins that invariant by counting rows with task_id
// pointing at the completed task.
//
// Also asserts the surviving insert preserves the two fields the post-tx
// version added on top of the upstream insert — UnescapeBackslashEscapes on
// content and ElapsedMs — so a naive "delete the post-tx block" fix can't
// silently regress the rendered output.
func TestCompleteChatTask_PersistsExactlyOneAssistantMessage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	sessionID := createChatSessionForCoalesceTest(t)

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT a.id, a.runtime_id FROM agent a
		 JOIN chat_session cs ON cs.agent_id = a.id
		 WHERE cs.id = $1`, sessionID,
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load agent/runtime: %v", err)
	}

	// Backdate both created_at AND started_at — computeChatElapsedMs measures
	// from created_at (queue + dispatch + run = total wait the user feels),
	// not started_at. Same window for both so the assertion is stable.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			chat_session_id, created_at, started_at
		)
		VALUES ($1, $2, NULL, 'running', 2, $3, now() - interval '3 seconds', now() - interval '2 seconds')
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert running chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	// Output contains a literal `\n` 4-char sequence — UnescapeBackslashEscapes
	// must decode it into a real newline before the row hits the DB.
	rawOutput := "Første linje.\\nAnden linje."
	result, err := json.Marshal(protocol.TaskCompletedPayload{
		TaskID: taskID,
		Output: rawOutput,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	completed, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), result, "", "")
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if completed == nil || completed.Status != "completed" {
		t.Fatalf("expected task status 'completed', got %+v", completed)
	}

	// Single assistant row is the core regression assertion.
	var rowCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM chat_message
		WHERE chat_session_id = $1 AND task_id = $2 AND role = 'assistant'
	`, sessionID, taskID).Scan(&rowCount); err != nil {
		t.Fatalf("count assistant chat_messages: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected exactly 1 assistant chat_message for task, got %d", rowCount)
	}

	var content string
	var elapsedMs *int64
	if err := testPool.QueryRow(ctx, `
		SELECT content, elapsed_ms FROM chat_message
		WHERE chat_session_id = $1 AND task_id = $2 AND role = 'assistant'
	`, sessionID, taskID).Scan(&content, &elapsedMs); err != nil {
		t.Fatalf("load assistant chat_message: %v", err)
	}

	// UnescapeBackslashEscapes must have run — literal `\n` should now be a
	// real newline. The naive "drop post-tx block" fix would leave the
	// upstream `redact.Text(payload.Output)` form and silently regress this.
	if strings.Contains(content, `\n`) {
		t.Fatalf("expected literal `\\n` to be decoded into real newline, got %q", content)
	}
	if !strings.Contains(content, "Første linje.\nAnden linje.") {
		t.Fatalf("expected unescaped two-line content, got %q", content)
	}

	// ElapsedMs must be set — drives the "Replied in Xs" caption in the UI.
	// The naive fix would also drop this since it only lived on the post-tx
	// insert before consolidation.
	if elapsedMs == nil {
		t.Fatalf("expected elapsed_ms to be set on assistant chat_message, got NULL")
	}
	if *elapsedMs <= 0 {
		t.Fatalf("expected elapsed_ms > 0 (task ran for ~3s), got %d", *elapsedMs)
	}
}
