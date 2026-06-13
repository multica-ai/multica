package wakeup

// Integration tests for TECH-3487: a fired wakeup must thread its note back into
// the conversation that scheduled it, so the agent's reply lands in the original
// thread instead of a new orphaned root note. These exercise the real DB through
// resolveWakeupParent (the decision dispatch uses to set the note's parent) and a
// CreateComment round-trip proving the note resolves under the origin thread.
//
// Tests skip cleanly when no test DB is reachable, same pattern as
// sprints/sweeper_db_test.go and feature_flags/store_test.go.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	wkTestEmail         = "wakeup-origin-test@multica.ai"
	wkTestName          = "Wakeup Origin Test"
	wkTestWorkspaceSlug = "wakeup-origin-tests"
)

var (
	wkPool        *pgxpool.Pool
	wkWorkspaceID pgtype.UUID
	wkUserID      pgtype.UUID
	wkAgentID     pgtype.UUID
	wkIssueID     pgtype.UUID
	wkOtherIssue  pgtype.UUID
	wkRootID      pgtype.UUID // root comment on wkIssueID
	wkReplyID     pgtype.UUID // reply under wkRootID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping wakeup origin integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping wakeup origin integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	_ = cleanupWkFixture(ctx, pool)
	if err := setupWkFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to set up wakeup origin fixture: %v\n", err)
		_ = cleanupWkFixture(ctx, pool)
		pool.Close()
		os.Exit(1)
	}
	wkPool = pool
	code := m.Run()
	if err := cleanupWkFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean wakeup origin fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupWkFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		wkTestName, wkTestEmail).Scan(&wkUserID); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, $3, $4) RETURNING id`,
		"Wakeup Origin Tests", wkTestWorkspaceSlug, "Temporary workspace", "WOT").Scan(&wkWorkspaceID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		wkWorkspaceID, wkUserID); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	var runtimeID pgtype.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		 VALUES ($1, $2, 'local', 'claude_code', 'online') RETURNING id`,
		wkWorkspaceID, "Wakeup Origin Runtime").Scan(&runtimeID); err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		 VALUES ($1, $2, 'local', $3) RETURNING id`,
		wkWorkspaceID, "Wakeup Origin Agent", runtimeID).Scan(&wkAgentID); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, $2, 'todo', 'none', 'member', $3, 1) RETURNING id`,
		wkWorkspaceID, "Origin issue", wkUserID).Scan(&wkIssueID); err != nil {
		return fmt.Errorf("create issue: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, $2, 'todo', 'none', 'member', $3, 2) RETURNING id`,
		wkWorkspaceID, "Other issue", wkUserID).Scan(&wkOtherIssue); err != nil {
		return fmt.Errorf("create other issue: %w", err)
	}
	// Original conversation: a root comment plus a reply under it.
	if err := pool.QueryRow(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type)
		 VALUES ($1, $2, 'member', $3, 'original request', 'comment') RETURNING id`,
		wkWorkspaceID, wkIssueID, wkUserID).Scan(&wkRootID); err != nil {
		return fmt.Errorf("create root comment: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type, parent_id)
		 VALUES ($1, $2, 'member', $3, 'a reply in the thread', 'comment', $4) RETURNING id`,
		wkWorkspaceID, wkIssueID, wkUserID, wkRootID).Scan(&wkReplyID); err != nil {
		return fmt.Errorf("create reply comment: %w", err)
	}
	return nil
}

func cleanupWkFixture(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`DELETE FROM cerebro_agent_wakeup WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM comment WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM issue WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM agent WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM agent_runtime WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM workspace WHERE slug = $1`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt, wkTestWorkspaceSlug); err != nil {
			return fmt.Errorf("cleanup %q: %w", stmt, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, wkTestEmail); err != nil {
		return fmt.Errorf("cleanup user: %w", err)
	}
	return nil
}

// newWakeup inserts a time wakeup with the given origin comment (zero = none) and
// returns the persisted row for resolveWakeupParent to act on.
func newWakeup(t *testing.T, ctx context.Context, svc *Service, issueID, origin pgtype.UUID) cerebrodb.CerebroAgentWakeup {
	t.Helper()
	row, err := svc.Cerebro.CreateCerebroAgentWakeup(ctx, cerebrodb.CreateCerebroAgentWakeupParams{
		WorkspaceID:     wkWorkspaceID,
		AgentID:         wkAgentID,
		IssueID:         issueID,
		Prompt:          "check back later",
		TriggerType:     TriggerTime,
		FireAt:          pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		OriginCommentID: origin,
	})
	if err != nil {
		t.Fatalf("create wakeup: %v", err)
	}
	return row
}

func wkService() *Service {
	return &Service{Cerebro: cerebrodb.New(wkPool), Queries: db.New(wkPool)}
}

func TestResolveWakeupParent_RootOrigin(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	row := newWakeup(t, ctx, svc, wkIssueID, wkRootID)

	got := svc.resolveWakeupParent(ctx, row, wkIssueID)
	if util.UUIDToString(got) != util.UUIDToString(wkRootID) {
		t.Fatalf("parent = %s, want root %s", util.UUIDToString(got), util.UUIDToString(wkRootID))
	}
}

func TestResolveWakeupParent_ReplyOriginNormalizesToRoot(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	// Origin is a reply deep in the thread; the note must still anchor to the
	// thread root so it (and the agent's reply under it) resolve into the thread.
	row := newWakeup(t, ctx, svc, wkIssueID, wkReplyID)

	got := svc.resolveWakeupParent(ctx, row, wkIssueID)
	if util.UUIDToString(got) != util.UUIDToString(wkRootID) {
		t.Fatalf("parent = %s, want normalized root %s", util.UUIDToString(got), util.UUIDToString(wkRootID))
	}
}

func TestResolveWakeupParent_CrossIssueRejected(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	// Wakeup fires on a different issue than the origin comment lives on — never
	// cross threads; fall back to a root note.
	row := newWakeup(t, ctx, svc, wkOtherIssue, wkRootID)

	got := svc.resolveWakeupParent(ctx, row, wkOtherIssue)
	if got.Valid {
		t.Fatalf("parent = %s, want zero (cross-issue rejected)", util.UUIDToString(got))
	}
}

func TestResolveWakeupParent_NoOrigin(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	row := newWakeup(t, ctx, svc, wkIssueID, pgtype.UUID{})

	got := svc.resolveWakeupParent(ctx, row, wkIssueID)
	if got.Valid {
		t.Fatalf("parent = %s, want zero (no origin)", util.UUIDToString(got))
	}
}

// TestWakeupNoteThreadsUnderOrigin proves the product behavior end to end: the
// dispatched wakeup note, created with the resolved parent, lands inside the
// original thread (parent_id == thread root), not as a new orphaned root.
func TestWakeupNoteThreadsUnderOrigin(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	row := newWakeup(t, ctx, svc, wkIssueID, wkReplyID)

	parentID := svc.resolveWakeupParent(ctx, row, wkIssueID)
	note, err := svc.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     wkIssueID,
		WorkspaceID: wkWorkspaceID,
		AuthorType:  "system",
		AuthorID:    util.MustParseUUID("00000000-0000-0000-0000-000000000000"),
		Content:     buildWakeupCommentContent(row.TriggerType, row.Prompt),
		Type:        commentTypeWakeup,
		ParentID:    parentID,
	})
	if err != nil {
		t.Fatalf("create wakeup note: %v", err)
	}
	if !note.ParentID.Valid || util.UUIDToString(note.ParentID) != util.UUIDToString(wkRootID) {
		t.Fatalf("note parent = %s, want thread root %s", util.UUIDToString(note.ParentID), util.UUIDToString(wkRootID))
	}
}
