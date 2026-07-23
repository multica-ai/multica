package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeEvalResolver struct {
	passed bool
	err    error
	calls  int
}

func (f *fakeEvalResolver) EvalPassed(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	f.calls++
	return f.passed, f.err
}

func evalPassedCondition(evalID uuid.UUID) Condition {
	return Condition{Field: "eval", Op: OpEvalPassed, Value: evalID.String()}
}

func TestSplitHookConditions(t *testing.T) {
	pure, deferred := splitHookConditions([]Condition{
		{Field: "issue.status", Op: "eq", Value: "in_review"},
		evalPassedCondition(uuid.New()),
	})
	if len(pure) != 1 || pure[0].Op != "eq" {
		t.Fatalf("pure = %#v", pure)
	}
	if len(deferred) != 1 || deferred[0].Op != OpEvalPassed {
		t.Fatalf("deferred = %#v", deferred)
	}
}

func TestResolveHookConditions(t *testing.T) {
	ws, evalID, issue := uuid.New(), uuid.New(), uuid.New()
	event := HookEvent{WorkspaceID: ws.String(), IssueID: issue.String()}
	conds := []Condition{evalPassedCondition(evalID)}

	t.Run("resolver true", func(t *testing.T) {
		r := &fakeEvalResolver{passed: true}
		if !resolveHookConditions(context.Background(), r, conds, event) {
			t.Fatal("expected true when resolver passes")
		}
		if r.calls != 1 {
			t.Fatalf("resolver calls = %d", r.calls)
		}
	})

	t.Run("resolver false", func(t *testing.T) {
		if resolveHookConditions(context.Background(), &fakeEvalResolver{passed: false}, conds, event) {
			t.Fatal("expected false when resolver rejects")
		}
	})

	t.Run("resolver error fails closed", func(t *testing.T) {
		if resolveHookConditions(context.Background(), &fakeEvalResolver{err: errors.New("boom")}, conds, event) {
			t.Fatal("expected false on resolver error")
		}
	})

	t.Run("nil resolver fails closed", func(t *testing.T) {
		if resolveHookConditions(context.Background(), nil, conds, event) {
			t.Fatal("expected false with nil resolver")
		}
	})

	t.Run("no deferred conditions is vacuously true", func(t *testing.T) {
		if !resolveHookConditions(context.Background(), nil, nil, event) {
			t.Fatal("expected true with no deferred conditions")
		}
	})

	t.Run("missing issue context fails closed", func(t *testing.T) {
		noIssue := HookEvent{WorkspaceID: ws.String()}
		if resolveHookConditions(context.Background(), &fakeEvalResolver{passed: true}, conds, noIssue) {
			t.Fatal("expected false without issue context")
		}
	})
}

func TestHookEngineEvalPassedConditionGatesActions(t *testing.T) {
	ws, evalID, issue := uuid.New(), uuid.New(), uuid.New()
	policy := newTestHookPolicy("eval-cond-policy", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeIssue, ID: issue.String()})
	policy.Conditions = []Condition{
		{Field: "issue.status", Op: "eq", Value: "in_review"},
		evalPassedCondition(evalID),
	}
	policy.Handlers[0].Actions = []HookAction{{Type: "audit.record", Config: map[string]any{"event": "eval_ok"}}}
	event := HookEvent{
		EventID: "eval-cond-event", Type: HookBeforeTaskComplete,
		WorkspaceID: ws.String(), IssueID: issue.String(),
		Context: map[string]any{"issue": map[string]any{"status": "in_review"}},
	}

	run := func(passed bool) HookResult {
		registry := NewActionRegistry()
		registry.Register("audit.record", func(context.Context, ActionInvocation) (map[string]any, error) { return map[string]any{"ok": true}, nil })
		engine := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).
			WithActionRegistry(registry).
			WithConditionResolver(&fakeEvalResolver{passed: passed})
		result, err := engine.Evaluate(context.Background(), event)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	if got := run(false); len(got.Matches) != 0 || len(got.ActionResults) != 0 {
		t.Fatalf("failing eval condition must skip the policy: matches=%d actions=%d", len(got.Matches), len(got.ActionResults))
	}
	if got := run(true); len(got.Matches) == 0 || len(got.ActionResults) != 1 {
		t.Fatalf("passing eval condition must run the action: matches=%d actions=%d", len(got.Matches), len(got.ActionResults))
	}
}
