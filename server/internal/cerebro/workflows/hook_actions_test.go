package workflows

import (
	"context"
	"errors"
	"testing"
)

func TestTypedActionRegistryContainsLegacyAndVersionOneActions(t *testing.T) {
	registry := NewActionRegistry()
	registerVersionOneHookActions(registry, &fakeTypedActionExecutor{})
	registerLegacyActionNames(registry)
	want := []string{
		ActionSetStatus, ActionCreateSubIssue, ActionSendReminder, ActionRunSkill,
		"member.notify", "agent.dispatch", "squad.dispatch", "wakeup.create", "wakeup.cancel",
		"session.handoff", "task.retry", "task.cancel", "artifact.create_or_update",
		"workflow.activate", "workflow.pause", "workflow.resume", "workflow.stop",
		"approval.require", "audit.record", "metric.increment",
	}
	for _, name := range want {
		if !registry.Has(name) {
			t.Errorf("missing action %s", name)
		}
	}
}

func TestHookActionsDeclareTheCreatorCapabilityRecheckedAtExecution(t *testing.T) {
	for _, actionType := range versionOneHookActionTypes {
		if actionType == "audit.record" || actionType == "metric.increment" {
			continue
		}
		if hookActionCapability(actionType) == "" {
			t.Errorf("%s has no creator capability", actionType)
		}
	}
}

func TestStartHandoffRequiresTypedBriefTargetAndDepthLimit(t *testing.T) {
	valid := map[string]any{
		"target": "11111111-1111-1111-1111-111111111111", "summary": "Plan finished",
		"done": "Plan and acceptance checks", "remaining": "Build the implementation", "plan_ref": "artifact:plan-1", "max_depth": 2,
	}
	if _, err := validateHandoffAction(HookEvent{HookDepth: 1}, valid); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{"target", "summary", "done", "remaining", "plan_ref"} {
		config := map[string]any{}
		for key, value := range valid {
			config[key] = value
		}
		delete(config, missing)
		if _, err := validateHandoffAction(HookEvent{HookDepth: 1}, config); err == nil {
			t.Errorf("missing %s should fail", missing)
		}
	}
	if _, err := validateHandoffAction(HookEvent{HookDepth: 2}, valid); !errors.Is(err, ErrHookDepthExceeded) {
		t.Fatalf("depth error = %v", err)
	}
}

func TestHookEngineExecutesTypedActionOnceAndAuditsResult(t *testing.T) {
	executor := &fakeTypedActionExecutor{}
	registry := NewActionRegistry()
	registerVersionOneHookActions(registry, executor)
	policy := newTestHookPolicy("action-policy", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	policy.Handlers[0].Actions = []HookAction{{Type: "audit.record", Config: map[string]any{"message": "matched"}}}
	store := NewMemoryHookStore([]HookPolicy{policy})
	engine := NewHookEngine(true, store).WithActionRegistry(registry)
	event := HookEvent{EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1"}

	first, err := engine.Evaluate(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.Evaluate(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || len(first.ActionResults) != 1 || first.ActionResults[0].Status != HookActionSuccess {
		t.Fatalf("calls=%d first=%#v", executor.calls, first)
	}
	if len(second.ActionResults) != 1 {
		t.Fatalf("cached result lost action history: %#v", second)
	}
}

func TestHookEngineRecordsPermissionDenialAsDenied(t *testing.T) {
	registry := NewActionRegistry()
	registry.Register("agent.dispatch", func(context.Context, ActionInvocation) (map[string]any, error) {
		return nil, ErrHookActionPermissionDenied
	})
	policy := newTestHookPolicy("denied-action", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	policy.FailMode = HookFailClosed
	policy.Handlers[0].Actions = []HookAction{{Type: "agent.dispatch"}}
	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).WithActionRegistry(registry).Evaluate(context.Background(), HookEvent{EventID: "event-denied", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActionResults) != 1 || result.ActionResults[0].Status != HookActionDenied || result.Decision != HookBlock {
		t.Fatalf("permission denial result = %#v", result)
	}
}

type fakeTypedActionExecutor struct{ calls int }

func (f *fakeTypedActionExecutor) ExecuteHookAction(context.Context, HookPolicy, HookEvent, HookAction) (map[string]any, error) {
	f.calls++
	return map[string]any{"ok": true}, nil
}
