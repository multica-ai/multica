package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Test helpers for LLM chat auto-titling (MUL-4295)
// ---------------------------------------------------------------------------

// stubLLMCompletion returns an httptest server that mimics the OpenAI
// chat-completions endpoint, replying with `content` as the assistant message.
// When status != 200 it returns that status (with an error-ish body) so callers
// can exercise the upstream-failure fallback.
func stubLLMCompletion(t *testing.T, status int, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":{"message":"stub upstream error"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":` + jsonString(content) + `},"finish_reason":"stop"}]}`
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// jsonString escapes s into a JSON string literal (including surrounding
// quotes) so titles containing quotes/newlines embed cleanly in the stub body.
func jsonString(s string) string {
	b := make([]byte, 0, len(s)+2)
	b = append(b, '"')
	for _, r := range s {
		switch r {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\t':
			b = append(b, '\\', 't')
		default:
			b = append(b, string(r)...)
		}
	}
	b = append(b, '"')
	return string(b)
}

// withStubLLM points testHandler.LLM at a client backed by srv for the duration
// of the test, restoring the original (disabled) client afterwards.
func withStubLLM(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := testHandler.LLM
	testHandler.LLM = llm.New(llm.Config{APIKey: "test-key", BaseURL: srv.URL})
	t.Cleanup(func() { testHandler.LLM = prev })
}

// chatTitleTestAgentID returns the seeded workspace test agent id.
func chatTitleTestAgentID(t *testing.T) pgtype.UUID {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	return parseUUID(agentID)
}

// newChatTitleTestSession creates a chat session with the given (original)
// title and returns its row. Cleaned up via t.Cleanup.
func newChatTitleTestSession(t *testing.T, title string) db.ChatSession {
	t.Helper()
	session, err := testHandler.Queries.CreateChatSession(context.Background(), db.CreateChatSessionParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		AgentID:     chatTitleTestAgentID(t),
		CreatorID:   parseUUID(testUserID),
		Title:       title,
	})
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, uuidToString(session.ID))
	})
	return session
}

func chatSessionTitleFromDB(t *testing.T, sessionID pgtype.UUID) string {
	t.Helper()
	var title string
	if err := testPool.QueryRow(context.Background(),
		`SELECT title FROM chat_session WHERE id = $1`, uuidToString(sessionID),
	).Scan(&title); err != nil {
		t.Fatalf("load session title: %v", err)
	}
	return title
}

func requireDB(t *testing.T) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
}

// ---------------------------------------------------------------------------
// Case 1: LLM configured → first-round title becomes a concise semantic title.
// ---------------------------------------------------------------------------

func TestChatTitle_GeneratesSemanticTitleWhenConfigured(t *testing.T) {
	requireDB(t)
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, "修复登录跳转死循环"))

	original := "帮我看下为什么登录之后一直在几个页面之间来回跳转根本进不去首页"
	session := newChatTitleTestSession(t, original)

	updated, applied, err := testHandler.generateChatSessionTitle(context.Background(), session.ID, session.Title, original)
	if err != nil {
		t.Fatalf("generateChatSessionTitle: unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected title to be applied")
	}
	if updated.Title != "修复登录跳转死循环" {
		t.Fatalf("title = %q, want %q", updated.Title, "修复登录跳转死循环")
	}
	if got := chatSessionTitleFromDB(t, session.ID); got != "修复登录跳转死循环" {
		t.Fatalf("DB title = %q, want %q", got, "修复登录跳转死循环")
	}
}

// ---------------------------------------------------------------------------
// Case 2: LLM disabled (self-hosted, no key) → silent fallback to original.
// ---------------------------------------------------------------------------

func TestChatTitle_FallsBackWhenLLMDisabled(t *testing.T) {
	requireDB(t)
	// Force a disabled client regardless of ambient config.
	prev := testHandler.LLM
	testHandler.LLM = llm.New(llm.Config{})
	t.Cleanup(func() { testHandler.LLM = prev })

	original := "please help debug my flaky test"
	session := newChatTitleTestSession(t, original)

	_, applied, err := testHandler.generateChatSessionTitle(context.Background(), session.ID, session.Title, original)
	if err != llm.ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
	if applied {
		t.Fatal("expected no title applied when LLM disabled")
	}
	if got := chatSessionTitleFromDB(t, session.ID); got != original {
		t.Fatalf("DB title = %q, want original %q (must not change / blank)", got, original)
	}

	// The async entry point must be a no-op (no panic, no change) when disabled.
	testHandler.maybeGenerateChatTitleAsync(testWorkspaceID, testUserID, session.ID, session.Title, original)
	time.Sleep(50 * time.Millisecond)
	if got := chatSessionTitleFromDB(t, session.ID); got != original {
		t.Fatalf("DB title changed after disabled async call: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Case 3: LLM call fails (upstream 5xx / timeout) → silent fallback, no change.
// ---------------------------------------------------------------------------

func TestChatTitle_SilentFallbackOnUpstreamError(t *testing.T) {
	requireDB(t)
	withStubLLM(t, stubLLMCompletion(t, http.StatusInternalServerError, ""))

	original := "why does my query return duplicate rows"
	session := newChatTitleTestSession(t, original)

	_, applied, err := testHandler.generateChatSessionTitle(context.Background(), session.ID, session.Title, original)
	if err == nil {
		t.Fatal("expected an error from the failing upstream")
	}
	if applied {
		t.Fatal("expected no title applied on upstream error")
	}
	if got := chatSessionTitleFromDB(t, session.ID); got != original {
		t.Fatalf("DB title = %q, want unchanged original %q", got, original)
	}
}

// ---------------------------------------------------------------------------
// Case 4: user manually renamed the session → CAS miss, do not overwrite.
// ---------------------------------------------------------------------------

func TestChatTitle_DoesNotClobberManualRename(t *testing.T) {
	requireDB(t)
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, "Generated Title"))

	original := "original auto title"
	session := newChatTitleTestSession(t, original)

	// Simulate a manual rename landing before the async generator writes: the
	// generator still holds the stale observed title (session.Title), but the
	// DB now says something else.
	manual := "My Renamed Chat"
	if _, err := testHandler.Queries.UpdateChatSessionTitle(context.Background(), db.UpdateChatSessionTitleParams{
		ID:    session.ID,
		Title: manual,
	}); err != nil {
		t.Fatalf("simulate manual rename: %v", err)
	}

	_, applied, err := testHandler.generateChatSessionTitle(context.Background(), session.ID, original, original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied {
		t.Fatal("expected CAS miss (applied=false) when title was manually renamed")
	}
	if got := chatSessionTitleFromDB(t, session.ID); got != manual {
		t.Fatalf("DB title = %q, want manual rename %q preserved", got, manual)
	}
}

// ---------------------------------------------------------------------------
// Case 5: model returns empty / unusable output → fallback, no change.
// ---------------------------------------------------------------------------

func TestChatTitle_FallsBackOnEmptyModelOutput(t *testing.T) {
	requireDB(t)
	// Model replies with only quotes + punctuation → sanitizes to "".
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `"。"`))

	original := "some opening message"
	session := newChatTitleTestSession(t, original)

	_, applied, err := testHandler.generateChatSessionTitle(context.Background(), session.ID, session.Title, original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied {
		t.Fatal("expected no title applied for empty/unusable model output")
	}
	if got := chatSessionTitleFromDB(t, session.ID); got != original {
		t.Fatalf("DB title = %q, want unchanged original %q", got, original)
	}
}

// ---------------------------------------------------------------------------
// Case 6: auto-titling is idempotent — a second run does not re-title / clobber.
// ---------------------------------------------------------------------------

func TestChatTitle_AutoTitlesOnlyOnce(t *testing.T) {
	requireDB(t)
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, "First Generated Title"))

	original := "original title text"
	session := newChatTitleTestSession(t, original)

	// First generation applies against the observed original title.
	_, applied, err := testHandler.generateChatSessionTitle(context.Background(), session.ID, original, original)
	if err != nil || !applied {
		t.Fatalf("first generation: applied=%v err=%v", applied, err)
	}
	if got := chatSessionTitleFromDB(t, session.ID); got != "First Generated Title" {
		t.Fatalf("after first generation title = %q", got)
	}

	// A second run still observing the ORIGINAL title (as the first-message
	// trigger would) must be a no-op: the CAS no longer matches, so the
	// already-generated title is left intact.
	_, applied2, err2 := testHandler.generateChatSessionTitle(context.Background(), session.ID, original, original)
	if err2 != nil {
		t.Fatalf("second generation: unexpected error: %v", err2)
	}
	if applied2 {
		t.Fatal("expected second generation to be a no-op (CAS miss)")
	}
	if got := chatSessionTitleFromDB(t, session.ID); got != "First Generated Title" {
		t.Fatalf("second generation clobbered title: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Async path: successful generation publishes chat:session_updated so the
// frontend refreshes the title in place (reuses the manual-rename channel).
// ---------------------------------------------------------------------------

func TestChatTitle_AsyncPublishesSessionUpdated(t *testing.T) {
	requireDB(t)
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, "Async Semantic Title"))

	original := "async trigger original title"
	session := newChatTitleTestSession(t, original)

	got := make(chan protocol.ChatSessionUpdatedPayload, 1)
	testHandler.Bus.Subscribe(protocol.EventChatSessionUpdated, func(e events.Event) {
		if p, ok := e.Payload.(protocol.ChatSessionUpdatedPayload); ok && p.ChatSessionID == uuidToString(session.ID) {
			select {
			case got <- p:
			default:
			}
		}
	})

	testHandler.maybeGenerateChatTitleAsync(testWorkspaceID, testUserID, session.ID, session.Title, original)

	select {
	case p := <-got:
		if p.Title != "Async Semantic Title" {
			t.Fatalf("event title = %q, want %q", p.Title, "Async Semantic Title")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive chat:session_updated event for generated title")
	}
	if dbTitle := chatSessionTitleFromDB(t, session.ID); dbTitle != "Async Semantic Title" {
		t.Fatalf("DB title = %q, want %q", dbTitle, "Async Semantic Title")
	}
}

// ---------------------------------------------------------------------------
// sanitizeChatTitle unit tests: enforce the formatting rules regardless of how
// the model formats its reply (no quotes / no trailing punctuation / no label
// prefix / language-preserving / length cap).
// ---------------------------------------------------------------------------

func TestSanitizeChatTitle(t *testing.T) {
	longInput := ""
	for i := 0; i < chatSessionTitleMaxLen+50; i++ {
		longInput += "a"
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Fix login bug", "Fix login bug"},
		{"surrounding double quotes", `"Fix login bug"`, "Fix login bug"},
		{"surrounding single quotes", `'Fix login bug'`, "Fix login bug"},
		{"smart quotes", "“修复登录问题”", "修复登录问题"},
		{"cjk brackets", "「优化查询性能」", "优化查询性能"},
		{"english label prefix", "Title: Fix login bug", "Fix login bug"},
		{"chinese label prefix", "标题：修复登录问题", "修复登录问题"},
		{"label then quotes", `标题："修复登录问题"`, "修复登录问题"},
		{"prefix wrapped in quotes", `"Title: Fix login"`, "Fix login"},
		{"prefix wrapped in cjk brackets", "「标题：修复登录问题」", "修复登录问题"},
		{"prefix in quotes with trailing period", `"Title: Fix login".`, "Fix login"},
		{"prefix in cjk brackets with trailing period", "「标题：修复登录问题」。", "修复登录问题"},
		{"trailing period", "Fix login bug.", "Fix login bug"},
		{"trailing cjk period", "修复登录问题。", "修复登录问题"},
		{"newlines collapsed", "Fix\nlogin\nbug", "Fix login bug"},
		{"leading trailing space", "   Fix login bug   ", "Fix login bug"},
		{"only punctuation empty", `"。"`, ""},
		{"blank", "   ", ""},
		{"length cap", longInput, longInput[:chatSessionTitleMaxLen]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeChatTitle(tc.in); got != tc.want {
				t.Fatalf("sanitizeChatTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
