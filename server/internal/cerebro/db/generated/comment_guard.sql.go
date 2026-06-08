// Hand-written counterpart to comment_guard.sql — run `make sqlc` to
// regenerate. Kept in sync with the sqlc output style so the output
// is identical after regeneration. TECH-3099.

package cerebrodb

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const hasTaskPostedOnIssue = `-- name: HasTaskPostedOnIssue :one
SELECT EXISTS(
    SELECT 1 FROM cerebro_comment_task
    WHERE task_id = $1 AND issue_id = $2
) AS posted`

type HasTaskPostedOnIssueParams struct {
	TaskID  pgtype.UUID `json:"task_id"`
	IssueID pgtype.UUID `json:"issue_id"`
}

// HasTaskPostedOnIssue returns true when the given task has at least one
// cerebro_comment_task row for the given issue. Used by the sub-issue
// no-split-session guard (TECH-3099).
func (q *Queries) HasTaskPostedOnIssue(ctx context.Context, arg HasTaskPostedOnIssueParams) (bool, error) {
	row := q.db.QueryRow(ctx, hasTaskPostedOnIssue, arg.TaskID, arg.IssueID)
	var posted bool
	err := row.Scan(&posted)
	return posted, err
}
