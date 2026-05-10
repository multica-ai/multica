// CEREBRO-PATCH(chat-search-cerebro-test): JEH-901 — tests for SearchChatSessions
// + buildChatSessionSearchQuery. Mirrors search_test.go (unit-level SQL builder
// assertions) and chat_test.go (integration with testHandler + testPool).
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Unit tests for buildChatSessionSearchQuery ---

func TestBuildChatSessionSearchQuery_SingleTerm(t *testing.T) {
	query, args := buildChatSessionSearchQuery("Hello", []string{"Hello"})

	if args[0] != "hello" {
		t.Errorf("expected phrase arg lowercased, got %q", args[0])
	}

	if strings.Contains(query, "ILIKE") {
		t.Error("query should not contain ILIKE")
	}
	if !strings.Contains(query, "LOWER(m.content) LIKE") {
		t.Error("query should contain LOWER(m.content) LIKE")
	}

	// Workspace + creator scoping must be present in both layers (CTE filter
	// + outer SELECT). Without them the search would leak across users.
	if !strings.Contains(query, "s.workspace_id =") {
		t.Error("query must filter chat_session.workspace_id")
	}
	if !strings.Contains(query, "s.creator_id =") {
		t.Error("query must filter chat_session.creator_id")
	}

	// DISTINCT ON groups by session — newest match drives the snippet.
	if !strings.Contains(query, "DISTINCT ON (m.chat_session_id)") {
		t.Error("query must group matches per session via DISTINCT ON")
	}
}

func TestBuildChatSessionSearchQuery_MultiTerm(t *testing.T) {
	query, args := buildChatSessionSearchQuery("Foo Bar", []string{"Foo", "Bar"})

	if args[0] != "foo bar" {
		t.Errorf("expected phrase arg lowercased, got %q", args[0])
	}
	// args[1]=workspace_id placeholder, args[2]=creator_id placeholder, term args start at args[3].
	if args[3] != "foo" {
		t.Errorf("expected first term arg lowercased, got %q", args[3])
	}
	if args[4] != "bar" {
		t.Errorf("expected second term arg lowercased, got %q", args[4])
	}

	if !strings.Contains(query, " AND ") {
		t.Error("multi-word query should contain AND conditions for per-term matching")
	}
}

func TestBuildChatSessionSearchQuery_SpecialChars(t *testing.T) {
	_, args := buildChatSessionSearchQuery("100%", []string{"100%"})

	escaped, ok := args[0].(string)
	if !ok || !strings.Contains(escaped, `\%`) {
		t.Errorf("expected %% to be escaped in phrase arg, got %q", args[0])
	}
}

// --- Integration tests against the real DB fixture. ---

// seedChatSession creates a chat_session for the given creator + agent and
// inserts the supplied messages in order. Returns the session ID. Cleanup
// (cascade delete via chat_session) runs at test end.
func seedChatSession(t *testing.T, creatorID, agentID, title string, messages []string) string {
	t.Helper()
	ctx := context.Background()

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, $4, 'active')
		RETURNING id
	`, testWorkspaceID, agentID, creatorID, title).Scan(&sessionID); err != nil {
		t.Fatalf("seed chat_session: %v", err)
	}

	for _, m := range messages {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO chat_message (chat_session_id, role, content)
			VALUES ($1, 'user', $2)
		`, sessionID, m); err != nil {
			t.Fatalf("seed chat_message: %v", err)
		}
	}

	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM chat_message WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})

	return sessionID
}

func TestSearchChatSessions_RequiresQ(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodGet, "/api/chat/sessions/search", testUserID, nil)
	testHandler.SearchChatSessions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing q: want 400 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSearchChatSessions_Match(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Chat Search Match Agent", []byte(`null`))
	sessionID := seedChatSession(t, testUserID, agentID, "Coffee chat",
		[]string{"first message about coffee beans", "follow up on espresso roast"})

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodGet,
		"/api/chat/sessions/search?q=espresso", testUserID, nil)
	testHandler.SearchChatSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200 got %d body=%s", w.Code, w.Body.String())
	}

	var body struct {
		ChatSessions []struct {
			ChatSessionID  string  `json:"chat_session_id"`
			Title          string  `json:"title"`
			AgentID        string  `json:"agent_id"`
			MatchedSnippet *string `json:"matched_snippet,omitempty"`
		} `json:"chat_sessions"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(body.ChatSessions) != 1 {
		t.Fatalf("want 1 hit, got %d (body=%s)", len(body.ChatSessions), w.Body.String())
	}
	hit := body.ChatSessions[0]
	if hit.ChatSessionID != sessionID {
		t.Errorf("hit session id: want %s got %s", sessionID, hit.ChatSessionID)
	}
	if hit.AgentID != agentID {
		t.Errorf("hit agent id: want %s got %s", agentID, hit.AgentID)
	}
	if hit.Title != "Coffee chat" {
		t.Errorf("hit title: want %q got %q", "Coffee chat", hit.Title)
	}
	if hit.MatchedSnippet == nil || !strings.Contains(strings.ToLower(*hit.MatchedSnippet), "espresso") {
		t.Errorf("matched_snippet should contain query, got %v", hit.MatchedSnippet)
	}
	if body.Total != 1 {
		t.Errorf("total: want 1 got %d", body.Total)
	}
}

func TestSearchChatSessions_NewestMatchWins(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Chat Search Newest Agent", []byte(`null`))
	// Two messages match — newest must drive the snippet.
	seedChatSession(t, testUserID, agentID, "Recipes",
		[]string{"first mention of saffron rice", "second saffron note (newer)"})

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodGet,
		"/api/chat/sessions/search?q=saffron", testUserID, nil)
	testHandler.SearchChatSessions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}

	var body struct {
		ChatSessions []struct {
			MatchedSnippet *string `json:"matched_snippet,omitempty"`
		} `json:"chat_sessions"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.ChatSessions) != 1 {
		t.Fatalf("want 1 hit, got %d", len(body.ChatSessions))
	}
	snip := body.ChatSessions[0].MatchedSnippet
	if snip == nil || !strings.Contains(*snip, "newer") {
		t.Errorf("expected newest match in snippet, got %v", snip)
	}
}

func TestSearchChatSessions_OnlyCallersSessions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Chat Search Permission Agent", []byte(`null`))
	otherUser := createWorkspaceMemberUser(t, "member")

	// Other user's session — caller must not see it even though same workspace.
	seedChatSession(t, otherUser, agentID, "Other user chat",
		[]string{"unique-cerebro-keyword in someone else's session"})

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodGet,
		"/api/chat/sessions/search?q=unique-cerebro-keyword", testUserID, nil)
	testHandler.SearchChatSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}

	var body struct {
		ChatSessions []map[string]any `json:"chat_sessions"`
		Total        int64            `json:"total"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Total != 0 || len(body.ChatSessions) != 0 {
		t.Fatalf("caller saw other user's session: total=%d sessions=%d body=%s",
			body.Total, len(body.ChatSessions), w.Body.String())
	}
}

func TestSearchChatSessions_NoMatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Chat Search NoMatch Agent", []byte(`null`))
	seedChatSession(t, testUserID, agentID, "Empty hit", []string{"hello world"})

	w := httptest.NewRecorder()
	req := newChatRequest(http.MethodGet,
		"/api/chat/sessions/search?q=zzzzzznopezzz", testUserID, nil)
	testHandler.SearchChatSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		ChatSessions []map[string]any `json:"chat_sessions"`
		Total        int64            `json:"total"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Total != 0 || len(body.ChatSessions) != 0 {
		t.Fatalf("expected no hits, got total=%d sessions=%d", body.Total, len(body.ChatSessions))
	}
}
