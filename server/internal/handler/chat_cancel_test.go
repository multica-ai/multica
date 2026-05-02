package handler

import (
	"context"
	"strings"
	"testing"
)

// TestCancelChatTask_PersistsAssistantMessageWithTaskID is the regression
// guard for JEH-348: when a user clicks Stop mid-stream, the chat timeline
// disappears because no chat_message row links to the cancelled task. The
// fix creates an assistant chat_message inside the cancel transaction so
// the frontend's AssistantMessage component (which renders the timeline
// from task_messages keyed by task_id) has something to attach to.
func TestCancelChatTask_PersistsAssistantMessageWithTaskID(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	sessionID := createChatSessionForCoalesceTest(t)

	// Seed a running chat task for the session.
	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT a.id, a.runtime_id FROM agent a
		 JOIN chat_session cs ON cs.agent_id = a.id
		 WHERE cs.id = $1`, sessionID,
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load agent/runtime: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			chat_session_id, started_at
		)
		VALUES ($1, $2, NULL, 'running', 2, $3, now())
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert running chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	// Seed task_messages: a text event, a tool_use, a tool_result, then more
	// text. Only the text events should land in the assistant chat_message
	// body — tool events are rendered separately by the frontend timeline.
	insertTaskMessage(t, taskID, 1, "text", "", "Starting work on this.", "")
	insertTaskMessage(t, taskID, 2, "tool_use", "Bash", "", "")
	insertTaskMessage(t, taskID, 3, "tool_result", "Bash", "", "ok\n")
	insertTaskMessage(t, taskID, 4, "text", "", "Found the file.", "")

	cancelled, err := testHandler.TaskService.CancelTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if cancelled == nil || cancelled.Status != "cancelled" {
		t.Fatalf("expected task status 'cancelled', got %+v", cancelled)
	}

	// Verify: an assistant chat_message exists for this session with task_id
	// pointing at the cancelled task, and content includes both text events
	// plus the cancelled-by-user marker.
	var role, content string
	var msgTaskID *string
	if err := testPool.QueryRow(ctx, `
		SELECT role, content, task_id::text FROM chat_message
		WHERE chat_session_id = $1 AND task_id = $2
	`, sessionID, taskID).Scan(&role, &content, &msgTaskID); err != nil {
		t.Fatalf("expected assistant chat_message linked to cancelled task, got: %v", err)
	}
	if role != "assistant" {
		t.Fatalf("expected role 'assistant', got %q", role)
	}
	if msgTaskID == nil || *msgTaskID != taskID {
		t.Fatalf("expected task_id %q on chat_message, got %v", taskID, msgTaskID)
	}
	if !strings.Contains(content, "Starting work on this.") || !strings.Contains(content, "Found the file.") {
		t.Fatalf("expected both text events in content, got %q", content)
	}
	if !strings.Contains(content, "_(stoppet af bruger)_") {
		t.Fatalf("expected cancelled-by-user marker in content, got %q", content)
	}
	// Tool events must not bleed into the chat_message body — they're
	// rendered from task_messages by the frontend timeline.
	if strings.Contains(content, "Bash") || strings.Contains(content, "ok\n") {
		t.Fatalf("expected tool events to be excluded from content, got %q", content)
	}
}

// TestCancelChatTask_NoTextEvents_StillPersistsMarker covers the cancel-very-early
// case: the user clicks Stop before the agent has emitted a single text event.
// The chat_message body must still exist (NOT NULL constraint) and carry the
// marker — otherwise we trip the FK linkage and AssistantMessage has nothing
// to render the timeline against.
func TestCancelChatTask_NoTextEvents_StillPersistsMarker(t *testing.T) {
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

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			chat_session_id, started_at
		)
		VALUES ($1, $2, NULL, 'running', 2, $3, now())
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert running chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	if _, err := testHandler.TaskService.CancelTask(ctx, parseUUID(taskID)); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}

	var content string
	if err := testPool.QueryRow(ctx, `
		SELECT content FROM chat_message
		WHERE chat_session_id = $1 AND task_id = $2
	`, sessionID, taskID).Scan(&content); err != nil {
		t.Fatalf("expected assistant chat_message even without text events: %v", err)
	}
	if content != "_(stoppet af bruger)_" {
		t.Fatalf("expected marker-only content, got %q", content)
	}
}

func insertTaskMessage(t *testing.T, taskID string, seq int, msgType, tool, content, output string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO task_message (task_id, seq, type, tool, content, output)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''))
	`, taskID, seq, msgType, tool, content, output)
	if err != nil {
		t.Fatalf("insert task_message seq=%d: %v", seq, err)
	}
}
