package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createChatSessionForCoalesceTest seeds an active chat session owned by the
// test user, against the workspace's default agent. Returns the session ID;
// cleanup is registered via t.Cleanup (cascades to chat_message and queued
// tasks via FK).
func createChatSessionForCoalesceTest(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, "coalesce-test-"+t.Name()).Scan(&sessionID); err != nil {
		t.Fatalf("insert chat_session: %v", err)
	}
	t.Cleanup(func() {
		// Delete queued/dispatched/running tasks first — they reference
		// chat_session and may have been left behind by SendChatMessage.
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})

	return sessionID
}

func sendChatMessage(t *testing.T, sessionID, content string) *httptest.ResponseRecorder {
	t.Helper()
	req := newChatRequest(http.MethodPost, "/api/chat-sessions/"+sessionID+"/messages", testUserID,
		map[string]any{"content": content})
	req = withURLParam(req, "sessionId", sessionID)
	w := httptest.NewRecorder()
	testHandler.SendChatMessage(w, req)
	return w
}

func countQueuedChatTasks(t *testing.T, sessionID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE chat_session_id = $1 AND status = 'queued'`,
		sessionID,
	).Scan(&n); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	return n
}

// TestSendChatMessage_CoalescesIntoSingleQueuedTask is the core regression
// guard for JEH-330. The user sends three messages back-to-back — simulating
// the mid-stream case where the first task is still running. The handler
// must keep the running task untouched and add at most one queued successor;
// the daemon will pull every unanswered user message at claim time.
//
// Without coalescing, three messages produced three queued tasks → three
// separate agent responses. The DoD smoke-test point 5 (one combined reply
// to messages 2+3) hinges on this behavior.
func TestSendChatMessage_CoalescesIntoSingleQueuedTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sessionID := createChatSessionForCoalesceTest(t)

	// First send creates the queued task (no prior task, so coalesce is a
	// straight insert).
	if w := sendChatMessage(t, sessionID, "first"); w.Code != http.StatusCreated {
		t.Fatalf("first send: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var first SendChatMessageResponse
	json.NewDecoder(sendChatMessage(t, sessionID, "second").Body).Decode(&first)
	// Second send: still queued, must coalesce (return existing task ID).
	var second SendChatMessageResponse
	json.NewDecoder(sendChatMessage(t, sessionID, "third").Body).Decode(&second)

	if got := countQueuedChatTasks(t, sessionID); got != 1 {
		t.Fatalf("expected exactly 1 queued chat task after 3 sends, got %d", got)
	}

	// All three messages should still be persisted — coalescing applies to the
	// task queue, not to the message history.
	var msgCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE chat_session_id = $1 AND role = 'user'`,
		sessionID,
	).Scan(&msgCount); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if msgCount != 3 {
		t.Fatalf("expected 3 user messages persisted, got %d", msgCount)
	}
}

// TestSendChatMessage_CoalesceIsRaceSafe fires N concurrent SendChatMessage
// calls against the same session. Without the partial unique index added in
// migration 057 (and the matching ON CONFLICT in CreateOrGetQueuedChatTask),
// two parallel inserts could both pass a "no queued task exists" check before
// either commits, ending up with multiple queued tasks for the same session.
func TestSendChatMessage_CoalesceIsRaceSafe(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sessionID := createChatSessionForCoalesceTest(t)

	const concurrent = 8
	var wg sync.WaitGroup
	wg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer wg.Done()
			sendChatMessage(t, sessionID, "concurrent")
		}()
	}
	wg.Wait()

	if got := countQueuedChatTasks(t, sessionID); got != 1 {
		t.Fatalf("expected exactly 1 queued chat task after %d concurrent sends, got %d", concurrent, got)
	}
}

// TestListUserMessagesSinceLastAssistant_BoundedByAssistant verifies that the
// daemon-claim query returns only user messages newer than the most recent
// assistant reply. This is what lets a queued successor task pull every
// unanswered user message into a single follow-up prompt without re-sending
// messages from earlier turns.
func TestListUserMessagesSinceLastAssistant_BoundedByAssistant(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sessionID := createChatSessionForCoalesceTest(t)
	ctx := context.Background()

	// Seed: user → assistant → user → user.
	// Only the last two should be returned (newer than the assistant reply).
	insertChatMessage(t, sessionID, "user", "old user 1")
	insertChatMessage(t, sessionID, "assistant", "old assistant reply")
	insertChatMessage(t, sessionID, "user", "new user 1")
	insertChatMessage(t, sessionID, "user", "new user 2")

	msgs, err := testHandler.Queries.ListUserMessagesSinceLastAssistant(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list user messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages newer than last assistant, got %d", len(msgs))
	}
	if msgs[0].Content != "new user 1" || msgs[1].Content != "new user 2" {
		t.Fatalf("unexpected message contents: %q, %q", msgs[0].Content, msgs[1].Content)
	}
}

// TestListUserMessagesSinceLastAssistant_NoAssistantYet verifies the
// first-turn edge case Sara flagged: when no assistant message exists in the
// session, the query falls back to returning ALL user messages instead of
// silently producing zero rows (which would leave the daemon with an empty
// prompt).
func TestListUserMessagesSinceLastAssistant_NoAssistantYet(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sessionID := createChatSessionForCoalesceTest(t)
	ctx := context.Background()

	insertChatMessage(t, sessionID, "user", "first")
	insertChatMessage(t, sessionID, "user", "second")

	msgs, err := testHandler.Queries.ListUserMessagesSinceLastAssistant(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list user messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages on first turn (no assistant yet), got %d", len(msgs))
	}
	if msgs[0].Content != "first" || msgs[1].Content != "second" {
		t.Fatalf("unexpected order: %q, %q", msgs[0].Content, msgs[1].Content)
	}
}

func insertChatMessage(t *testing.T, sessionID, role, content string) {
	t.Helper()
	if _, err := testHandler.Queries.CreateChatMessage(context.Background(), db.CreateChatMessageParams{
		ChatSessionID: parseUUID(sessionID),
		Role:          role,
		Content:       content,
	}); err != nil {
		t.Fatalf("create chat_message: %v", err)
	}
}
