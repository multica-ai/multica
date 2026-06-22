package sessions

// FIR-1874 (thread = session) DB tests: the Send-button Handoff action
// (StartFresh) must resolve the chosen thread's root comment (closing the
// session, B-100%) and store a handoff on that thread's session row.
//
// Tests skip cleanly when no test DB is reachable, same pattern as
// wakeup/service_db_test.go and feature_flags/store_test.go.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var sessTestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping sessions integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping sessions integration tests: db not reachable: %v\n", err)
		os.Exit(m.Run())
	}
	sessTestPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// seedIssue creates a throwaway workspace + issue and returns their ids as
// strings (the form StartFresh reads from URL param / workspace context). The
// workspace is dropped on cleanup, cascading to the issue and its sessions.
func seedIssue(t *testing.T) (issueID, workspaceID string) {
	t.Helper()
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	err := sessTestPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('Sessions Test', 'sessions-test-'||gen_random_uuid()) RETURNING id::text`).
		Scan(&workspaceID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	err = sessTestPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		 VALUES ($1::uuid, 'Concurrency test', 'member', gen_random_uuid()) RETURNING id::text`,
		workspaceID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sessTestPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1::uuid`, workspaceID)
	})
	return issueID, workspaceID
}

func callStartFresh(h *Handler, issueID, workspaceID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("issueId", issueID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.SetMemberContext(ctx, workspaceID, db.Member{})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.StartFresh(rec, req)
	return rec
}

// seedRootComment inserts a root comment (a thread) on the issue and returns its
// id as a string.
func seedRootComment(t *testing.T, issueID, workspaceID string) string {
	t.Helper()
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	var id string
	if err := sessTestPool.QueryRow(context.Background(),
		`INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		 VALUES ($1::uuid, $2::uuid, 'member', gen_random_uuid(), 'work in this thread', 'comment')
		 RETURNING id::text`, issueID, workspaceID).Scan(&id); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	return id
}

// TestStartFreshHandoffResolvesThread proves the FIR-1874 behavior: handing off a
// thread resolves its root comment (closing the session) and writes a handoff on
// that thread's session row.
func TestStartFreshHandoffResolvesThread(t *testing.T) {
	issueID, workspaceID := seedIssue(t)
	commentID := seedRootComment(t, issueID, workspaceID)
	h := NewHandler(sessTestPool, db.New(sessTestPool))

	rec := callStartFresh(h, issueID, workspaceID, `{"root_comment_id":"`+commentID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("StartFresh handoff: code=%d body=%s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	ctx := context.Background()
	var resolved bool
	if err := sessTestPool.QueryRow(ctx,
		`SELECT resolved_at IS NOT NULL FROM comment WHERE id = $1::uuid`, commentID).Scan(&resolved); err != nil {
		t.Fatalf("read comment: %v", err)
	}
	if !resolved {
		t.Error("expected the thread root to be resolved after handoff (session closed)")
	}

	var hasHandoff bool
	if err := sessTestPool.QueryRow(ctx,
		`SELECT handoff IS NOT NULL FROM cerebro_session WHERE root_comment_id = $1::uuid`, commentID).Scan(&hasHandoff); err != nil {
		t.Fatalf("read session row: %v", err)
	}
	if !hasHandoff {
		t.Error("expected a session row with a handoff keyed to the thread root")
	}
}
