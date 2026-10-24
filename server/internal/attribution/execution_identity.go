package attribution

import (
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ExecutionIdentity distinguishes a requesting human from an armed automation's
// accountable principal. It does not confer access to external credentials.
type ExecutionIdentity struct {
	Kind   string
	UserID pgtype.UUID
}

// ResolveExecutionIdentity follows retry/delegation lineage using a lookup that
// MUST be scoped to the current task's workspace. Manual reruns are new roots.
func ResolveExecutionIdentity(task db.AgentTaskQueue, lookup func(pgtype.UUID) (db.AgentTaskQueue, error)) (ExecutionIdentity, error) {
	seen := map[pgtype.UUID]bool{}
	originator := task.OriginatorUserID
	for range 32 {
		if !task.ID.Valid || seen[task.ID] || task.OriginatorUserID != originator {
			break
		}
		seen[task.ID] = true
		parent := task.RetryOfTaskID
		if !parent.Valid {
			switch Source(task.OriginatorSource.String) {
			case SourceDirectHuman:
				if originator.Valid {
					return ExecutionIdentity{Kind: "human", UserID: originator}, nil
				}
			case SourceTriggerOwner, SourceRuleOwner:
				return ExecutionIdentity{Kind: "automation"}, nil
			case SourceDelegation, SourceCommentSource:
				parent = task.DelegatedFromTaskID
			}
		}
		if !parent.Valid {
			break
		}
		var err error
		task, err = lookup(parent)
		if err != nil {
			break
		}
	}
	return ExecutionIdentity{}, errors.New("execution identity could not be proven")
}
