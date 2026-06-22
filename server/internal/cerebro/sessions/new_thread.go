package sessions

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// OpenSessionForNewThread realises FIR-1787 point 5 / FIR-1741 Slice 2 under
// Jesper's decision A: each new root comment (a new thread) starts its own
// session, so the agent run that thread triggers gets a fresh context window
// (Sara's session-scoped engine in issue_context_session_scope_cerebro.go)
// instead of inheriting the previous thread's context.
//
// It is called once per created root comment, with `at` = the comment's
// created_at. It is a deliberate no-op for the FIRST root comment on an issue —
// the implicit "Session 1" (zero markers = whole timeline) already covers it.
// From the second thread onward it closes the open session and opens a new one
// whose created_at == the comment time, so timeline grouping (created_at order,
// see grouping.ts) places the new thread, and only it, inside the new session.
//
// Concurrency and idempotency mirror StartFresh: a transaction-scoped advisory
// lock serialises racing root comments on the same issue, and the unique
// (issue_id, position) index is the DB backstop. New-thread sessions carry no
// handoff — decision A is "fresh context", so nothing is inherited and the
// closing session is left untouched (an explicit Handoff is still the way to
// brief/name a session, point 2 / 2b).
//
// The caller is responsible for gating on the cerebro_comment_chapters flag and
// for skipping non-thread surfaces (channels); this function assumes it should
// run and only decides first-thread vs subsequent-thread.
func OpenSessionForNewThread(ctx context.Context, tx pgx.Tx, issueID pgtype.UUID, issueCreatedAt time.Time, at time.Time) error {
	// Serialise concurrent session opens on this issue (same lock key as
	// StartFresh, so a manual Start fresh and an auto new-thread open cannot
	// interleave into duplicate / multiple open sessions — the P1 failure class).
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, issueID); err != nil {
		return err
	}

	// First root comment ever → implicit Session 1 covers it; write nothing.
	var rootCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*)::int FROM comment WHERE issue_id = $1 AND parent_id IS NULL`,
		issueID).Scan(&rootCount); err != nil {
		return err
	}
	if rootCount <= 1 {
		return nil
	}

	var sessionCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*)::int FROM cerebro_session WHERE issue_id = $1`, issueID).Scan(&sessionCount); err != nil {
		return err
	}

	// FIR-1787 fix: the new session marker MUST be placed at the comment time
	// floored to the whole second. The comment API serialises created_at with
	// time.RFC3339 (second precision, util.TimestampToString) while the sessions
	// API serialises it as RFC3339Nano (sub-second). If we marked the session at
	// the comment's sub-second instant (e.g. .793), the frontend — which sees the
	// comment truncated to .000 of that second — would sort the comment BEFORE its
	// own marker, dropping it back into the previous session and leaving this
	// session empty (so it never renders). Flooring to the second makes the marker
	// equal the comment's frontend-visible time, so the comment lands in it.
	atSec := at.Truncate(time.Second)

	if sessionCount == 0 {
		// Second thread on an issue with no markers yet: materialise Session 1
		// covering the whole prior timeline (issue creation → now) and open
		// Session 2 at this comment, so the first thread stays in Session 1 and
		// this thread begins Session 2.
		if _, err := createSession(ctx, tx, issueID, 1, "Session 1", "done", nil, &issueCreatedAt); err != nil {
			return err
		}
		if _, err := createSession(ctx, tx, issueID, 2, "Session 2", "in_progress", nil, &atSec); err != nil {
			return err
		}
		return nil
	}

	// Close the currently open session (latest not-done), if any.
	var openID pgtype.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM cerebro_session
		WHERE issue_id = $1 AND status <> 'done'
		ORDER BY position DESC, created_at DESC, id DESC
		LIMIT 1`, issueID).Scan(&openID)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_session SET status = 'done', updated_at = now()
			WHERE id = $1 AND issue_id = $2`, openID, issueID); err != nil {
			return err
		}
	}

	var nextPos int32
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(position), 0)::int + 1 FROM cerebro_session WHERE issue_id = $1`,
		issueID).Scan(&nextPos); err != nil {
		return err
	}
	if _, err := createSession(ctx, tx, issueID, nextPos, "Session "+strconv.Itoa(int(nextPos)), "in_progress", nil, &atSec); err != nil {
		return err
	}
	return nil
}
