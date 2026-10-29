package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func insertChatThreadTestMessage(t *testing.T, sessionID, role, content string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, $2, $3)
		RETURNING id
	`, sessionID, role, content).Scan(&id); err != nil {
		t.Fatalf("insert chat message: %v", err)
	}
	return id
}

func chatThreadRequest(t *testing.T, method, path, sessionID string, body any, params map[string]string) *http.Request {
	t.Helper()
	req := newRequest(method, path, body)
	req = withURLParam(req, "sessionId", sessionID)
	for k, v := range params {
		req = withURLParams(req, k, v)
	}
	return withChatTestWorkspaceCtx(t, req)
}

func createChatThreadForTest(t *testing.T, sessionID, rootID string) (ChatSessionResponse, int) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateChatThread(w, chatThreadRequest(t, http.MethodPost, "/api/chat/sessions/"+sessionID+"/threads", sessionID,
		map[string]any{"root_message_id": rootID}, nil))
	var resp ChatSessionResponse
	if w.Code == http.StatusOK || w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode thread: %v", err)
		}
	}
	return resp, w.Code
}

func listChatSessionsForTest(t *testing.T, query string) []ChatSessionResponse {
	t.Helper()
	req := withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, "/api/chat/sessions"+query, nil))
	w := httptest.NewRecorder()
	testHandler.ListChatSessions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListChatSessions: %d %s", w.Code, w.Body.String())
	}
	var out []ChatSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	return out
}

func findChatSession(sessions []ChatSessionResponse, id string) *ChatSessionResponse {
	for i := range sessions {
		if sessions[i].ID == id {
			return &sessions[i]
		}
	}
	return nil
}

func TestChatThread_NotFoundWhileTeamPageOff(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, false)
	agentID := createHandlerTestAgent(t, "ChatThreadFlagOffAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	rootID := insertChatThreadTestMessage(t, sessionID, "user", "root")

	if _, code := createChatThreadForTest(t, sessionID, rootID); code != http.StatusNotFound {
		t.Fatalf("create thread with flag off: expected 404, got %d", code)
	}
}

func TestChatThread_GetOrCreateIsListedOnlyForThreadClients(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	agentID := createHandlerTestAgent(t, "ChatThreadCreateAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	rootID := insertChatThreadTestMessage(t, sessionID, "assistant", "## Plan\nship the thing")

	thread, code := createChatThreadForTest(t, sessionID, rootID)
	if code != http.StatusCreated {
		t.Fatalf("first open: expected 201, got %d", code)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, thread.ID) })
	if thread.ParentChatSessionID == nil || *thread.ParentChatSessionID != sessionID {
		t.Fatalf("thread parent = %v, want %s", thread.ParentChatSessionID, sessionID)
	}
	if thread.RootChatMessageID == nil || *thread.RootChatMessageID != rootID {
		t.Fatalf("thread root = %v, want %s", thread.RootChatMessageID, rootID)
	}
	if thread.Title != "Plan" {
		t.Fatalf("thread title = %q, want first line without markdown", thread.Title)
	}

	again, code := createChatThreadForTest(t, sessionID, rootID)
	if code != http.StatusOK || again.ID != thread.ID {
		t.Fatalf("second open: got %d %s, want 200 %s", code, again.ID, thread.ID)
	}

	insertChatThreadTestMessage(t, thread.ID, "user", "follow-up")

	if findChatSession(listChatSessionsForTest(t, "?status=all"), thread.ID) != nil {
		t.Fatal("a client that does not ask for threads must not see one")
	}
	listed := findChatSession(listChatSessionsForTest(t, "?status=all&include_threads=true"), thread.ID)
	if listed == nil {
		t.Fatal("include_threads=true must list the thread")
	}
	if listed.ThreadReplyCount == nil || *listed.ThreadReplyCount != 1 {
		t.Fatalf("thread reply count = %v, want 1", listed.ThreadReplyCount)
	}
	if parent := findChatSession(listChatSessionsForTest(t, "?status=all&include_threads=true"), sessionID); parent == nil || parent.ThreadReplyCount != nil {
		t.Fatalf("top-level session must carry no thread fields: %#v", parent)
	}

	nestedRoot := insertChatThreadTestMessage(t, thread.ID, "user", "nested")
	if _, code := createChatThreadForTest(t, thread.ID, nestedRoot); code != http.StatusBadRequest {
		t.Fatalf("thread on a thread: expected 400, got %d", code)
	}
	otherSession := createHandlerTestChatSession(t, agentID)
	if _, code := createChatThreadForTest(t, otherSession, rootID); code != http.StatusNotFound {
		t.Fatalf("root from another session: expected 404, got %d", code)
	}
}

func TestCreateIssueFromChatMessage_LinksIssueToThread(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	agentID := createHandlerTestAgent(t, "ChatThreadIssueAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	messageID := insertChatThreadTestMessage(t, sessionID, "user", "Fix the login redirect\nIt loops on Safari.")

	w := httptest.NewRecorder()
	testHandler.CreateIssueFromChatMessage(w, chatThreadRequest(t, http.MethodPost,
		"/api/chat/sessions/"+sessionID+"/messages/"+messageID+"/issue", sessionID, map[string]any{},
		map[string]string{"messageId": messageID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssueFromChatMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateIssueFromChatMessageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, resp.Issue.ID)
		testPool.Exec(ctx, `DELETE FROM chat_message_issue WHERE issue_id = $1`, resp.Issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, resp.Issue.ID)
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, resp.Thread.ID)
	})
	if resp.Issue.Title != "Fix the login redirect" {
		t.Fatalf("issue title = %q", resp.Issue.Title)
	}
	if resp.Issue.AssigneeType == nil || *resp.Issue.AssigneeType != "agent" || resp.Issue.AssigneeID == nil || *resp.Issue.AssigneeID != agentID {
		t.Fatalf("issue must be assigned to the conversation's agent: %#v", resp.Issue)
	}
	if resp.Thread.RootChatMessageID == nil || *resp.Thread.RootChatMessageID != messageID {
		t.Fatalf("issue thread root = %v", resp.Thread.RootChatMessageID)
	}

	msgs, err := testHandler.Queries.ListChatMessages(context.Background(), parseUUID(resp.Thread.ID))
	if err != nil {
		t.Fatalf("list thread messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].MessageKind != "issue_event" || !strings.Contains(msgs[0].Content, resp.Issue.Identifier) {
		t.Fatalf("thread must open with the issue event: %#v", msgs)
	}

	lw := httptest.NewRecorder()
	testHandler.ListChatMessageIssues(lw, chatThreadRequest(t, http.MethodGet, "/api/chat/sessions/"+sessionID+"/issues", sessionID, nil, nil))
	var listed struct {
		Issues []ChatMessageIssueResponse `json:"issues"`
	}
	if err := json.Unmarshal(lw.Body.Bytes(), &listed); err != nil || lw.Code != http.StatusOK {
		t.Fatalf("ListChatMessageIssues: %d %s", lw.Code, lw.Body.String())
	}
	if len(listed.Issues) != 1 || listed.Issues[0].ChatMessageID != messageID || listed.Issues[0].Identifier != resp.Issue.Identifier {
		t.Fatalf("listed issues = %#v", listed.Issues)
	}
}

func TestDeleteChatSession_RemovesItsThreads(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.TeamPage, true)
	agentID := createHandlerTestAgent(t, "ChatThreadDeleteAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	rootID := insertChatThreadTestMessage(t, sessionID, "user", "root")
	thread, code := createChatThreadForTest(t, sessionID, rootID)
	if code != http.StatusCreated {
		t.Fatalf("create thread: %d", code)
	}

	w := httptest.NewRecorder()
	testHandler.DeleteChatSession(w, chatThreadRequest(t, http.MethodDelete, "/api/chat/sessions/"+sessionID, sessionID, nil, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession: %d %s", w.Code, w.Body.String())
	}
	if _, err := testHandler.Queries.GetChatSession(context.Background(), parseUUID(thread.ID)); err == nil {
		t.Fatal("deleting a conversation must delete its threads")
	}
}

func TestChatThreadRootContext_QuotesRootForFirstTurn(t *testing.T) {
	got := chatThreadRootContext(db.ChatMessage{Role: "assistant", Content: "line one\nline two"}, "why?")
	for _, want := range []string{"written by you", "> line one\n> line two", "Reply in this thread:\n\nwhy?"} {
		if !strings.Contains(got, want) {
			t.Fatalf("root context %q missing %q", got, want)
		}
	}
	if s := firstLineSummary("\n\n**Bold** `code` title\nrest", 10); s != "Bold code…" {
		t.Fatalf("firstLineSummary = %q", s)
	}
}
