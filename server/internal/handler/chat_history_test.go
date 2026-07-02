package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
)

type fakeChatHistoryReader struct {
	page       channel.HistoryPage
	err        error
	gotSession pgtype.UUID
	gotOpts    channel.HistoryOptions
}

func (f *fakeChatHistoryReader) Fetch(_ context.Context, sid pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	f.gotSession = sid
	f.gotOpts = opts
	return f.page, f.err
}

// newChatHistoryTask inserts a chat task bound to a fresh chat session and
// returns the task id and (for chat tasks) the session id. With
// chatSession=false it inserts a non-chat task and an empty session id.
func newChatHistoryTask(t *testing.T, chatSession bool) (taskID, sessionID string) {
	t.Helper()
	agentID := createHandlerTestAgent(t, "ChatHistoryAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)
	var sessionArg any
	if chatSession {
		sessionID = createHandlerTestChatSession(t, agentID)
		sessionArg = sessionID
	}
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, chat_session_id)
		VALUES ($1, $2, 'completed', 0, $3)
		RETURNING id
	`, agentID, runtimeID, sessionArg).Scan(&taskID); err != nil {
		t.Fatalf("insert chat history task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID, sessionID
}

// addAssistantMessage records a prior bot reply in the session, so the endpoint
// classifies the next read as a follow-up. The chat_session cleanup cascades to
// chat_message, so no separate cleanup is needed.
func addAssistantMessage(t *testing.T, sessionID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'assistant', 'prior reply')`,
		sessionID); err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}
}

// taskActorRequest builds a /api/chat/history request as the Auth middleware
// would leave it for a mat_ task token: the server-set X-Actor-Source=task_token
// plus the authoritative X-Task-ID.
func taskActorRequest(taskID string) *http.Request {
	req := newRequest("GET", "/api/chat/history", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Task-ID", taskID)
	return req
}

func withSlackHistory(t *testing.T, r ChatChannelHistoryReader) {
	t.Helper()
	orig := testHandler.SlackHistory
	testHandler.SlackHistory = r
	t.Cleanup(func() { testHandler.SlackHistory = orig })
}

func TestGetChatChannelHistory_Success(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID, _ := newChatHistoryTask(t, true)
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{
		ChannelType: "slack",
		Messages: []channel.HistoryMessage{
			{ID: "100", Author: "Alice", Role: channel.HistoryRoleUser, Text: "alert", TS: "100"},
			{ID: "101", Author: "Bot", Role: channel.HistoryRoleAssistant, Text: "on it", TS: "101"},
		},
		NextCursor: "100",
	}}
	withSlackHistory(t, fake)

	req := taskActorRequest(taskID)
	req.URL.RawQuery = "limit=10"
	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ChatChannelHistoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ChannelType != "slack" || len(resp.Messages) != 2 || resp.NextCursor != "100" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if !fake.gotSession.Valid {
		t.Errorf("reader was not called with a session id")
	}
}

func TestGetChatChannelHistory_NoBindingReturnsNote(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID, _ := newChatHistoryTask(t, true)
	withSlackHistory(t, &fakeChatHistoryReader{err: slack.ErrNoSlackSession})

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorRequest(taskID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ChatChannelHistoryResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Note == "" || len(resp.Messages) != 0 {
		t.Fatalf("expected empty messages + a note, got %+v", resp)
	}
}

func TestGetChatChannelHistory_NilReaderReturnsNote(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID, _ := newChatHistoryTask(t, true)
	withSlackHistory(t, nil)

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorRequest(taskID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ChatChannelHistoryResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Note == "" {
		t.Fatalf("expected a note when no reader configured, got %+v", resp)
	}
}

// TestGetChatChannelHistory_RejectsForgedTaskID is the security regression test
// for Niko's must-fix: a normal request (no server-set X-Actor-Source) that
// forges X-Task-ID — exactly what a workspace member could do with a JWT / mul_
// PAT, since the Auth middleware does NOT strip a client-sent X-Task-ID — must be
// rejected, never served another session's history.
func TestGetChatChannelHistory_RejectsForgedTaskID(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID, _ := newChatHistoryTask(t, true)
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{ChannelType: "slack"}}
	withSlackHistory(t, fake)

	req := newRequest("GET", "/api/chat/history", nil)
	req.Header.Set("X-Task-ID", taskID) // forged: no X-Actor-Source=task_token
	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if fake.gotSession.Valid {
		t.Fatalf("reader must not be called for a forged X-Task-ID")
	}
}

func TestGetChatChannelHistory_MissingTaskHeader(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	// Task-token actor source but no X-Task-ID: a defensive 400 (the mat_ branch
	// always stamps both, so this should not happen in practice).
	req := newRequest("GET", "/api/chat/history", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestGetChatChannelHistory_NonChatTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID, _ := newChatHistoryTask(t, false) // task with no chat_session_id
	withSlackHistory(t, &fakeChatHistoryReader{})

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorRequest(taskID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// TestGetChatChannelHistory_AutoFirstTurnReadsChannel: with no prior bot reply,
// scope=auto resolves to channel (the surrounding context before the thread).
func TestGetChatChannelHistory_AutoFirstTurnReadsChannel(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID, _ := newChatHistoryTask(t, true) // no assistant message => first turn
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{ChannelType: "slack", Scope: channel.HistoryScopeChannel}}
	withSlackHistory(t, fake)

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorRequest(taskID)) // no ?scope => auto
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if fake.gotOpts.Scope != channel.HistoryScopeChannel {
		t.Fatalf("auto first-turn scope = %q, want channel", fake.gotOpts.Scope)
	}
}

// TestGetChatChannelHistory_AutoFollowUpReadsThread: once the bot has replied,
// scope=auto resolves to thread.
func TestGetChatChannelHistory_AutoFollowUpReadsThread(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID, sessionID := newChatHistoryTask(t, true)
	addAssistantMessage(t, sessionID) // bot already replied => follow-up
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{ChannelType: "slack", Scope: channel.HistoryScopeThread}}
	withSlackHistory(t, fake)

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorRequest(taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if fake.gotOpts.Scope != channel.HistoryScopeThread {
		t.Fatalf("auto follow-up scope = %q, want thread", fake.gotOpts.Scope)
	}
}

// TestGetChatChannelHistory_ExplicitChannelScope: ?scope=channel overrides the
// auto default even on a follow-up.
func TestGetChatChannelHistory_ExplicitChannelScope(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID, sessionID := newChatHistoryTask(t, true)
	addAssistantMessage(t, sessionID) // follow-up, but explicit override below
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{ChannelType: "slack", Scope: channel.HistoryScopeChannel}}
	withSlackHistory(t, fake)

	req := taskActorRequest(taskID)
	req.URL.RawQuery = "scope=channel"
	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if fake.gotOpts.Scope != channel.HistoryScopeChannel {
		t.Fatalf("explicit scope = %q, want channel", fake.gotOpts.Scope)
	}
}
