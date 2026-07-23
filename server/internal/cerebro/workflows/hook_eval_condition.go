package workflows

import (
	"context"

	"github.com/google/uuid"
)

// HookConditionResolver evaluates deferred (DB-backed) hook condition operators.
// It mirrors how the workflow engine's Service.conditionsHold splits pure ops
// from ops that need a database lookup. *evals.Store satisfies this via its
// EvalPassed method (an alias of LatestRunPassed); it is declared here rather
// than imported from evals because evals → loops → workflows would cycle.
type HookConditionResolver interface {
	EvalPassed(ctx context.Context, workspaceID, evalID, issueID uuid.UUID) (bool, error)
}

// splitHookConditions partitions a policy's conditions into the pure operators
// (evaluated in-memory by evaluate()/match()) and the deferred operators that
// need a resolver. Today the only deferred operator is OpEvalPassed.
func splitHookConditions(conds []Condition) (pure, deferred []Condition) {
	for _, c := range conds {
		if c.Op == OpEvalPassed {
			deferred = append(deferred, c)
			continue
		}
		pure = append(pure, c)
	}
	return pure, deferred
}

// resolveHookConditions evaluates the deferred conditions against the resolver.
// Every deferred condition must hold (conjunctive, like evaluate()). It fails
// closed: a nil resolver, a missing workspace/issue context, an unparseable
// eval id, or a resolver error all return false — better to skip the hook than
// run it on an unresolved gate.
func resolveHookConditions(ctx context.Context, r HookConditionResolver, conds []Condition, event HookEvent) bool {
	for _, c := range conds {
		if c.Op != OpEvalPassed {
			continue
		}
		if r == nil || event.WorkspaceID == "" || event.IssueID == "" {
			return false
		}
		evalID, err := uuid.Parse(uuidFromConditionValue(c.Value))
		if err != nil {
			return false
		}
		workspaceID, err := uuid.Parse(event.WorkspaceID)
		if err != nil {
			return false
		}
		issueID, err := uuid.Parse(event.IssueID)
		if err != nil {
			return false
		}
		ok, err := r.EvalPassed(ctx, workspaceID, evalID, issueID)
		if err != nil || !ok {
			return false
		}
	}
	return true
}

func uuidFromConditionValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case uuid.UUID:
		return value.String()
	default:
		return ""
	}
}
