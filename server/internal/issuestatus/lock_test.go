package issuestatus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCreateCustomStatusAfterWorkspaceDeletedInsertsNothing is the create-side
// mirror of the seed orphan guard (P0, MUL-4809 review): the FOR KEY SHARE
// existence gate in CreateCustomIssueStatus must insert zero rows for a
// workspace that has since been deleted, so a create that raced a workspace
// delete can never leave an orphan status behind. Zero rows surfaces as
// pgx.ErrNoRows to the caller.
func TestCreateCustomStatusAfterWorkspaceDeletedInsertsNothing(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID := freshWorkspace(ctx, t)

	if err := Ensure(ctx, q, wsID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// A live workspace accepts a custom status.
	if _, err := q.CreateCustomIssueStatus(ctx, db.CreateCustomIssueStatusParams{
		WorkspaceID: wsID, Name: "Live Stage", Description: "", Icon: "in_progress", Color: "warning", Category: "in_progress",
	}); err != nil {
		t.Fatalf("create on live workspace: %v", err)
	}

	if err := q.DeleteWorkspace(ctx, wsID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	// After the workspace is gone the existence gate must reject the insert.
	_, err := q.CreateCustomIssueStatus(ctx, db.CreateCustomIssueStatusParams{
		WorkspaceID: wsID, Name: "Orphan Stage", Description: "", Icon: "in_progress", Color: "warning", Category: "in_progress",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("create on deleted workspace: want pgx.ErrNoRows, got %v", err)
	}
	if n := rawStatusCount(ctx, t, wsID); n != 0 {
		t.Fatalf("create seeded %d orphan statuses for a deleted workspace; want 0", n)
	}
}

// TestWorkspaceLockKeyCanonicalizesUUID proves the lock key is derived from the
// canonical UUID bytes, not a raw request string: the same workspace reached via
// different textual UUID forms (upper vs lower hex) must map to the SAME key, so
// two case variants cannot take two different advisory locks and bypass mutual
// exclusion (P0, MUL-4809 review).
func TestWorkspaceLockKeyCanonicalizesUUID(t *testing.T) {
	lower, err := util.ParseUUID("a1b2c3d4-5e6f-4a8b-9c0d-1e2f3a4b5c6d")
	if err != nil {
		t.Fatalf("parse lower: %v", err)
	}
	upper, err := util.ParseUUID("A1B2C3D4-5E6F-4A8B-9C0D-1E2F3A4B5C6D")
	if err != nil {
		t.Fatalf("parse upper: %v", err)
	}
	if got, want := WorkspaceLockKey(upper), WorkspaceLockKey(lower); got != want {
		t.Fatalf("differently-cased UUIDs produced different lock keys: %q vs %q", got, want)
	}
	// A different workspace must not collide.
	other, err := util.ParseUUID("00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("parse other: %v", err)
	}
	if WorkspaceLockKey(other) == WorkspaceLockKey(lower) {
		t.Fatal("distinct workspaces produced the same lock key")
	}
}

// TestLockWorkspaceForStatusWriteSerializes is the controlled-concurrency proof
// that the shared advisory lock actually serializes two transactions on the same
// workspace: while tx1 holds it, tx2's acquire blocks; once tx1 releases, tx2
// proceeds. This is the protocol the catalog CRUD and archive migration share.
func TestLockWorkspaceForStatusWriteSerializes(t *testing.T) {
	ctx := context.Background()
	wsID := freshWorkspace(ctx, t)

	tx1, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback(ctx)
	if err := LockWorkspaceForStatusWrite(ctx, tx1, wsID); err != nil {
		t.Fatalf("tx1 acquire: %v", err)
	}

	acquired := make(chan error, 1)
	go func() {
		tx2, err := testPool.Begin(context.Background())
		if err != nil {
			acquired <- err
			return
		}
		defer tx2.Rollback(context.Background())
		// Blocks until tx1 releases the workspace lock.
		acquired <- LockWorkspaceForStatusWrite(context.Background(), tx2, wsID)
	}()

	// While tx1 holds the lock, tx2 must stay blocked.
	select {
	case err := <-acquired:
		t.Fatalf("tx2 acquired the lock while tx1 held it (err=%v); lock did not serialize", err)
	case <-time.After(300 * time.Millisecond):
	}

	// Releasing tx1 must let tx2 acquire promptly.
	if err := tx1.Rollback(ctx); err != nil {
		t.Fatalf("tx1 release: %v", err)
	}
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("tx2 acquire after tx1 release: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tx2 did not acquire the lock after tx1 released it")
	}
}

// TestArchiveMigrationClosesUnderSharedLock is the archive/assignment closure
// proof (P0, MUL-4809 review). It pins the dangerous interleave — a status
// re-point that lands AFTER the archive census — and shows the shared lock plus
// the assignment write's `archived_at IS NULL` guard prevent an issue from being
// stranded on an archived status. The re-point statement is the contract any
// future custom-status assignment path must follow: take
// LockWorkspaceForStatusWrite, then write only onto an active status.
func TestArchiveMigrationClosesUnderSharedLock(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID := freshWorkspace(ctx, t)
	if err := Ensure(ctx, q, wsID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// Two custom in_progress statuses: A gets archived, B is the migrate target.
	statusA, err := q.CreateCustomIssueStatus(ctx, db.CreateCustomIssueStatusParams{
		WorkspaceID: wsID, Name: "Stage A", Icon: "in_progress", Color: "warning", Category: "in_progress",
	})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	statusB, err := q.CreateCustomIssueStatus(ctx, db.CreateCustomIssueStatusParams{
		WorkspaceID: wsID, Name: "Stage B", Icon: "in_progress", Color: "warning", Category: "in_progress",
	})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	// An issue currently points at A via the authoritative status_id.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, status_id, priority, creator_type, creator_id, number)
		VALUES ($1, 'closure', 'in_progress', $2, 'none', 'member', gen_random_uuid(),
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id::text
	`, wsID, statusA.ID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	// tx1 (archive migration) takes the shared lock and holds it.
	tx1, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback(ctx)
	if err := LockWorkspaceForStatusWrite(ctx, tx1, wsID); err != nil {
		t.Fatalf("tx1 acquire: %v", err)
	}

	// tx2 (a future assignment write) tries to re-point the issue back onto A,
	// but blocks on the shared lock until the archive commits.
	repointDone := make(chan error, 1)
	go func() {
		tx2, err := testPool.Begin(context.Background())
		if err != nil {
			repointDone <- err
			return
		}
		defer tx2.Rollback(context.Background())
		if err := LockWorkspaceForStatusWrite(context.Background(), tx2, wsID); err != nil {
			repointDone <- err
			return
		}
		// The assignment write only lands on an ACTIVE (non-archived) status.
		_, err = tx2.Exec(context.Background(), `
			UPDATE issue SET status_id = $2, updated_at = now()
			WHERE id = $1
			  AND EXISTS (SELECT 1 FROM issue_status WHERE id = $2 AND workspace_id = $3 AND archived_at IS NULL)
		`, issueID, statusA.ID, wsID)
		if err != nil {
			repointDone <- err
			return
		}
		repointDone <- tx2.Commit(context.Background())
	}()

	// The re-point must stay blocked while the archive holds the lock.
	select {
	case err := <-repointDone:
		t.Fatalf("re-point completed while archive held the lock (err=%v); lock did not serialize", err)
	case <-time.After(200 * time.Millisecond):
	}

	// Archive migration under the lock: reassign A->B, archive A, commit.
	qtx := q.WithTx(tx1)
	if err := qtx.ReassignIssuesStatus(ctx, db.ReassignIssuesStatusParams{WorkspaceID: wsID, FromStatusID: statusA.ID, ToStatusID: statusB.ID}); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if _, err := qtx.ArchiveIssueStatus(ctx, db.ArchiveIssueStatusParams{ID: statusA.ID, WorkspaceID: wsID}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("archive commit: %v", err)
	}

	// The re-point now unblocks; its guard turns it into a no-op (A is archived).
	if err := <-repointDone; err != nil {
		t.Fatalf("re-point tx: %v", err)
	}

	// Invariant: the issue is on B (active), never stranded on the archived A.
	var finalStatusID string
	if err := testPool.QueryRow(ctx, `SELECT status_id::text FROM issue WHERE id = $1`, issueID).Scan(&finalStatusID); err != nil {
		t.Fatalf("read final status_id: %v", err)
	}
	if finalStatusID != uuidToString(statusB.ID) {
		t.Fatalf("issue stranded on archived status: status_id = %s, want migrated to B %s", finalStatusID, uuidToString(statusB.ID))
	}
}

// TestDefaultCreateAndWorkspaceDeleteNoDeadlock is the lock-order regression
// guard (P0 follow-up, MUL-4809 review). It reproduces the exact interleave that
// deadlocked before the fix: a create with is_default=true holds a status row
// (ClearCategoryDefault) while a concurrent DeleteWorkspace holds the workspace
// row FOR UPDATE, and only then does the create reach for the workspace row. If
// the create took the workspace lock at that point (inside the INSERT), the two
// would deadlock (40P01).
//
// LockWorkspaceForStatusWrite now takes the workspace row FOR KEY SHARE FIRST, so
// the create already owns the workspace lock before it touches a status row: the
// concurrent delete blocks on FOR KEY SHARE, the create finishes, and the delete
// then sweeps cleanly with no orphan. The interleave is arranged so that
// reintroducing the old order (workspace lock taken only in the INSERT) makes the
// CreateCustomIssueStatus step below deadlock and fails this test.
func TestDefaultCreateAndWorkspaceDeleteNoDeadlock(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	wsID := freshWorkspace(ctx, t)
	if err := Ensure(ctx, q, wsID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// tx1 mirrors the create-with-default flow up to (but not including) the row
	// insert: shared status-write locks first, then clear the current default,
	// which locks a status row.
	tx1, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback(ctx)
	if err := LockWorkspaceForStatusWrite(ctx, tx1, wsID); err != nil {
		t.Fatalf("tx1 status locks: %v", err)
	}
	q1 := q.WithTx(tx1)
	if err := q1.ClearCategoryDefault(ctx, db.ClearCategoryDefaultParams{WorkspaceID: wsID, Category: "todo"}); err != nil {
		t.Fatalf("tx1 clear default: %v", err)
	}

	// tx2 (workspace delete) races in NOW, while tx1 holds a status row but has
	// not yet inserted: it grabs the workspace row FOR UPDATE then sweeps the
	// catalog. `started` fires just before it reaches for the workspace lock.
	started := make(chan struct{})
	deleteDone := make(chan error, 1)
	go func() {
		tx2, err := testPool.Begin(context.Background())
		if err != nil {
			close(started)
			deleteDone <- err
			return
		}
		defer tx2.Rollback(context.Background())
		q2 := q.WithTx(tx2)
		close(started)
		if _, err := q2.LockWorkspaceForDelete(context.Background(), wsID); err != nil {
			deleteDone <- err
			return
		}
		if err := q2.DeleteWorkspace(context.Background(), wsID); err != nil {
			deleteDone <- err
			return
		}
		deleteDone <- tx2.Commit(context.Background())
	}()

	// Let tx2's workspace-lock request reach Postgres (it would acquire on the old
	// order, or block on FOR KEY SHARE on the fixed one) before tx1 inserts.
	<-started
	time.Sleep(250 * time.Millisecond)

	// The insert reaches for the workspace row (FOR KEY SHARE in its CTE). On the
	// fixed order tx1 already holds it, so this proceeds; on the old order it
	// would block on tx2's FOR UPDATE and deadlock (40P01) here.
	if _, err := q1.CreateCustomIssueStatus(ctx, db.CreateCustomIssueStatusParams{
		WorkspaceID: wsID, Name: "Triage", Icon: "todo", Color: "warning", Category: "todo", IsDefault: true,
	}); err != nil {
		t.Fatalf("tx1 create default deadlocked or failed (lock-order regression?): %v", err)
	}

	// The delete must still be blocked, waiting on tx1's FOR KEY SHARE.
	select {
	case err := <-deleteDone:
		t.Fatalf("workspace delete completed while the create still held the lock (err=%v); expected it to wait", err)
	case <-time.After(150 * time.Millisecond):
	}

	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("tx1 commit failed: %v", err)
	}
	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatalf("workspace delete errored (want clean, no deadlock): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("workspace delete never completed after the create committed")
	}

	// No orphan catalog rows survive the delete, and the workspace is gone.
	if n := rawStatusCount(ctx, t, wsID); n != 0 {
		t.Fatalf("workspace delete left %d orphan issue_status rows; want 0", n)
	}
	var exists bool
	if err := testPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM workspace WHERE id = $1)", wsID).Scan(&exists); err != nil {
		t.Fatalf("check workspace existence: %v", err)
	}
	if exists {
		t.Fatal("workspace still exists after its delete committed")
	}
}
