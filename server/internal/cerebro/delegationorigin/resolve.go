// Package delegationorigin recovers the human principal behind an agent→agent
// delegation chain by walking an agent-created issue's origin chain.
//
// TECH-3629 ("agenter kan ikke starte agenter"): the comment delegation gate
// only looked for a human on the running task, the issue creator, or the latest
// member comment. It never followed the issue's `origin_type='agent_task'`
// pointer back to the task that created the issue — even though the
// "På vegne af" sidebar (handler.resolveOnBehalfOf) already resolves the human
// exactly that way. So an agent-created sub-issue whose creating run carried a
// human was still denied. This package closes that gap and walks the chain
// transitively (an agent-created issue whose creating task was itself
// agent-created, etc.) up to a bounded number of hops.
package delegationorigin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrMissingHuman signals that an agent→agent delegation chain has no human
// principal anywhere — not on the running task, not via the issue creator /
// latest member comment, and not recoverable via the issue origin chain. The
// comment handler treats this sentinel as "ask the agent owner to approve"
// rather than as a hard authentication failure. The message string is kept
// identical to the original error so existing assertions keep matching.
var ErrMissingHuman = errors.New("agent delegation denied: missing original user")

// Queries is the read-only subset of *db.Queries this resolver needs.
// *db.Queries satisfies it, and tests can supply a fake.
type Queries interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
}

// maxHops bounds the walk so a cyclic or pathological origin chain can never
// loop forever.
const maxHops = 10

// ResolveHumanViaOrigin walks the issue's origin chain
// (origin_type='agent_task' → agent_task_queue.original_user_id), and when that
// task itself has no human, continues from the task's own issue, repeating up
// to maxHops. Returns the recovered human user id and true on success, or a
// zero UUID and false when no human can be found.
func ResolveHumanViaOrigin(ctx context.Context, q Queries, issue db.Issue) (pgtype.UUID, bool) {
	if q == nil {
		return pgtype.UUID{}, false
	}
	seen := make(map[[16]byte]bool)
	cur := issue
	for i := 0; i < maxHops; i++ {
		if !cur.OriginType.Valid || cur.OriginType.String != "agent_task" || !cur.OriginID.Valid {
			return pgtype.UUID{}, false
		}
		if seen[cur.OriginID.Bytes] {
			return pgtype.UUID{}, false
		}
		seen[cur.OriginID.Bytes] = true

		task, err := q.GetAgentTask(ctx, cur.OriginID)
		if err != nil {
			return pgtype.UUID{}, false
		}
		if task.OriginalUserID.Valid {
			return task.OriginalUserID, true
		}
		if !task.IssueID.Valid {
			return pgtype.UUID{}, false
		}
		next, err := q.GetIssue(ctx, task.IssueID)
		if err != nil {
			return pgtype.UUID{}, false
		}
		cur = next
	}
	return pgtype.UUID{}, false
}
