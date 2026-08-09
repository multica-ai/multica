package workflows

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/commentquality"
)

func stubQualityRunner(t *testing.T, res commentquality.Result, err error) {
	t.Helper()
	original := qualityGateRunner
	t.Cleanup(func() { qualityGateRunner = original })
	qualityGateRunner = func(context.Context, string, string, string) (commentquality.Result, error) {
		return res, err
	}
}

func TestQualityGatePassReturnsNoRequirement(t *testing.T) {
	stubQualityRunner(t, commentquality.Result{Pass: true}, nil)
	out, err := (&PostgresHookActionExecutor{}).qualityGate(context.Background(),
		HookEvent{Proposed: map[string]any{"content": "En klar konklusion."}},
		map[string]any{"rubric": "effective-comments"})
	if err != nil {
		t.Fatal(err)
	}
	if out["pass"] != true {
		t.Fatalf("out = %#v, want pass", out)
	}
}

func TestQualityGateRejectReturnsRequireVerdict(t *testing.T) {
	stubQualityRunner(t, commentquality.Result{Pass: false, Requirement: "Start med en konklusion."}, nil)
	out, err := (&PostgresHookActionExecutor{}).qualityGate(context.Background(),
		HookEvent{Proposed: map[string]any{"content": "Her er status"}},
		map[string]any{"rubric": "effective-comments"})
	// A REJECT is a successful judgment, not an action failure: no error, and the
	// verdict rides the output so the engine blocks independent of fail_mode.
	if err != nil {
		t.Fatalf("reject must not error: %v", err)
	}
	if out["decision"] != string(HookRequire) || out["requirement"] != "Start med en konklusion." {
		t.Fatalf("out = %#v, want require verdict + requirement", out)
	}
}

func TestQualityGateJudgeFailureReturnsError(t *testing.T) {
	stubQualityRunner(t, commentquality.Result{}, errors.New("gateway down"))
	_, err := (&PostgresHookActionExecutor{}).qualityGate(context.Background(),
		HookEvent{Proposed: map[string]any{"content": "text"}},
		map[string]any{"rubric": "effective-comments"})
	if err == nil {
		t.Fatal("a judge failure must error so fail_mode can decide")
	}
}

func TestQualityGateEmptyContentPasses(t *testing.T) {
	// No stub: an empty comment must never reach the judge.
	qualityGateRunner = func(context.Context, string, string, string) (commentquality.Result, error) {
		t.Fatal("judge must not run for empty content")
		return commentquality.Result{}, nil
	}
	t.Cleanup(func() {
		qualityGateRunner = func(ctx context.Context, rubric, content, userLabel string) (commentquality.Result, error) {
			return (&commentquality.Judger{}).Judge(ctx, rubric, content, userLabel)
		}
	})
	out, err := (&PostgresHookActionExecutor{}).qualityGate(context.Background(),
		HookEvent{Proposed: map[string]any{"content": "   "}},
		map[string]any{"rubric": "effective-comments"})
	if err != nil || out["pass"] != true {
		t.Fatalf("out=%#v err=%v, want pass", out, err)
	}
}

func TestQualityGateRequiresRubric(t *testing.T) {
	_, err := (&PostgresHookActionExecutor{}).qualityGate(context.Background(),
		HookEvent{Proposed: map[string]any{"content": "text"}}, map[string]any{})
	if err == nil {
		t.Fatal("expected error when rubric is missing")
	}
}

func TestQualityGateValidationRequiresRubric(t *testing.T) {
	if err := validateTypedHookAction(HookAction{Type: "quality.gate"}); err == nil {
		t.Fatal("quality.gate without rubric must fail validation")
	}
	if err := validateTypedHookAction(HookAction{Type: "quality.gate", Config: map[string]any{"rubric": "r"}}); err != nil {
		t.Fatalf("quality.gate with rubric must validate: %v", err)
	}
}

func qualityGatePolicy(failMode HookFailMode) HookPolicy {
	return HookPolicy{
		ID: "q-gate", Version: 1, Name: "q-gate", Mode: HookModeEnforce, FailMode: failMode,
		Events:   []HookEventType{HookBeforeMessageSend},
		Bindings: []HookBinding{{Kind: HookScopeWorkspace, ID: "ws-1"}},
		Handlers: []HookHandler{{ID: "h1", Decision: HookAllow, Actions: []HookAction{{Type: "quality.gate"}}}},
	}
}

func evaluateQualityGate(t *testing.T, policy HookPolicy, action func(ActionInvocation) (map[string]any, error)) HookResult {
	t.Helper()
	registry := NewActionRegistry()
	registry.Register("quality.gate", func(_ context.Context, in ActionInvocation) (map[string]any, error) {
		return action(in)
	})
	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).
		WithActionRegistry(registry).
		Evaluate(context.Background(), HookEvent{
			EventID: "q1", Type: HookBeforeMessageSend, WorkspaceID: "ws-1",
			Proposed: map[string]any{"content": "Her er status"},
		})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// A REJECT must block the send AND surface the fix even under fail_mode: warn —
// this is the review point: warn governs judge FAILURES, never a bad comment.
func TestQualityGateRejectBlocksEvenUnderFailModeWarn(t *testing.T) {
	result := evaluateQualityGate(t, qualityGatePolicy(HookFailWarn), func(ActionInvocation) (map[string]any, error) {
		return map[string]any{"decision": string(HookRequire), "requirement": "Start med en konklusion."}, nil
	})
	if result.Decision != HookRequire {
		t.Fatalf("decision = %q, want require (blocked) even under fail_mode: warn", result.Decision)
	}
	if len(result.Requirements) != 1 || !strings.Contains(result.Requirements[0], "konklusion") {
		t.Fatalf("requirements = %#v, want the judge fix", result.Requirements)
	}
}

// A judge FAILURE (gateway down) follows fail_mode: warn lets the send through,
// closed blocks it.
func TestQualityGateJudgeFailureFollowsFailMode(t *testing.T) {
	fail := func(ActionInvocation) (map[string]any, error) { return nil, errors.New("gateway down") }

	warn := evaluateQualityGate(t, qualityGatePolicy(HookFailWarn), fail)
	if warn.Decision != HookAllow {
		t.Fatalf("fail_mode warn: decision = %q, want allow (comment flows during outage)", warn.Decision)
	}
	closed := evaluateQualityGate(t, qualityGatePolicy(HookFailClosed), fail)
	if closed.Decision != HookBlock {
		t.Fatalf("fail_mode closed: decision = %q, want block", closed.Decision)
	}
}
