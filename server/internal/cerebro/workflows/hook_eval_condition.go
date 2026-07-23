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

// HookConditionFreshRunner is the optional upgrade of HookConditionResolver
// used by the eval_failed operator (FIR-3659): when the resolver can execute
// the eval fresh (issue-target evals are deterministic and token-free), the
// gate re-runs it at evaluation time so the CURRENT issue state decides —
// otherwise an agent that just fixed its issue would stay blocked on a stale
// verdict. *evals.Store satisfies this via RunForIssue.
type HookConditionFreshRunner interface {
	RunForIssue(ctx context.Context, workspaceID, actorID, evalID, issueID uuid.UUID, actorType string) (runID string, status string, err error)
}

func isDeferredHookOp(op string) bool {
	return op == OpEvalPassed || op == OpEvalFailed
}

// splitHookConditions partitions a policy's conditions into the pure operators
// (evaluated in-memory by evaluate()/match()) and the deferred operators that
// need a resolver (eval_passed, eval_failed).
func splitHookConditions(conds []Condition) (pure, deferred []Condition) {
	for _, c := range conds {
		if isDeferredHookOp(c.Op) {
			deferred = append(deferred, c)
			continue
		}
		pure = append(pure, c)
	}
	return pure, deferred
}

// resolveHookConditions evaluates the deferred conditions against the resolver.
// Every deferred condition must hold (conjunctive, like evaluate()).
//
// Fail-direction differs by operator, and the asymmetry is deliberate:
//   - eval_passed gates a policy on a recorded PASS. A nil resolver, missing
//     context, bad eval id or resolver error returns false — better to skip
//     the hook than run it on an unresolved gate.
//   - eval_failed gates a policy on the eval NOT passing. The same problems
//     resolve to true (the condition holds), so a blocking policy stays
//     blocking when the verdict cannot be established — the gate fails closed.
func resolveHookConditions(ctx context.Context, r HookConditionResolver, policy HookPolicy, conds []Condition, event HookEvent) bool {
	for _, c := range conds {
		if !isDeferredHookOp(c.Op) {
			continue
		}
		wantFailed := c.Op == OpEvalFailed
		holds := resolveEvalCondition(ctx, r, policy, c, event, wantFailed)
		if !holds {
			return false
		}
	}
	return true
}

// resolveEvalCondition resolves one eval_passed / eval_failed condition.
// wantFailed selects the operator; see resolveHookConditions for the
// fail-direction contract.
func resolveEvalCondition(ctx context.Context, r HookConditionResolver, policy HookPolicy, c Condition, event HookEvent, wantFailed bool) bool {
	if r == nil || event.WorkspaceID == "" || event.IssueID == "" {
		return wantFailed
	}
	evalID, err := uuid.Parse(uuidFromConditionValue(c.Value))
	if err != nil {
		return wantFailed
	}
	workspaceID, err := uuid.Parse(event.WorkspaceID)
	if err != nil {
		return wantFailed
	}
	issueID, err := uuid.Parse(event.IssueID)
	if err != nil {
		return wantFailed
	}
	// eval_failed re-runs the eval fresh when the resolver supports it, so the
	// verdict reflects the issue as it is NOW (the whole point of the workpad
	// start-gate: adding the workpad must unblock the very next attempt).
	if wantFailed {
		if runner, ok := r.(HookConditionFreshRunner); ok {
			if actorID, actorErr := uuid.Parse(policy.CreatedByID); actorErr == nil {
				_, status, runErr := runner.RunForIssue(ctx, workspaceID, actorID, evalID, issueID, policy.CreatedByType)
				if runErr == nil {
					return status != "passed"
				}
				// A fresh run that cannot execute (e.g. non-issue target with
				// no gateway executor wired) falls back to the stored verdict.
			}
		}
	}
	passed, err := r.EvalPassed(ctx, workspaceID, evalID, issueID)
	if err != nil {
		return wantFailed
	}
	if wantFailed {
		return !passed
	}
	return passed
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
