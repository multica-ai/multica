package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
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

// TestListUnrespondedUserMessages_OnlyUnanswered verifies that the daemon
// claim query returns user messages whose responded_at is NULL — the ones
// no prior task has marked as answered. Replaces the older time-based
// "since last assistant" filter, which silently dropped follow-up messages
// sent mid-stream (their created_at landed older than the prior turn's
// assistant reply, so they were excluded even though they had never been
// addressed).
func TestListUnrespondedUserMessages_OnlyUnanswered(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sessionID := createChatSessionForCoalesceTest(t)
	ctx := context.Background()

	insertChatMessage(t, sessionID, "user", "answered earlier")
	insertChatMessage(t, sessionID, "assistant", "old reply")
	insertChatMessage(t, sessionID, "user", "still pending 1")
	insertChatMessage(t, sessionID, "user", "still pending 2")

	// Mark the first user message as already answered. The remaining two
	// must still be returned regardless of created_at vs. assistant.
	if _, err := testPool.Exec(ctx,
		`UPDATE chat_message SET responded_at = now() WHERE chat_session_id = $1 AND content = $2`,
		sessionID, "answered earlier"); err != nil {
		t.Fatalf("seed responded_at: %v", err)
	}

	msgs, err := testHandler.Queries.ListUnrespondedUserMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list unresponded: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 unresponded user messages, got %d", len(msgs))
	}
	if msgs[0].Content != "still pending 1" || msgs[1].Content != "still pending 2" {
		t.Fatalf("unexpected contents/order: %q, %q", msgs[0].Content, msgs[1].Content)
	}
}

// TestListUnrespondedUserMessages_NoAssistantYet verifies the first-turn
// edge case: when no assistant message exists yet, every user message is
// still considered un-answered and must be returned.
func TestListUnrespondedUserMessages_NoAssistantYet(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sessionID := createChatSessionForCoalesceTest(t)
	ctx := context.Background()

	insertChatMessage(t, sessionID, "user", "first")
	insertChatMessage(t, sessionID, "user", "second")

	msgs, err := testHandler.Queries.ListUnrespondedUserMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list unresponded: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages on first turn, got %d", len(msgs))
	}
	if msgs[0].Content != "first" || msgs[1].Content != "second" {
		t.Fatalf("unexpected order: %q, %q", msgs[0].Content, msgs[1].Content)
	}
}

// TestMarkUserMessagesRespondedBefore_PreservesMidStream is the regression
// guard for the "Tom besked" bug. Reproduces the actual sequence captured
// in chat session 04094fc1: a user follow-up arrives while the prior task
// is running, and the prior task's completion uses a cutoff (started_at)
// that excludes it. The follow-up must remain unresponded so the queued
// successor task picks it up — instead of being silently marked answered
// by the prior reply and producing an empty prompt.
func TestMarkUserMessagesRespondedBefore_PreservesMidStream(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sessionID := createChatSessionForCoalesceTest(t)
	ctx := context.Background()

	// Replay timeline:
	//   T0:  user "first"      (claimed by task A)
	//   T1:  task A starts     (started_at = T1)
	//   T2:  user "midstream"  (sent while task A is running)
	//   T3:  task A completes, inserts assistant + marks responded
	//        with cutoff = T1. "midstream" was created at T2 > T1 →
	//        must stay unresponded.
	insertChatMessage(t, sessionID, "user", "first")

	// Capture the cutoff. now() runs in the same wall-clock as the inserts;
	// we want a cutoff strictly between "first" and "midstream".
	var cutoff pgtype.Timestamptz
	if err := testPool.QueryRow(ctx, `SELECT now()`).Scan(&cutoff); err != nil {
		t.Fatalf("read cutoff: %v", err)
	}

	insertChatMessage(t, sessionID, "user", "midstream")
	insertChatMessage(t, sessionID, "assistant", "reply to first")

	if err := testHandler.Queries.MarkUserMessagesRespondedBefore(ctx, db.MarkUserMessagesRespondedBeforeParams{
		ChatSessionID: parseUUID(sessionID),
		CreatedAt:     cutoff,
	}); err != nil {
		t.Fatalf("mark responded: %v", err)
	}

	msgs, err := testHandler.Queries.ListUnrespondedUserMessages(ctx, parseUUID(sessionID))
	if err != nil {
		t.Fatalf("list unresponded: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 unresponded message (the mid-stream one), got %d", len(msgs))
	}
	if msgs[0].Content != "midstream" {
		t.Fatalf("expected mid-stream message preserved, got %q", msgs[0].Content)
	}
}

// TestClaimTask_PopulatesBackwardsCompatChatMessage guards the JEH-330
// daemon-API breaking change: the backend now returns chat_messages
// (plural) but pre-JEH-330 daemons read chat_message (singular) and
// would build an empty prompt without it. A live regression hit prod
// when a pre-JEH-330 runtime claimed a chat task and the agent
// received "Tom besked modtaget" — the bug here is exactly that the
// JSON contract dropped chat_message. This test asserts the response
// has BOTH fields populated so old binaries keep working.
func TestClaimTask_PopulatesBackwardsCompatChatMessage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sessionID := createChatSessionForCoalesceTest(t)
	ctx := context.Background()

	insertChatMessage(t, sessionID, "user", "first message")
	insertChatMessage(t, sessionID, "user", "second message")

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT a.id, a.runtime_id FROM chat_session cs JOIN agent a ON a.id = cs.agent_id WHERE cs.id = $1`,
		sessionID,
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("lookup agent/runtime: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, chat_session_id)
		VALUES ($1, $2, NULL, 'queued', 0, $3)
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("queue chat task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, "test-daemon-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			ChatMessage  string   `json:"chat_message"`
			ChatMessages []string `json:"chat_messages"`
		} `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected task in response")
	}
	if len(resp.Task.ChatMessages) != 2 {
		t.Fatalf("ChatMessages: expected 2 entries, got %d", len(resp.Task.ChatMessages))
	}
	if resp.Task.ChatMessage != "second message" {
		t.Fatalf("ChatMessage backwards-compat field: expected %q (latest user msg), got %q",
			"second message", resp.Task.ChatMessage)
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
