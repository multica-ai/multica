package sessions

// FIR-1741 (Codex review of PR #1657) regression tests: concurrent `Start fresh`
// on one issue must never produce duplicate session positions or more than one
// open session — the P1 failure class (empty/duplicate sessions) through a new
// door. The advisory lock in StartFresh serializes the racing transactions and
// the unique index on (issue_id, position) (migration 9098) is the DB backstop.
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
	"sync"
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

// TestStartFreshUniquePositionBackstop proves the DB-level guarantee: two
// sessions on the same issue can never share a position.
func TestStartFreshUniquePositionBackstop(t *testing.T) {
	issueID, _ := seedIssue(t)
	ctx := context.Background()
	if _, err := sessTestPool.Exec(ctx,
		`INSERT INTO cerebro_session (issue_id, position, name, status) VALUES ($1::uuid, 1, 'Session 1', 'done')`, issueID); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, err := sessTestPool.Exec(ctx,
		`INSERT INTO cerebro_session (issue_id, position, name, status) VALUES ($1::uuid, 1, 'Dup', 'in_progress')`, issueID)
	if err == nil {
		t.Fatal("expected unique-violation on duplicate (issue_id, position), got nil")
	}
	if !strings.Contains(err.Error(), "duplicate key") && !strings.Contains(err.Error(), "unique") {
		t.Fatalf("expected unique violation, got: %v", err)
	}
}

// TestStartFreshConcurrent fires many Start fresh calls at once and asserts no
// duplicate positions and exactly one open session survive — the regression for
// finding 1. Without the advisory lock + unique index this races into duplicate
// "Session N" rows or multiple open sessions.
func TestStartFreshConcurrent(t *testing.T) {
	issueID, workspaceID := seedIssue(t)
	h := NewHandler(sessTestPool, db.New(sessTestPool))

	const n = 8
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := callStartFresh(h, issueID, workspaceID, `{"mode":"blank"}`)
			codes[idx] = rec.Code
		}(i)
	}
	wg.Wait()

	ctx := context.Background()
	var total, distinctPos, inProgress int
	if err := sessTestPool.QueryRow(ctx, `
		SELECT COUNT(*)::int,
		       COUNT(DISTINCT position)::int,
		       COUNT(*) FILTER (WHERE status <> 'done')::int
		FROM cerebro_session WHERE issue_id = $1::uuid`, issueID).Scan(&total, &distinctPos, &inProgress); err != nil {
		t.Fatalf("count sessions: %v", err)
	}

	if total != distinctPos {
		t.Errorf("duplicate session positions: total=%d distinct=%d", total, distinctPos)
	}
	if inProgress != 1 {
		t.Errorf("expected exactly one open session, got %d", inProgress)
	}
	okCount := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			okCount++
		}
	}
	if okCount == 0 {
		t.Errorf("expected at least one StartFresh to succeed, codes=%v", codes)
	}
}
