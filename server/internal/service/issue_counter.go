package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueNumberConstraint is the unique index that makes a workspace counter
// trailing MAX(issue.number) observable at all: the number the counter handed out
// is already persisted somewhere in the workspace.
const IssueNumberConstraint = "uq_issue_workspace_number"

// pgSQLStateUniqueViolation is Postgres' SQLSTATE for a unique constraint
// violation.
const pgSQLStateUniqueViolation = "23505"

// IssueNumberCollisionError reports the board-integrity fault behind what would
// otherwise be an opaque duplicate-key error: the number this create allocated is
// already held by another issue in the workspace, so workspace.issue_counter is
// below the highest number its issue rows carry. Allocated is the number the
// counter handed out.
type IssueNumberCollisionError struct {
	Allocated  int32
	Constraint string
}

func (e *IssueNumberCollisionError) Error() string {
	return fmt.Sprintf("issue number collision: allocated %d, already held by %s; workspace issue_counter is behind MAX(issue.number)", e.Allocated, e.Constraint)
}

// classifyIssueNumberCollision types exactly one constraint violation and leaves
// every other error alone, so no unrelated unique index gets folded into this
// meaning and no caller has to read a SQLSTATE.
func classifyIssueNumberCollision(err error, allocated int32) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgSQLStateUniqueViolation || pgErr.ConstraintName != IssueNumberConstraint {
		return err
	}
	return &IssueNumberCollisionError{Allocated: allocated, Constraint: pgErr.ConstraintName}
}

// healIssueCounterAfterCollision repairs a workspace counter that trails
// MAX(issue.number), and reports whether the caller may retry its transaction.
//
// It only acts on a proven collision, and it runs on the committed path rather
// than inside the transaction that failed: that transaction is already aborted,
// and its own increment was rolled back with the INSERT. Successful creates never
// reach this function at all.
//
// The repair is logged with the value it replaced and the value it wrote. A
// counter that is not actually behind is not repaired and not retried either —
// that collision has a different cause, and retrying it would land on the same
// number while hiding why.
func healIssueCounterAfterCollision(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, cause error) bool {
	var collision *IssueNumberCollisionError
	if !errors.As(cause, &collision) {
		return false
	}
	workspaceIDString := util.UUIDToString(workspaceID)

	row, err := q.HealIssueCounter(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		slog.Error("issue number collision without counter drift",
			"workspace_id", workspaceIDString,
			"allocated", collision.Allocated,
			"constraint", collision.Constraint)
		return false
	}
	if err != nil {
		slog.Error("issue counter repair failed",
			"workspace_id", workspaceIDString,
			"allocated", collision.Allocated,
			"error", err)
		return false
	}

	slog.Warn("issue counter repaired after issue number collision",
		"workspace_id", workspaceIDString,
		"previous_counter", row.PreviousCounter,
		"max_number", row.MaxNumber,
		"repaired_to", row.IssueCounter,
		"collision_allocated", collision.Allocated)
	return true
}
