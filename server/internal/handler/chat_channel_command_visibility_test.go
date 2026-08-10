package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func insertChatVisibilityMessage(t *testing.T, sessionID, content, messageKind string, channelIngested bool, createdAt time.Time) {
	t.Helper()
	insertChatMessageRole(t, sessionID, "user", content, messageKind, channelIngested, createdAt)
}

// insertChatMessageRole inserts a chat_message row with an explicit role, for
// tests that need a non-user row (e.g. the transcript read-back role mapping).
func insertChatMessageRole(t *testing.T, sessionID, role, content, messageKind string, channelIngested bool, createdAt time.Time) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO chat_message (
			chat_session_id, role, content, message_kind, channel_ingested, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, sessionID, role, content, messageKind, channelIngested, createdAt); err != nil {
		t.Fatalf("insert chat message role=%q %q: %v", role, content, err)
	}
}

func TestChannelCommandVisibility_MessageListsExcludeCommandsBeforePagination(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChannelCommandMessageVisibilityAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	base := time.Date(2026, 8, 7, 1, 0, 0, 0, time.UTC)

	insertChatVisibilityMessage(t, sessionID, "visible oldest", "message", true, base)
	insertChatVisibilityMessage(t, sessionID, "/issue hidden oldest", "channel_command", true, base.Add(time.Second))
	insertChatVisibilityMessage(t, sessionID, "visible middle", "message", true, base.Add(2*time.Second))
	insertChatVisibilityMessage(t, sessionID, "/issue hidden newest", "channel_command", true, base.Add(3*time.Second))
	insertChatVisibilityMessage(t, sessionID, "visible newest", "message", true, base.Add(4*time.Second))

	legacyReq := httptest.NewRequest(http.MethodGet, "/api/chat/sessions/"+sessionID+"/messages", nil)
	legacyReq.Header.Set("X-User-ID", testUserID)
	legacyReq = withURLParam(legacyReq, "sessionId", sessionID)
	legacyReq = withChatTestWorkspaceCtx(t, legacyReq)
	legacyW := httptest.NewRecorder()
	testHandler.ListChatMessages(legacyW, legacyReq)
	if legacyW.Code != http.StatusOK {
		t.Fatalf("ListChatMessages: expected 200, got %d: %s", legacyW.Code, legacyW.Body.String())
	}
	var legacy []ChatMessageResponse
	if err := json.Unmarshal(legacyW.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("decode legacy messages: %v", err)
	}
	if len(legacy) != 3 || legacy[0].Content != "visible oldest" || legacy[1].Content != "visible middle" || legacy[2].Content != "visible newest" {
		t.Fatalf("legacy visible messages = %#v", legacy)
	}

	latest := fetchChatMessagesPageForTest(t, sessionID, url.Values{"limit": {"2"}})
	if len(latest.Messages) != 2 || latest.Messages[0].Content != "visible middle" || latest.Messages[1].Content != "visible newest" {
		t.Fatalf("latest visible page = %#v", latest)
	}
	if !latest.HasMore || latest.NextCursor == nil {
		t.Fatalf("latest visible page metadata = %#v", latest)
	}

	older := fetchChatMessagesPageForTest(t, sessionID, url.Values{
		"limit":             {"2"},
		"before_created_at": {latest.NextCursor.CreatedAt},
		"before_id":         {latest.NextCursor.ID},
	})
	if len(older.Messages) != 1 || older.Messages[0].Content != "visible oldest" || older.HasMore || older.NextCursor != nil {
		t.Fatalf("older visible page = %#v", older)
	}
}

func TestChannelCommandVisibility_SessionProjectionUsesPublicMessages(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChannelCommandSessionVisibilityAgent", []byte("[]"))
	webEmptySessionID := createHandlerTestChatSession(t, agentID)
	commandOnlySessionID := createHandlerTestChatSession(t, agentID)
	mixedSessionID := createHandlerTestChatSession(t, agentID)
	newerPublicSessionID := createHandlerTestChatSession(t, agentID)
	base := time.Date(2026, 8, 7, 2, 0, 0, 0, time.UTC)

	insertChatVisibilityMessage(t, commandOnlySessionID, "/issue hidden only", "channel_command", true, base.Add(3*time.Second))
	insertChatVisibilityMessage(t, mixedSessionID, "mixed public", "message", true, base)
	insertChatVisibilityMessage(t, mixedSessionID, "/issue hidden after public", "channel_command", true, base.Add(4*time.Second))
	insertChatVisibilityMessage(t, newerPublicSessionID, "newer public", "message", true, base.Add(2*time.Second))

	active, err := testHandler.Queries.ListChatSessionsByCreator(context.Background(), db.ListChatSessionsByCreatorParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		CreatorID:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("list active chat sessions: %v", err)
	}
	activeIndex := make(map[string]int, len(active))
	for i, row := range active {
		activeIndex[uuidToString(row.ID)] = i
	}
	if _, ok := activeIndex[commandOnlySessionID]; ok {
		t.Fatalf("command-only session %s is publicly listed", commandOnlySessionID)
	}
	if _, ok := activeIndex[webEmptySessionID]; !ok {
		t.Fatalf("empty web-created session %s must remain publicly listed", webEmptySessionID)
	}
	mixedIndex, mixedOK := activeIndex[mixedSessionID]
	newerIndex, newerOK := activeIndex[newerPublicSessionID]
	if !mixedOK || !newerOK {
		t.Fatalf("public sessions missing: mixed=%v newer=%v", mixedOK, newerOK)
	}
	if newerIndex >= mixedIndex {
		t.Fatalf("hidden command reordered mixed session: newer index=%d mixed index=%d", newerIndex, mixedIndex)
	}
	for _, row := range active {
		if uuidToString(row.ID) == mixedSessionID && (row.LastMessageContent != "mixed public" || row.LastMessageKind != "message") {
			t.Fatalf("mixed session preview = content %q kind %q", row.LastMessageContent, row.LastMessageKind)
		}
	}

	if _, err := testPool.Exec(context.Background(), `UPDATE chat_session SET status = 'archived' WHERE id = $1`, commandOnlySessionID); err != nil {
		t.Fatalf("archive command-only session: %v", err)
	}
	all, err := testHandler.Queries.ListAllChatSessionsByCreator(context.Background(), db.ListAllChatSessionsByCreatorParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		CreatorID:   parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("list all chat sessions: %v", err)
	}
	for _, row := range all {
		if uuidToString(row.ID) == commandOnlySessionID {
			t.Fatalf("command-only session %s is publicly listed in status=all", commandOnlySessionID)
		}
	}
}

func TestChannelCommandVisibility_CommandOnlySessionRejectsChatInteractionsButAllowsCleanup(t *testing.T) {
	agentID := createHandlerTestAgent(t, "ChannelCommandDirectAccessAgent", []byte("[]"))
	commandOnlySessionID := createHandlerTestChatSession(t, agentID)
	webEmptySessionID := createHandlerTestChatSession(t, agentID)
	insertChatVisibilityMessage(t, commandOnlySessionID, "/issue hidden", "channel_command", true, time.Now())

	get := func(sessionID string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/chat/sessions/"+sessionID, nil)
		req.Header.Set("X-User-ID", testUserID)
		req = withURLParam(req, "sessionId", sessionID)
		req = withChatTestWorkspaceCtx(t, req)
		w := httptest.NewRecorder()
		testHandler.GetChatSession(w, req)
		return w
	}

	if w := get(commandOnlySessionID); w.Code != http.StatusNotFound {
		t.Fatalf("command-only session: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := get(webEmptySessionID); w.Code != http.StatusOK {
		t.Fatalf("empty web-created session: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	request := func(method, path string, body any, handler http.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		req := newRequest(method, path, body)
		req.Header.Set("X-User-ID", testUserID)
		req = withURLParam(req, "sessionId", commandOnlySessionID)
		req = withChatTestWorkspaceCtx(t, req)
		w := httptest.NewRecorder()
		handler(w, req)
		return w
	}

	if w := request(
		http.MethodPatch,
		"/api/chat/sessions/"+commandOnlySessionID,
		map[string]any{"title": "must not be renamed"},
		testHandler.UpdateChatSession,
	); w.Code != http.StatusNotFound {
		t.Fatalf("rename command-only session: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if w := request(
		http.MethodPost,
		"/api/chat/sessions/"+commandOnlySessionID+"/messages",
		map[string]any{"content": "must not resurrect the chat"},
		testHandler.SendChatMessage,
	); w.Code != http.StatusNotFound {
		t.Fatalf("send to command-only session: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	var publicMessages int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM chat_message
		WHERE chat_session_id = $1 AND message_kind != 'channel_command'
	`, commandOnlySessionID).Scan(&publicMessages); err != nil {
		t.Fatalf("count public messages after rejected send: %v", err)
	}
	if publicMessages != 0 {
		t.Fatalf("rejected send created %d public messages", publicMessages)
	}

	// Archive remains reachable for stale clients so they can sever a channel
	// binding and clean up a session that the public Chat projection hides.
	if w := request(
		http.MethodPatch,
		"/api/chat/sessions/"+commandOnlySessionID+"/archive",
		map[string]any{"archived": true},
		testHandler.SetChatSessionArchived,
	); w.Code != http.StatusOK {
		t.Fatalf("archive command-only session: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM chat_session WHERE id = $1`, commandOnlySessionID).Scan(&status); err != nil {
		t.Fatalf("load archived command-only session: %v", err)
	}
	if status != "archived" {
		t.Fatalf("command-only session status = %q, want archived", status)
	}
}
