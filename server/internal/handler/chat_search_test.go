package handler

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type chatSearchEnvelope struct {
	Messages []SearchChatMessageResponse `json:"messages"`
}

// chatSearchWorkspace is an isolated workspace (chat rows survive other tests'
// cleanups in the shared workspace) with one agent both creators chat with.
func chatSearchWorkspace(t *testing.T) (wsID, agentID, otherUserID string) {
	t.Helper()
	wsID = searchIndexWorkspace(t, "CHS")
	agentID = dbfx.Agent(t, "ChatSearchAgent", handlerTestRuntimeID(t), testutil.Cols{"workspace_id": wsID})
	otherUserID = dbfx.User(t, "Chat Search Other", "chat-search-other@example.com")
	return wsID, agentID, otherUserID
}

// insertChatMessageAt inserts one message with an explicit created_at (ranking
// asserts on it) and returns its id; insertChatMessage is taken by the agent
// builder tests.
func insertChatMessageAt(t *testing.T, sessionID, role, content, createdAt string) string {
	t.Helper()
	return dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": sessionID,
		"role":            role,
		"content":         content,
		"created_at":      testutil.Raw(createdAt),
		"message_kind":    "message",
	})
}

func searchChatMessages(t *testing.T, wsID, query string) []SearchChatMessageResponse {
	t.Helper()
	out := testutil.Decode[chatSearchEnvelope](t, testHandler.SearchChatMessages,
		searchIndexRequest(http.MethodGet, "/api/chat/sessions/search?q="+url.QueryEscape(query), wsID, nil),
		http.StatusOK)
	return out.Messages
}

// Creator scoping, matching, snippet, and per-hit session info.
func TestSearchChatMessages_FindsOwnMessages(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID, agentID, otherUserID := chatSearchWorkspace(t)
	mine := dbfx.ChatSession(t, agentID, testutil.Cols{"workspace_id": wsID, "title": "My deploy chat"})
	archived := dbfx.ChatSession(t, agentID, testutil.Cols{"workspace_id": wsID, "title": "Old planning", "status": "archived"})
	foreign := dbfx.ChatSession(t, agentID, testutil.Cols{"workspace_id": wsID, "creator_id": otherUserID, "title": "Someone else"})

	hit := insertChatMessageAt(t, mine, "user", "please deploy the payment service", "now() - interval '2 hours'")
	insertChatMessageAt(t, archived, "assistant", "the payment plan is ready for review", "now() - interval '1 hour'")
	insertChatMessageAt(t, foreign, "user", "payment secrets are elsewhere", "now()")

	messages := searchChatMessages(t, wsID, "payment")
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2 (own active + archived, not another member's)", len(messages))
	}
	ids := map[string]bool{}
	for _, m := range messages {
		ids[m.ID] = true
		if m.MatchSource != "content" {
			t.Fatalf("match_source = %q, want content", m.MatchSource)
		}
		if m.MatchedSnippet == nil || *m.MatchedSnippet == "" {
			t.Fatalf("matched_snippet missing on %+v", m)
		}
		if m.Session.ID != m.ChatSessionID {
			t.Fatalf("session id %q does not match chat_session_id %q", m.Session.ID, m.ChatSessionID)
		}
	}
	if !ids[hit] {
		t.Fatalf("own hit %s not returned: %v", hit, ids)
	}
}

// Multi-word queries need every word, like project and issue search.
func TestSearchChatMessages_MultiWordRequiresAllTerms(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID, agentID, _ := chatSearchWorkspace(t)
	session := dbfx.ChatSession(t, agentID, testutil.Cols{"workspace_id": wsID})
	insertChatMessageAt(t, session, "user", "deploy finished", "now() - interval '5 minutes'")
	insertChatMessageAt(t, session, "user", "only the deploy is mentioned here", "now()")

	if messages := searchChatMessages(t, wsID, "deploy finished"); len(messages) != 1 {
		t.Fatalf("got %d messages, want 1 (every word must match)", len(messages))
	}
}

// Phrase hits rank above scattered-term hits; inside a tier, newest first.
func TestSearchChatMessages_RanksPhraseThenRecency(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID, agentID, _ := chatSearchWorkspace(t)
	session := dbfx.ChatSession(t, agentID, testutil.Cols{"workspace_id": wsID})
	olderPhrase := insertChatMessageAt(t, session, "user", "please rotate keys now", "now() - interval '1 hour'")
	newerScattered := insertChatMessageAt(t, session, "user", "keys for the rotate job", "now() - interval '1 minute'")
	newestPhrase := insertChatMessageAt(t, session, "user", "rotate keys tonight", "now()")

	messages := searchChatMessages(t, wsID, "rotate keys")
	if len(messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(messages))
	}
	want := []string{newestPhrase, olderPhrase, newerScattered}
	for i, m := range messages {
		if m.ID != want[i] {
			t.Fatalf("order = %s, want %s (phrase tier first, then created_at DESC)",
				[]string{messages[0].ID, messages[1].ID, messages[2].ID}, want)
		}
	}
}

// Product-authored context the conversation never shows is not searchable.
func TestSearchChatMessages_ExcludesOnboardingKickoff(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID, agentID, _ := chatSearchWorkspace(t)
	session := dbfx.ChatSession(t, agentID, testutil.Cols{"workspace_id": wsID})
	dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": session,
		"role":            "user",
		"content":         "kickoff onboarding unique marker",
		"created_at":      testutil.Raw("now()"),
		"message_kind":    "onboarding_kickoff",
	})

	if messages := searchChatMessages(t, wsID, "unique marker"); len(messages) != 0 {
		t.Fatalf("got %d messages, want 0: kickoff context must stay hidden", len(messages))
	}
}

// q is required.
func TestSearchChatMessages_RequiresQuery(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	wsID, _, _ := chatSearchWorkspace(t)
	testutil.Call(t, testHandler.SearchChatMessages,
		searchIndexRequest(http.MethodGet, "/api/chat/sessions/search", wsID, nil)).
		Want(http.StatusBadRequest)
}
