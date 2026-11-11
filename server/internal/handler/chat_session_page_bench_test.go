package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Run with a local migrated database and -benchmem. Every row is synthetic;
// the full-list endpoint remains a useful control after the UI switches to pages.
func BenchmarkChatSessionListPayload(b *testing.B) {
	agent := dbfx.Agent(b, "Synthetic pagination benchmark", "")
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	body := strings.Repeat("Synthetic assistant response for pagination measurement. ", 160)
	rows := make([][]any, 2000)
	messages := make([][]any, len(rows))
	for i := range rows {
		id := fmt.Sprintf("%08x-4444-4444-8444-444444444444", i+1)
		status := "active"
		if i >= 500 {
			status = "archived"
		}
		lastRead := at
		if i < 500 && i%7 == 0 {
			lastRead = at.Add(-time.Hour)
		}
		rows[i] = []any{id, testWorkspaceID, testUserID, agent, fmt.Sprintf("Demo %04d", i), status, at.Add(-time.Duration(i) * time.Second), lastRead}
		messages[i] = []any{id, "assistant", body, at.Add(-time.Duration(i) * time.Second)}
	}
	ctx := context.Background()
	b.Cleanup(func() {
		dbfx.Exec(b, "DELETE FROM chat_message WHERE chat_session_id IN (SELECT id FROM chat_session WHERE agent_id = $1)", agent)
		dbfx.Exec(b, "DELETE FROM chat_session WHERE agent_id = $1", agent)
	})
	if _, err := testPool.CopyFrom(ctx, pgx.Identifier{"chat_session"}, []string{"id", "workspace_id", "creator_id", "agent_id", "title", "status", "updated_at", "last_read_at"}, pgx.CopyFromRows(rows)); err != nil {
		b.Fatal(err)
	}
	if _, err := testPool.CopyFrom(ctx, pgx.Identifier{"chat_message"}, []string{"chat_session_id", "role", "content", "created_at"}, pgx.CopyFromRows(messages)); err != nil {
		b.Fatal(err)
	}
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: util.MustParseUUID(testUserID), WorkspaceID: util.MustParseUUID(testWorkspaceID)})
	if err != nil {
		b.Fatal(err)
	}
	for _, scenario := range []struct {
		name, path string
		handler    http.HandlerFunc
	}{
		{"all", "/api/chat/sessions?status=all", testHandler.ListChatSessions},
		{"active-first-page", "/api/chat/sessions/page?limit=50", testHandler.ListChatSessionsPage},
		{"summary", "/api/chat/sessions/summary", testHandler.GetChatSessionSummary},
		{"archive-first-page", "/api/chat/sessions/page?status=archived&limit=50", testHandler.ListChatSessionsPage},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			for range b.N {
				req := newRequest("GET", scenario.path, nil).WithContext(middleware.SetMemberContext(ctx, testWorkspaceID, member))
				result := testutil.Call(b, scenario.handler, req).Want(http.StatusOK)
				b.ReportMetric(float64(result.Body.Len()), "response-B")
			}
		})
	}
}
