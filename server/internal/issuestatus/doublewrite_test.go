package issuestatus

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// These tests pin the Phase 2 double-write (MUL-4809): every issue write path
// that sets the legacy status token must also populate the authoritative
// issue.status_id from the built-in status with the matching system_key.

func newUUID(ctx context.Context, t *testing.T) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := testPool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&id); err != nil {
		t.Fatalf("generate uuid: %v", err)
	}
	return id
}

func builtinStatusID(ctx context.Context, t *testing.T, q *db.Queries, wsID pgtype.UUID, systemKey string) pgtype.UUID {
	t.Helper()
	statuses, err := q.ListWorkspaceIssueStatuses(ctx, db.ListWorkspaceIssueStatusesParams{WorkspaceID: wsID})
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	for _, s := range statuses {
		if s.SystemKey.Valid && s.SystemKey.String == systemKey {
			return s.ID
		}
	}
	t.Fatalf("no built-in status with system_key %q", systemKey)
	return pgtype.UUID{}
}

func createTestIssue(ctx context.Context, t *testing.T, q *db.Queries, wsID pgtype.UUID, status string) db.Issue {
	t.Helper()
	// Mirror the handler: CreateIssue no longer derives status_id from system_key,
	// so the caller resolves the token through the catalog and double-writes the
	// pair. An unseeded workspace resolves to nothing and keeps status_id NULL.
	token := status
	var statusID pgtype.UUID
	resolved, ok, err := ResolveForWrite(ctx, q, wsID, status)
	if err != nil {
		t.Fatalf("resolve status %q: %v", status, err)
	}
	if ok {
		token = LegacyStatusToken(resolved)
		statusID = resolved.ID
	}
	iss, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: wsID,
		Title:       "double-write-test",
		Status:      token,
		StatusID:    statusID,
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   newUUID(ctx, t),
		Number:      1,
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return iss
}

func TestCreateIssueDoubleWritesStatusID(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID, _ := seededWorkspace(ctx, t)

	iss := createTestIssue(ctx, t, q, wsID, "in_progress")

	want := builtinStatusID(ctx, t, q, wsID, "in_progress")
	if !iss.StatusID.Valid || iss.StatusID != want {
		t.Fatalf("CreateIssue did not double-write status_id: got %v, want %v", iss.StatusID, want)
	}
}

func TestUpdateIssueStatusDoubleWritesStatusID(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID, _ := seededWorkspace(ctx, t)
	iss := createTestIssue(ctx, t, q, wsID, "todo")

	updated, err := q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: iss.ID, Status: "done", WorkspaceID: wsID,
	})
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	want := builtinStatusID(ctx, t, q, wsID, "done")
	if !updated.StatusID.Valid || updated.StatusID != want {
		t.Fatalf("UpdateIssueStatus did not re-derive status_id: got %v, want %v", updated.StatusID, want)
	}
}

// UpdateIssue stores the status_id its caller supplies and never derives one
// itself (MUL-4809 §6.1). Deriving it here — as this query once did, via
// system_key — could only ever reach the 7 built-ins, which is what made custom
// statuses unusable. The handler resolves through issuestatus.Resolve and passes
// the pair down, so the compat token and status_id always come from one row.
func TestUpdateIssueWritesCallerSuppliedStatusID(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID, _ := seededWorkspace(ctx, t)
	iss := createTestIssue(ctx, t, q, wsID, "todo")
	wantDone := builtinStatusID(ctx, t, q, wsID, "done")

	changed, err := q.UpdateIssue(ctx, db.UpdateIssueParams{
		ID:       iss.ID,
		Status:   pgtype.Text{String: "done", Valid: true},
		StatusID: wantDone,
	})
	if err != nil {
		t.Fatalf("update (status + status_id): %v", err)
	}
	if changed.StatusID != wantDone {
		t.Fatalf("status change: got status_id %v, want %v", changed.StatusID, wantDone)
	}
	if changed.Status != "done" {
		t.Fatalf("compat status: got %q, want done", changed.Status)
	}

	// A title-only update leaves both untouched.
	titleOnly, err := q.UpdateIssue(ctx, db.UpdateIssueParams{
		ID:    iss.ID,
		Title: pgtype.Text{String: "renamed", Valid: true},
	})
	if err != nil {
		t.Fatalf("update (title only): %v", err)
	}
	if titleOnly.StatusID != wantDone {
		t.Fatalf("title-only update changed status_id: got %v, want unchanged %v", titleOnly.StatusID, wantDone)
	}

	// The query must NOT guess: a legacy token with no status_id leaves the
	// authoritative column alone rather than silently re-deriving it. This is the
	// regression guard against re-introducing the system_key subquery.
	noID, err := q.UpdateIssue(ctx, db.UpdateIssueParams{
		ID:     iss.ID,
		Status: pgtype.Text{String: "todo", Valid: true},
	})
	if err != nil {
		t.Fatalf("update (status only): %v", err)
	}
	if noID.StatusID != wantDone {
		t.Fatalf("status-only update must not re-derive status_id: got %v, want unchanged %v", noID.StatusID, wantDone)
	}
}

// TestCreateIssueUnseededWorkspaceLeavesStatusIDNull covers the rolling-deploy
// window: before the workspace catalog is seeded, the derivation subquery
// returns no row and status_id stays NULL while status remains authoritative.
func TestCreateIssueUnseededWorkspaceLeavesStatusIDNull(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID := freshWorkspace(ctx, t) // deliberately NOT seeded

	iss := createTestIssue(ctx, t, q, wsID, "todo")
	if iss.StatusID.Valid {
		t.Fatalf("unseeded workspace: status_id should be NULL, got %v", iss.StatusID)
	}
	if iss.Status != "todo" {
		t.Fatalf("unseeded workspace: legacy status must still be written, got %q", iss.Status)
	}
}
