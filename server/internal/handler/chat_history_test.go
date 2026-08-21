package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/slack"
)

type fakeChatHistoryReader struct {
	page          channel.HistoryPage
	err           error
	overviewCalls int
	threadCalls   int
	gotSession    pgtype.UUID
	gotThreadID   string
	gotOpts       channel.HistoryOptions
}

func (f *fakeChatHistoryReader) ChannelOverview(_ context.Context, sid pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	f.overviewCalls++
	f.gotSession = sid
	f.gotOpts = opts
	return f.page, f.err
}

func (f *fakeChatHistoryReader) Thread(_ context.Context, sid pgtype.UUID, threadID string, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	f.threadCalls++
	f.gotSession = sid
	f.gotThreadID = threadID
	f.gotOpts = opts
	return f.page, f.err
}

// newChatHistoryTask inserts a chat task bound to a fresh chat session and
// returns the task id. With chatSession=false it inserts a non-chat task.
func newChatHistoryTask(t *testing.T, chatSession bool) string {
	t.Helper()
	agentID := createHandlerTestAgent(t, "ChatHistoryAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)
	var sessionArg any
	if chatSession {
		sessionArg = createHandlerTestChatSession(t, agentID)
	}
	var taskID string
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
	return taskID
}

// newChatHistoryTaskForSession inserts a chat task bound to a specific chat
// session and returns the task id.
func newChatHistoryTaskForSession(t *testing.T, sessionID string) string {
	t.Helper()
	agentID := createHandlerTestAgent(t, "ChatHistorySessionAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, chat_session_id)
		VALUES ($1, $2, 'completed', 0, $3)
		RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert chat history task for session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

// taskActorReq builds a request as the Auth middleware would leave it for a mat_
// task token: the server-set X-Actor-Source=task_token + the authoritative
// X-Task-ID. target may carry query params (e.g. "?id=70.0").
func taskActorReq(target, taskID string) *http.Request {
	req := newRequest("GET", target, nil)
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
	taskID := newChatHistoryTask(t, true)
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{
		ChannelType: "slack",
		Messages: []channel.HistoryMessage{
			{ID: "100", Author: "Alice", Role: channel.HistoryRoleUser, Text: "deploy thread", TS: "100", ThreadID: "100", ReplyCount: 3},
			{ID: "101", Author: "Bob", Role: channel.HistoryRoleUser, Text: "fyi", TS: "101"},
		},
		NextCursor: "100",
	}}
	withSlackHistory(t, fake)

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorReq("/api/chat/history?limit=10", taskID))

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
	if resp.Messages[0].ThreadID != "100" || resp.Messages[0].ReplyCount != 3 {
		t.Errorf("overview did not carry thread metadata: %+v", resp.Messages[0])
	}
	if fake.overviewCalls != 1 || fake.threadCalls != 0 {
		t.Errorf("expected ChannelOverview, got overview=%d thread=%d", fake.overviewCalls, fake.threadCalls)
	}
}

func TestGetChatThread_CurrentThread(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID := newChatHistoryTask(t, true)
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{ChannelType: "slack", ThreadID: "50.0", Messages: []channel.HistoryMessage{{ID: "50.0", TS: "50.0", Text: "root"}}}}
	withSlackHistory(t, fake)

	w := httptest.NewRecorder()
	testHandler.GetChatThread(w, taskActorReq("/api/chat/thread", taskID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if fake.threadCalls != 1 || fake.overviewCalls != 0 {
		t.Errorf("expected Thread, got overview=%d thread=%d", fake.overviewCalls, fake.threadCalls)
	}
	if fake.gotThreadID != "" {
		t.Errorf("current-thread read should pass empty id, got %q", fake.gotThreadID)
	}
}

func TestGetChatThread_ByID(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID := newChatHistoryTask(t, true)
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{ChannelType: "slack", ThreadID: "70.0"}}
	withSlackHistory(t, fake)

	w := httptest.NewRecorder()
	testHandler.GetChatThread(w, taskActorReq("/api/chat/thread?id=70.0", taskID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if fake.gotThreadID != "70.0" {
		t.Errorf("thread id passed to reader = %q, want 70.0", fake.gotThreadID)
	}
}

// TestGetChatHistory_NoSlackBindingFallsBackToTranscript: a session with no
// Slack binding (web chat / Feishu) is served from its stored chat_message
// transcript instead of "no channel integration". A fresh session has no
// messages, so the fallback returns an empty page with no note.
func TestGetChatHistory_NoSlackBindingFallsBackToTranscript(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID := newChatHistoryTask(t, true)
	withSlackHistory(t, &fakeChatHistoryReader{err: slack.ErrNoSlackSession})

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorReq("/api/chat/history", taskID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ChatChannelHistoryResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Note != "" || len(resp.Messages) != 0 {
		t.Fatalf("expected empty transcript with no note, got %+v", resp)
	}
}

// TestGetChatHistory_NoSlackBindingReadsStoredTranscript: when the session has
// no Slack binding but DOES have stored chat_message rows, the fallback returns
// those messages (oldest-first), so an agent can rebuild a web chat / Feishu
// conversation instead of being told nothing is readable. Channel-command rows
// are excluded, matching what the frontend shows.
func TestGetChatHistory_NoSlackBindingReadsStoredTranscript(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	agentID := createHandlerTestAgent(t, "ChatHistoryTranscriptAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := newChatHistoryTaskForSession(t, sessionID)
	withSlackHistory(t, &fakeChatHistoryReader{err: slack.ErrNoSlackSession})

	base := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	insertChatVisibilityMessage(t, sessionID, "first user turn", "message", true, base)
	insertChatMessageRole(t, sessionID, "assistant", "assistant reply", "message", true, base.Add(time.Second))
	insertChatVisibilityMessage(t, sessionID, "/issue hidden", "channel_command", true, base.Add(2*time.Second))
	insertChatVisibilityMessage(t, sessionID, "second user turn", "message", true, base.Add(3*time.Second))

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorReq("/api/chat/history", taskID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ChatChannelHistoryResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Note != "" {
		t.Fatalf("expected no note when a transcript is served, got %q", resp.Note)
	}
	if len(resp.Messages) != 3 {
		t.Fatalf("expected 3 visible messages, got %d: %+v", len(resp.Messages), resp.Messages)
	}
	if resp.Messages[0].Text != "first user turn" || resp.Messages[1].Text != "assistant reply" || resp.Messages[2].Text != "second user turn" {
		t.Fatalf("transcript not oldest-first / channel_command leaked: %+v", resp.Messages)
	}
	if resp.Messages[1].Role != channel.HistoryRoleAssistant {
		t.Errorf("assistant row role = %q, want %q", resp.Messages[1].Role, channel.HistoryRoleAssistant)
	}
	// Author uses the channel vocabulary ("Bot" / "User"), not the raw role
	// string, and TS is the row's RFC3339 creation time.
	if resp.Messages[0].Author != "User" || resp.Messages[1].Author != "Bot" || resp.Messages[2].Author != "User" {
		t.Errorf("transcript authors = %q/%q/%q, want User/Bot/User",
			resp.Messages[0].Author, resp.Messages[1].Author, resp.Messages[2].Author)
	}
	if got := resp.Messages[0].TS; got != base.UTC().Format(time.RFC3339Nano) {
		t.Errorf("transcript TS = %q, want %q", got, base.UTC().Format(time.RFC3339Nano))
	}
}

// TestGetChatHistory_NilReaderServesTranscript: a deployment with no Slack
// integration has SlackHistory == nil, but a web chat / Feishu / WeCom /
// DingTalk session's history still lives in chat_message — so the transcript is
// served instead of a "no channel integration" note. A fresh session has no
// messages, so the fallback returns an empty page with no note.
func TestGetChatHistory_NilReaderServesTranscript(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID := newChatHistoryTask(t, true)
	withSlackHistory(t, nil)

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorReq("/api/chat/history", taskID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ChatChannelHistoryResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Note != "" || len(resp.Messages) != 0 {
		t.Fatalf("expected empty transcript with no note when no reader configured, got %+v", resp)
	}
}

// TestGetChatHistory_NilReaderServesStoredTranscript is the regression for the
// no-Slack deployment: SlackHistory == nil AND the session has stored rows. The
// transcript must still be served — a no-Slack self-host is exactly the target
// of the transcript read-back feature, and must not dead-end on a
// "no channel integration" note.
func TestGetChatHistory_NilReaderServesStoredTranscript(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	agentID := createHandlerTestAgent(t, "ChatHistoryNilReaderAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := newChatHistoryTaskForSession(t, sessionID)
	withSlackHistory(t, nil)

	base := time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC)
	insertChatVisibilityMessage(t, sessionID, "nil reader still reads me", "message", true, base)
	insertChatMessageRole(t, sessionID, "assistant", "and my reply", "message", true, base.Add(time.Second))

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorReq("/api/chat/history", taskID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp ChatChannelHistoryResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Note != "" {
		t.Fatalf("expected no note when the transcript is served, got %q", resp.Note)
	}
	if len(resp.Messages) != 2 || resp.Messages[0].Text != "nil reader still reads me" || resp.Messages[1].Text != "and my reply" {
		t.Fatalf("nil reader did not serve the stored transcript: %+v", resp.Messages)
	}
}

// TestGetChatHistory_TranscriptNamesItsChannel: a served transcript reports the
// platform the session is bound to. HistoryPage.ChannelType is documented as
// empty ONLY for a session on no channel, so omitting it here would tell a
// WeCom/Feishu/DingTalk agent it is in a web-only chat — and would contradict
// the empty-read path, which already names the platform for the same session.
// One session, asserted both ways round, so the two paths cannot drift apart.
func TestGetChatHistory_TranscriptNamesItsChannel(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "ChatHistoryChannelTypeAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := newChatHistoryTaskForSession(t, sessionID)
	withSlackHistory(t, nil)

	var instID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, status, installer_user_id)
		VALUES ($1, $2, 'wecom', '{"app_id":"bot-transcript-channel"}'::jsonb, 'active', $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&instID); err != nil {
		t.Fatalf("create installation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = $1`, instID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO channel_chat_session_binding
			(chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
		VALUES ($1, $2, 'wecom', 'GROUP_TRANSCRIPT', 'group')
	`, sessionID, instID); err != nil {
		t.Fatalf("bind session: %v", err)
	}

	// Empty read first: this leg already named the platform before this change.
	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorReq("/api/chat/history", taskID))
	var empty ChatChannelHistoryResponse
	_ = json.Unmarshal(w.Body.Bytes(), &empty)

	base := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	insertChatVisibilityMessage(t, sessionID, "a wecom turn", "message", true, base)

	w = httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorReq("/api/chat/history", taskID))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var served ChatChannelHistoryResponse
	_ = json.Unmarshal(w.Body.Bytes(), &served)

	if len(served.Messages) != 1 {
		t.Fatalf("expected the stored transcript, got %+v", served.Messages)
	}
	if served.ChannelType != "wecom" {
		t.Errorf("served transcript channel_type = %q, want wecom", served.ChannelType)
	}
	if served.ChannelType != empty.ChannelType {
		t.Errorf("same session reports channel_type %q when it has messages and %q when it does not",
			served.ChannelType, empty.ChannelType)
	}
}

// TestGetChatHistory_WebOnlyTranscriptHasNoChannelType: the other half of the
// contract — a session bound to nothing must still report an empty channel_type.
func TestGetChatHistory_WebOnlyTranscriptHasNoChannelType(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	agentID := createHandlerTestAgent(t, "ChatHistoryWebOnlyTypeAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := newChatHistoryTaskForSession(t, sessionID)
	withSlackHistory(t, nil)
	insertChatVisibilityMessage(t, sessionID, "a web turn", "message", true, time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC))

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorReq("/api/chat/history", taskID))

	var resp ChatChannelHistoryResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.ChannelType != "" {
		t.Errorf("web-only session channel_type = %q, want empty", resp.ChannelType)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected the stored transcript, got %+v", resp.Messages)
	}
}

// TestGetChatHistory_TranscriptPagesWithoutDuplicatesOrGaps: the transcript
// honors the ?limit / ?before paging contract — walking back page by page with
// next_cursor yields every stored message exactly once, and a full page
// advertises a cursor while a short page does not.
func TestGetChatHistory_TranscriptPagesWithoutDuplicatesOrGaps(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	agentID := createHandlerTestAgent(t, "ChatHistoryPagingAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	taskID := newChatHistoryTaskForSession(t, sessionID)
	withSlackHistory(t, nil)

	base := time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC)
	const total = 7
	for i := 0; i < total; i++ {
		insertChatVisibilityMessage(t, sessionID, "msg "+strconv.Itoa(i), "message", true, base.Add(time.Duration(i)*time.Second))
	}

	seen := make([]string, 0, total)
	before := ""
	for page := 0; ; page++ {
		target := "/api/chat/history?limit=3"
		if before != "" {
			target += "&before=" + url.QueryEscape(before)
		}
		w := httptest.NewRecorder()
		testHandler.GetChatChannelHistory(w, taskActorReq(target, taskID))
		if w.Code != http.StatusOK {
			t.Fatalf("page %d status = %d, want 200: %s", page, w.Code, w.Body.String())
		}
		var resp ChatChannelHistoryResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Note != "" {
			t.Fatalf("page %d unexpected note %q", page, resp.Note)
		}
		// Within a page the transcript is oldest-first.
		prev := -1
		for _, m := range resp.Messages {
			seen = append(seen, m.Text)
			n, _ := strconv.Atoi(strings.TrimPrefix(m.Text, "msg "))
			if n <= prev {
				t.Fatalf("page %d not oldest-first: %v", page, resp.Messages)
			}
			prev = n
		}
		if len(resp.Messages) < 3 {
			// A short page is the last one: no cursor advertised.
			if resp.NextCursor != "" {
				t.Fatalf("last page advertised a cursor: %q", resp.NextCursor)
			}
			break
		}
		if resp.NextCursor == "" {
			t.Fatalf("full page %d advertised no cursor", page)
		}
		before = resp.NextCursor
	}

	// The union of all pages is exactly the full transcript: no duplicates, no
	// gaps. (Across pages the sequence is NOT monotonic — paging to older
	// messages means each later page is older than the previous one.)
	if len(seen) != total {
		t.Fatalf("paged %d messages, want %d: %v", len(seen), total, seen)
	}
	dup := map[string]bool{}
	for _, text := range seen {
		if dup[text] {
			t.Fatalf("duplicate message %q across pages: %v", text, seen)
		}
		dup[text] = true
	}
	for i := 0; i < total; i++ {
		want := "msg " + strconv.Itoa(i)
		if !dup[want] {
			t.Fatalf("missing %q across pages: %v", want, seen)
		}
	}
}

// TestGetChatHistory_RejectsForgedTaskID: a normal request (no server-set
// X-Actor-Source) that forges X-Task-ID — what a member could do with a JWT /
// mul_ PAT, since the Auth middleware does NOT strip a client-sent X-Task-ID —
// must be rejected, never served another session's history.
func TestGetChatHistory_RejectsForgedTaskID(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID := newChatHistoryTask(t, true)
	fake := &fakeChatHistoryReader{page: channel.HistoryPage{ChannelType: "slack"}}
	withSlackHistory(t, fake)

	req := newRequest("GET", "/api/chat/history", nil)
	req.Header.Set("X-Task-ID", taskID) // forged: no X-Actor-Source=task_token
	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if fake.overviewCalls != 0 || fake.threadCalls != 0 {
		t.Fatalf("reader must not be called for a forged X-Task-ID")
	}
}

func TestGetChatHistory_MissingTaskHeader(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	// Task-token actor source but no X-Task-ID: a defensive 400.
	req := newRequest("GET", "/api/chat/history", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestGetChatHistory_NonChatTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires test database")
	}
	taskID := newChatHistoryTask(t, false) // task with no chat_session_id
	withSlackHistory(t, &fakeChatHistoryReader{})

	w := httptest.NewRecorder()
	testHandler.GetChatChannelHistory(w, taskActorReq("/api/chat/history", taskID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}
