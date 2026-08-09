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
		if !resolveHookConditions(context.Background(), r, HookPolicy{}, conds, event) {
			t.Fatal("expected true when resolver passes")
		}
		if r.calls != 1 {
			t.Fatalf("resolver calls = %d", r.calls)
		}
	})

	t.Run("resolver false", func(t *testing.T) {
		if resolveHookConditions(context.Background(), &fakeEvalResolver{passed: false}, HookPolicy{}, conds, event) {
			t.Fatal("expected false when resolver rejects")
		}
	})

	t.Run("resolver error fails closed", func(t *testing.T) {
		if resolveHookConditions(context.Background(), &fakeEvalResolver{err: errors.New("boom")}, HookPolicy{}, conds, event) {
			t.Fatal("expected false on resolver error")
		}
	})

	t.Run("nil resolver fails closed", func(t *testing.T) {
		if resolveHookConditions(context.Background(), nil, HookPolicy{}, conds, event) {
			t.Fatal("expected false with nil resolver")
		}
	})

	t.Run("no deferred conditions is vacuously true", func(t *testing.T) {
		if !resolveHookConditions(context.Background(), nil, HookPolicy{}, nil, event) {
			t.Fatal("expected true with no deferred conditions")
		}
	})

	t.Run("missing issue context fails closed", func(t *testing.T) {
		noIssue := HookEvent{WorkspaceID: ws.String()}
		if resolveHookConditions(context.Background(), &fakeEvalResolver{passed: true}, HookPolicy{}, conds, noIssue) {
			t.Fatal("expected false without issue context")
		}
	})
}

type fakeFreshRunner struct {
	fakeEvalResolver
	runStatus string
	runErr    error
	runCalls  int
}

func (f *fakeFreshRunner) RunForIssue(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, string) (string, string, error) {
	f.runCalls++
	return "run-id", f.runStatus, f.runErr
}

func evalFailedCondition(evalID uuid.UUID) Condition {
	return Condition{Field: "eval", Op: OpEvalFailed, Value: evalID.String()}
}

func TestResolveEvalFailedCondition(t *testing.T) {
	ws, evalID, issue := uuid.New(), uuid.New(), uuid.New()
	event := HookEvent{WorkspaceID: ws.String(), IssueID: issue.String()}
	conds := []Condition{evalFailedCondition(evalID)}
	policy := HookPolicy{CreatedByID: uuid.New().String(), CreatedByType: "member"}

	t.Run("eval failed holds", func(t *testing.T) {
		if !resolveHookConditions(context.Background(), &fakeEvalResolver{passed: false}, policy, conds, event) {
			t.Fatal("expected eval_failed to hold when eval fails")
		}
	})

	t.Run("eval passed does not hold", func(t *testing.T) {
		if resolveHookConditions(context.Background(), &fakeEvalResolver{passed: true}, policy, conds, event) {
			t.Fatal("expected eval_failed not to hold when eval passes")
		}
	})

	t.Run("nil resolver fails closed toward the gate", func(t *testing.T) {
		if !resolveHookConditions(context.Background(), nil, policy, conds, event) {
			t.Fatal("expected eval_failed to hold with nil resolver")
		}
	})

	t.Run("resolver error fails closed toward the gate", func(t *testing.T) {
		if !resolveHookConditions(context.Background(), &fakeEvalResolver{err: errors.New("boom")}, policy, conds, event) {
			t.Fatal("expected eval_failed to hold on resolver error")
		}
	})

	t.Run("fresh run passed unblocks immediately", func(t *testing.T) {
		r := &fakeFreshRunner{fakeEvalResolver: fakeEvalResolver{passed: false}, runStatus: "passed"}
		if resolveHookConditions(context.Background(), r, policy, conds, event) {
			t.Fatal("fresh passing run must override the stale stored verdict")
		}
		if r.runCalls != 1 {
			t.Fatalf("fresh run calls = %d", r.runCalls)
		}
	})

	t.Run("fresh run failed holds", func(t *testing.T) {
		r := &fakeFreshRunner{fakeEvalResolver: fakeEvalResolver{passed: true}, runStatus: "failed"}
		if !resolveHookConditions(context.Background(), r, policy, conds, event) {
			t.Fatal("fresh failing run must hold the gate")
		}
	})

	t.Run("fresh run error falls back to stored verdict", func(t *testing.T) {
		r := &fakeFreshRunner{fakeEvalResolver: fakeEvalResolver{passed: true}, runErr: errors.New("no executor")}
		if resolveHookConditions(context.Background(), r, policy, conds, event) {
			t.Fatal("stored pass must clear the gate when the fresh run cannot execute")
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
		registry.Register("audit.record", func(context.Context, ActionInvocation) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		})
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

func TestHookEngineAnyMatchesAcrossPureAndDeferredConditions(t *testing.T) {
	ws, evalID, issue := uuid.New(), uuid.New(), uuid.New()
	policy := newTestHookPolicy("any-mixed-policy", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeIssue, ID: issue.String()})
	policy.ConditionMode = HookConditionAny
	policy.Conditions = []Condition{
		{Field: "issue.id", Op: "eq", Value: "does-not-match"},
		evalPassedCondition(evalID),
	}

	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).
		WithConditionResolver(&fakeEvalResolver{passed: true}).
		Evaluate(context.Background(), HookEvent{
			EventID:     "any-mixed-event",
			Type:        HookBeforeTaskComplete,
			WorkspaceID: ws.String(),
			IssueID:     issue.String(),
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("matches = %d, want 1 when deferred condition satisfies any", len(result.Matches))
	}
	if len(result.MatchedConditions) != 1 || result.MatchedConditions[0].Op != OpEvalPassed {
		t.Fatalf("matched conditions = %#v, want only the true deferred condition", result.MatchedConditions)
	}
}

func TestHookConditionModeShortCircuitsInStoredOrder(t *testing.T) {
	ws, evalID, issue := uuid.New(), uuid.New(), uuid.New()
	event := HookEvent{WorkspaceID: ws.String(), IssueID: issue.String()}
	falsePure := Condition{Field: "issue.id", Op: "eq", Value: "does-not-match"}
	truePure := Condition{Field: "issue.id", Op: "eq", Value: issue.String()}
	deferred := evalPassedCondition(evalID)

	t.Run("any skips deferred after a pure match", func(t *testing.T) {
		resolver := &fakeEvalResolver{passed: false}
		matches, matched := evaluateHookPolicyConditions(context.Background(), resolver, HookPolicy{
			ConditionMode: HookConditionAny,
			Conditions:    []Condition{truePure, deferred},
		}, event)
		if !matches || len(matched) != 1 || resolver.calls != 0 {
			t.Fatalf("matches=%v matched=%#v resolver calls=%d", matches, matched, resolver.calls)
		}
	})

	t.Run("all skips deferred after a pure miss", func(t *testing.T) {
		resolver := &fakeEvalResolver{passed: true}
		matches, _ := evaluateHookPolicyConditions(context.Background(), resolver, HookPolicy{
			ConditionMode: HookConditionAll,
			Conditions:    []Condition{falsePure, deferred},
		}, event)
		if matches || resolver.calls != 0 {
			t.Fatalf("matches=%v resolver calls=%d", matches, resolver.calls)
		}
	})

	t.Run("any returns false when every condition misses", func(t *testing.T) {
		resolver := &fakeEvalResolver{passed: false}
		matches, matched := evaluateHookPolicyConditions(context.Background(), resolver, HookPolicy{
			ConditionMode: HookConditionAny,
			Conditions:    []Condition{falsePure, deferred},
		}, event)
		if matches || len(matched) != 0 || resolver.calls != 1 {
			t.Fatalf("matches=%v matched=%#v resolver calls=%d", matches, matched, resolver.calls)
		}
	})
}
