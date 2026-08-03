package workflows

import (
	"context"
	"testing"
)

type fakeWorkspaceFlagResolver struct {
	enabled bool
	seen    []string
}

func (r *fakeWorkspaceFlagResolver) WorkflowHooksEnabledForWorkspace(_ context.Context, workspaceID string) (bool, error) {
	r.seen = append(r.seen, workspaceID)
	return r.enabled, nil
}

func TestWorkspaceHookEvaluatorRunsWhenWorkspaceFlagIsOn(t *testing.T) {
	event := HookEvent{Type: HookBeforeMessageSend, WorkspaceID: "11111111-1111-1111-1111-111111111111"}
	engine := &fakeHookEvaluator{result: HookResult{Decision: HookRequire}}
	flags := &fakeWorkspaceFlagResolver{enabled: true}
	evaluator := NewWorkspaceHookEvaluator(true, engine).WithWorkspaceFlags(flags)
	result, err := evaluator.Evaluate(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookRequire || engine.calls != 1 {
		t.Fatalf("result=%#v calls=%d", result, engine.calls)
	}
	if len(flags.seen) != 1 || flags.seen[0] != event.WorkspaceID {
		t.Fatalf("flag resolved for %v", flags.seen)
	}
}

func TestWorkspaceHookEvaluatorWorkspaceFlagOffSkipsEngine(t *testing.T) {
	engine := &fakeHookEvaluator{result: HookResult{Decision: HookBlock}}
	evaluator := NewWorkspaceHookEvaluator(true, engine).
		WithWorkspaceFlags(&fakeWorkspaceFlagResolver{enabled: false})
	result, err := evaluator.Evaluate(context.Background(), HookEvent{
		Type: HookBeforeMessageSend, WorkspaceID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookAllow || engine.calls != 0 {
		t.Fatalf("result=%#v calls=%d", result, engine.calls)
	}
}

func TestWorkspaceHookEvaluatorDefaultsOnWithoutFlagResolver(t *testing.T) {
	engine := &fakeHookEvaluator{result: HookResult{Decision: HookRequire}}
	evaluator := NewWorkspaceHookEvaluator(true, engine)
	result, err := evaluator.Evaluate(context.Background(), HookEvent{
		Type: HookBeforeMessageSend, WorkspaceID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookRequire || engine.calls != 1 {
		t.Fatalf("result=%#v calls=%d", result, engine.calls)
	}
}

func TestWorkspaceHookEvaluatorEmergencyServerSwitch(t *testing.T) {
	engine := &fakeHookEvaluator{result: HookResult{Decision: HookBlock}}
	evaluator := NewWorkspaceHookEvaluator(false, engine)
	result, err := evaluator.Evaluate(context.Background(), HookEvent{
		Type: HookBeforeMessageSend, WorkspaceID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookAllow || engine.calls != 0 {
		t.Fatalf("result=%#v calls=%d", result, engine.calls)
	}
}
