package cerebrodb

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// ReclassifyAutoPauseFailure updates the coarse task failure reason after an
// auto-pause has identified the provider blocker. The guard preserves
// idempotency and avoids overwriting more specific classifications.
func (q *Queries) ReclassifyAutoPauseFailure(ctx context.Context, id pgtype.UUID, failureReason string) error {
	_, err := q.db.Exec(ctx, `
UPDATE agent_task_queue
SET failure_reason = $2
WHERE id = $1
  AND failure_reason = 'agent_error'`,
		id, failureReason)
	return err
}
