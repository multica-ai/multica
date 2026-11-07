package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestParseChatSessionPage(t *testing.T) {
	for _, query := range []string{"status=all", "limit=0", "limit=101", "limit=bad", "cursor=bad", "cursor=" + strings.Repeat("a", 1025)} {
		t.Run(query[:min(len(query), 40)], func(t *testing.T) {
			if _, err := parseChatSessionPage(httptest.NewRequest("GET", "/?"+query, nil)); err == nil {
				t.Fatal("accepted invalid page parameters")
			}
		})
	}
	cursor := chatSessionPageCursor{Pin: time.Unix(0, 0), Activity: time.Now().UTC(), ID: "11111111-1111-4111-8111-111111111111", Status: "active"}
	body, _ := json.Marshal(cursor)
	raw := base64.RawURLEncoding.EncodeToString(body)
	page, err := parseChatSessionPage(httptest.NewRequest("GET", "/?limit=2&cursor="+raw, nil))
	if err != nil || !page.HasCursor || page.PageLimit != 3 {
		t.Fatalf("cursor did not round-trip: %+v, %v", page, err)
	}
	if _, err := parseChatSessionPage(httptest.NewRequest("GET", "/?status=archived&cursor="+raw, nil)); err == nil {
		t.Fatal("accepted an active cursor in the archive")
	}
}

func TestChatSessionsPage_BoundedPreviewsAndStableCursor(t *testing.T) {
	agent := dbfx.Agent(t, "Pagination demo", "")
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	body := strings.Repeat("Hello 世界🙂", 100)
	ids := map[string]bool{}
	for range 5 {
		id := dbfx.ChatSession(t, agent, testutil.Cols{"updated_at": at, "last_read_at": at.Add(-time.Hour)})
		ids[id] = true
		dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": id, "role": "assistant", "content": body, "created_at": at})
	}
	archived := dbfx.ChatSession(t, agent, testutil.Cols{"status": "archived", "explicitly_created_at": at})
	// A command-only channel session must not become a public Chat row.
	hidden := dbfx.ChatSession(t, agent)
	dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": hidden, "role": "user", "content": "/issue demo", "message_kind": "channel_command"})
	seen := map[string]bool{}
	query := "?limit=2"
	for {
		var page ChatSessionsPageResponse
		req := withChatTestWorkspaceCtx(t, newRequest("GET", "/api/chat/sessions/page"+query, nil))
		testutil.Call(t, testHandler.ListChatSessionsPage, req).Want(http.StatusOK).JSON(&page)
		if len(page.Sessions) > 2 {
			t.Fatalf("unbounded page: %d", len(page.Sessions))
		}
		for _, session := range page.Sessions {
			if !ids[session.ID] || seen[session.ID] {
				t.Fatalf("unexpected or duplicate row: %s", session.ID)
			}
			seen[session.ID] = true
			if session.LastMessage == nil || utf8.RuneCountInString(session.LastMessage.Content) != 256 || session.UnreadCount != 1 {
				t.Fatalf("bad bounded preview/unread: %+v", session)
			}
		}
		if !page.HasMore {
			if page.NextCursor != nil {
				t.Fatal("terminal page has a cursor")
			}
			break
		}
		if page.NextCursor == nil {
			t.Fatal("missing next cursor")
		}
		query = "?limit=2&cursor=" + url.QueryEscape(*page.NextCursor)
	}
	if len(seen) != len(ids) {
		t.Fatalf("lost tied rows: %d / %d", len(seen), len(ids))
	}
	var archive ChatSessionsPageResponse
	testutil.Call(t, testHandler.ListChatSessionsPage, withChatTestWorkspaceCtx(t, newRequest("GET", "/api/chat/sessions/page?status=archived", nil))).Want(http.StatusOK).JSON(&archive)
	if len(archive.Sessions) != 1 || archive.Sessions[0].ID != archived || archive.Sessions[0].UnreadCount != 0 {
		t.Fatalf("bad archive: %+v", archive)
	}
	var summary ChatSessionSummaryResponse
	testutil.Call(t, testHandler.GetChatSessionSummary, withChatTestWorkspaceCtx(t, newRequest("GET", "/api/chat/sessions/summary", nil))).Want(http.StatusOK).JSON(&summary)
	if summary.ActiveCount != 5 || summary.ArchivedCount != 1 || len(summary.UnreadBySession) != 5 {
		t.Fatalf("bad cross-page summary: %+v", summary)
	}
	var stored string
	dbfx.QueryRow(t, "SELECT content FROM chat_message WHERE chat_session_id = $1", func() string {
		for id := range ids {
			return id
		}
		return ""
	}()).Scan(&stored)
	if stored != body {
		t.Fatal("preview truncation changed the stored message")
	}
}

func TestChatSessionsPage_VisibilityBeforeLimit(t *testing.T) {
	privateAgent, owner, member := privateAgentTestFixture(t)
	publicAgent := dbfx.Agent(t, "Visible pagination demo", "", testutil.Cols{"owner_id": member})
	at := time.Now().UTC()
	for range 3 {
		dbfx.ChatSession(t, privateAgent, testutil.Cols{"creator_id": member, "explicitly_created_at": at, "updated_at": at})
	}
	visible := dbfx.ChatSession(t, publicAgent, testutil.Cols{"creator_id": member, "explicitly_created_at": at, "updated_at": at.Add(-time.Hour)})
	dbfx.ChatSession(t, publicAgent, testutil.Cols{"creator_id": owner, "explicitly_created_at": at})
	foreignWorkspace := dbfx.Insert(t, "workspace", testutil.Cols{"name": "Other demo", "slug": "pagination-other-" + visible})
	dbfx.ChatSession(t, publicAgent, testutil.Cols{"creator_id": member, "workspace_id": foreignWorkspace, "explicitly_created_at": at})
	membership, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{UserID: util.MustParseUUID(member), WorkspaceID: util.MustParseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	request := func(path string) *http.Request {
		return newRequestAs(member, "GET", path, nil).WithContext(middleware.SetMemberContext(context.Background(), testWorkspaceID, membership))
	}
	var page ChatSessionsPageResponse
	testutil.Call(t, testHandler.ListChatSessionsPage, request("/api/chat/sessions/page?limit=1")).Want(http.StatusOK).JSON(&page)
	if len(page.Sessions) != 1 || page.Sessions[0].ID != visible || page.HasMore {
		t.Fatalf("visibility was applied after LIMIT: %+v", page)
	}
	var summary ChatSessionSummaryResponse
	testutil.Call(t, testHandler.GetChatSessionSummary, request("/api/chat/sessions/summary")).Want(http.StatusOK).JSON(&summary)
	if summary.ActiveCount != 1 {
		t.Fatalf("summary leaked inaccessible rows: %+v", summary)
	}
}
