package issuestatus

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping issuestatus tests: could not connect to database: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping issuestatus tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// wantSystemStatuses maps each built-in system_key to its immutable Category.
var wantSystemStatuses = map[string]string{
	"backlog":     "backlog",
	"todo":        "todo",
	"in_progress": "in_progress",
	"in_review":   "in_progress",
	"blocked":     "in_progress",
	"done":        "done",
	"cancelled":   "cancelled",
}

// freshWorkspace creates a real workspace row (the seed's FOR KEY SHARE gate
// only seeds workspaces that exist) and registers idempotent cleanup of both
// the catalog and the workspace. Tests that delete the workspace themselves are
// fine: the cleanup DELETEs simply affect zero rows.
func freshWorkspace(ctx context.Context, t *testing.T) pgtype.UUID {
	t.Helper()
	q := db.New(testPool)
	var slug string
	if err := testPool.QueryRow(ctx, "SELECT 'ist-' || replace(gen_random_uuid()::text, '-', '')").Scan(&slug); err != nil {
		t.Fatalf("generate slug: %v", err)
	}
	ws, err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "issuestatus-test",
		Slug:        slug,
		IssuePrefix: "IST",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), "DELETE FROM issue_status WHERE workspace_id = $1", ws.ID)
		_, _ = testPool.Exec(context.Background(), "DELETE FROM workspace WHERE id = $1", ws.ID)
	})
	return ws.ID
}

func rawStatusCount(ctx context.Context, t *testing.T, wsID pgtype.UUID) int64 {
	t.Helper()
	var n int64
	if err := testPool.QueryRow(ctx, "SELECT COUNT(*) FROM issue_status WHERE workspace_id = $1", wsID).Scan(&n); err != nil {
		t.Fatalf("raw status count: %v", err)
	}
	return n
}

func TestEnsureSeedsBuiltinStatuses(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID := freshWorkspace(ctx, t)

	if err := Ensure(ctx, q, wsID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	statuses, err := q.ListWorkspaceIssueStatuses(ctx, db.ListWorkspaceIssueStatusesParams{WorkspaceID: wsID})
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	if len(statuses) != len(wantSystemStatuses) {
		t.Fatalf("want %d seeded statuses, got %d", len(wantSystemStatuses), len(statuses))
	}

	seenKeys := map[string]int{}
	defaultsByCategory := map[string]int{}
	for _, s := range statuses {
		if !s.SystemKey.Valid {
			t.Errorf("seeded status %q has NULL system_key (built-ins must carry a stable key)", s.Name)
			continue
		}
		seenKeys[s.SystemKey.String]++
		wantCat, ok := wantSystemStatuses[s.SystemKey.String]
		if !ok {
			t.Errorf("unexpected system_key %q", s.SystemKey.String)
			continue
		}
		if s.Category != wantCat {
			t.Errorf("system_key %q: want category %q, got %q", s.SystemKey.String, wantCat, s.Category)
		}
		if s.Name == "" || s.Icon == "" || s.Color == "" {
			t.Errorf("system_key %q: name/icon/color must be non-empty, got name=%q icon=%q color=%q", s.SystemKey.String, s.Name, s.Icon, s.Color)
		}
		if s.IsDefault {
			defaultsByCategory[s.Category]++
		}
	}
	for key := range wantSystemStatuses {
		if seenKeys[key] != 1 {
			t.Errorf("system_key %q seeded %d times, want exactly 1", key, seenKeys[key])
		}
	}
	// The seed must establish exactly one active default per Category — the "at
	// least one" side of the invariant the partial unique index cannot enforce.
	for _, cat := range []string{"backlog", "todo", "in_progress", "done", "cancelled"} {
		if defaultsByCategory[cat] != 1 {
			t.Errorf("category %q has %d default statuses, want exactly 1", cat, defaultsByCategory[cat])
		}
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID := freshWorkspace(ctx, t)

	for i := 0; i < 3; i++ {
		if err := Ensure(ctx, q, wsID); err != nil {
			t.Fatalf("Ensure call %d: %v", i+1, err)
		}
	}

	n, err := q.CountWorkspaceIssueStatuses(ctx, wsID)
	if err != nil {
		t.Fatalf("count statuses: %v", err)
	}
	if n != int64(len(wantSystemStatuses)) {
		t.Fatalf("want %d statuses after repeated Ensure, got %d (seed is not idempotent)", len(wantSystemStatuses), n)
	}
}

// TestEnsureAfterWorkspaceDeletedInsertsNothing reproduces the Backfill race:
// the workspace id is snapshotted, the workspace is then deleted, and Ensure
// runs afterward. The FOR KEY SHARE existence gate must make that seed a no-op
// so no orphan statuses are created.
func TestEnsureAfterWorkspaceDeletedInsertsNothing(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID := freshWorkspace(ctx, t)

	if err := q.DeleteWorkspace(ctx, wsID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	if err := Ensure(ctx, q, wsID); err != nil {
		t.Fatalf("Ensure on deleted workspace should be a no-op, got: %v", err)
	}
	if n := rawStatusCount(ctx, t, wsID); n != 0 {
		t.Fatalf("Ensure seeded %d statuses for a deleted workspace; want 0 (orphan guard failed)", n)
	}
}

// TestDeleteWorkspaceRemovesStatuses proves the no-FK cleanup: DeleteWorkspace
// sweeps the status catalog in the same statement as the workspace row.
func TestDeleteWorkspaceRemovesStatuses(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID := freshWorkspace(ctx, t)

	if err := Ensure(ctx, q, wsID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if n := rawStatusCount(ctx, t, wsID); n != int64(len(wantSystemStatuses)) {
		t.Fatalf("precondition: want %d statuses, got %d", len(wantSystemStatuses), n)
	}

	if err := q.DeleteWorkspace(ctx, wsID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	if n := rawStatusCount(ctx, t, wsID); n != 0 {
		t.Fatalf("DeleteWorkspace left %d orphan issue_status rows; want 0", n)
	}
}

func TestBackfillSeedsExistingWorkspace(t *testing.T) {
	ctx := context.Background()
	wsID := freshWorkspace(ctx, t)

	// The workspace exists but has no status catalog yet (Backfill is what seeds
	// pre-existing workspaces).
	if n := rawStatusCount(ctx, t, wsID); n != 0 {
		t.Fatalf("workspace unexpectedly pre-seeded: got %d statuses", n)
	}

	n, err := Backfill(ctx, testPool)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if n < 1 {
		t.Fatalf("Backfill walked %d workspaces, want >= 1", n)
	}

	if after := rawStatusCount(ctx, t, wsID); after != int64(len(wantSystemStatuses)) {
		t.Fatalf("Backfill did not seed the workspace: got %d statuses, want %d", after, len(wantSystemStatuses))
	}
}
