package sessions

// FIR-1787 point 5 / FIR-1741 Slice 2 — DB tests for OpenSessionForNewThread:
// "a new thread starts a new session." Skip cleanly when no test DB is reachable,
// same pattern as handler_db_test.go.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// insertRootComment writes a top-level comment on the issue and returns its
// created_at, the value the comment-create path passes to OpenSessionForNewThread.
func insertRootComment(t *testing.T, issueID, workspaceID string) time.Time {
	t.Helper()
	ctx := context.Background()
	var createdAt time.Time
	err := sessTestPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1::uuid, $2::uuid, 'member', gen_random_uuid(), 'root', 'comment')
		RETURNING created_at`, issueID, workspaceID).Scan(&createdAt)
	if err != nil {
		t.Fatalf("insert root comment: %v", err)
	}
	return createdAt
}

func issueCreatedAt(t *testing.T, issueID string) time.Time {
	t.Helper()
	var at time.Time
	if err := sessTestPool.QueryRow(context.Background(),
		`SELECT created_at FROM issue WHERE id = $1::uuid`, issueID).Scan(&at); err != nil {
		t.Fatalf("issue created_at: %v", err)
	}
	return at
}

func openForNewThread(t *testing.T, issueID string, issueAt, commentAt time.Time) {
	t.Helper()
	ctx := context.Background()
	tx, err := sessTestPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	var id pgtype.UUID
	if perr := id.Scan(issueID); perr != nil {
		t.Fatalf("parse issue id: %v", perr)
	}
	if err := OpenSessionForNewThread(ctx, tx, id, issueAt, commentAt); err != nil {
		t.Fatalf("OpenSessionForNewThread: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func countSessions(t *testing.T, issueID string) (total, open int) {
	t.Helper()
	if err := sessTestPool.QueryRow(context.Background(), `
		SELECT COUNT(*)::int, COUNT(*) FILTER (WHERE status <> 'done')::int
		FROM cerebro_session WHERE issue_id = $1::uuid`, issueID).Scan(&total, &open); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return total, open
}

// TestNewThreadOpensSession walks the lifecycle: the first thread writes no
// marker (implicit Session 1), the second thread materialises Session 1 + opens
// Session 2, and each later thread closes the open session and opens the next —
// always exactly one open session, positions never colliding.
func TestNewThreadOpensSession(t *testing.T) {
	issueID, workspaceID := seedIssue(t)
	if sessTestPool == nil {
		t.Skip("no test DB")
	}
	issueAt := issueCreatedAt(t, issueID)

	// Thread 1: no session row yet — implicit Session 1 covers it.
	c1 := insertRootComment(t, issueID, workspaceID)
	openForNewThread(t, issueID, issueAt, c1)
	if total, _ := countSessions(t, issueID); total != 0 {
		t.Fatalf("first thread should write no marker, got %d sessions", total)
	}

	// Thread 2: Session 1 (done, covers thread 1) + Session 2 (open).
	c2 := insertRootComment(t, issueID, workspaceID)
	openForNewThread(t, issueID, issueAt, c2)
	if total, open := countSessions(t, issueID); total != 2 || open != 1 {
		t.Fatalf("second thread: want 2 sessions / 1 open, got %d / %d", total, open)
	}

	// Thread 3: Session 2 closes, Session 3 opens.
	c3 := insertRootComment(t, issueID, workspaceID)
	openForNewThread(t, issueID, issueAt, c3)
	total, open := countSessions(t, issueID)
	if total != 3 || open != 1 {
		t.Fatalf("third thread: want 3 sessions / 1 open, got %d / %d", total, open)
	}

	// The open session must be the newest (position 3) and window at thread 3.
	var openPos int32
	var openCreated time.Time
	if err := sessTestPool.QueryRow(context.Background(), `
		SELECT position, created_at FROM cerebro_session
		WHERE issue_id = $1::uuid AND status <> 'done'`, issueID).Scan(&openPos, &openCreated); err != nil {
		t.Fatalf("load open session: %v", err)
	}
	if openPos != 3 {
		t.Fatalf("open session position: want 3, got %d", openPos)
	}
	if openCreated.Unix() != c3.Unix() {
		t.Fatalf("open session created_at should equal thread-3 comment time")
	}

	// FIR-1787 regression: the frontend groups a comment into the latest session
	// whose start <= the comment's created_at, but it sees the comment's
	// created_at truncated to the whole second (comment API = time.RFC3339) while
	// the session marker keeps sub-second precision (sessions API = RFC3339Nano).
	// So the active session's marker MUST be <= the comment's second-truncated
	// time, or the comment sorts before its own marker, drops into the previous
	// session, and the active session renders empty (the "active session doesn't
	// work" bug Jesper hit). Before the floor-to-second fix, openCreated carried
	// sub-seconds and this assertion fails.
	commentAsFrontendSees := c3.Truncate(time.Second)
	if openCreated.After(commentAsFrontendSees) {
		t.Fatalf("active session marker %v is AFTER the comment's second-truncated time %v — comment would fall into the previous session and the active session renders empty", openCreated, commentAsFrontendSees)
	}
}
