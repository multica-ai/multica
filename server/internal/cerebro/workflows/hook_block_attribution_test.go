package workflows

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// FIR-4797: a stop must always name the hook that made it. An anonymous
// "workflow hook blocked <event>" leaves the workspace owner with nothing to
// switch off, which is how one policy looked like a platform outage.
func TestHookEngineNamesTheBlockingHook(t *testing.T) {
	policy := newTestHookPolicy("block-1", HookBlock, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	policy.Name = "Require evidence before an agent run stops"
	engine := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy}))

	result, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BlockedBy == nil {
		t.Fatal("blocked result carries no hook identity")
	}
	if result.BlockedBy.ID != "block-1" || result.BlockedBy.Name != "Require evidence before an agent run stops" {
		t.Fatalf("blocked_by = %#v", result.BlockedBy)
	}
}

// A dry-run hook never stops anything, so it must never be named as the cause.
func TestHookEngineNeverNamesADryRunHook(t *testing.T) {
	policy := newTestHookPolicy("dry-1", HookBlock, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	engine := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy}))

	result, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BlockedBy != nil {
		t.Fatalf("dry-run hook was named as the blocker: %#v", result.BlockedBy)
	}
}

// The hardest attribution case: the handler itself said "allow" and the stop
// came from a failing action under fail_mode closed. Before FIR-4797 that stop
// carried no name and no reason at all.
func TestHookEngineNamesTheHookWhenAFailClosedActionStopsTheEvent(t *testing.T) {
	policy := newTestHookPolicy("gate-1", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	policy.Name = "Comment quality gate"
	policy.FailMode = HookFailClosed
	policy.Handlers[0].Actions = []HookAction{{Type: "judge.gate", Config: map[string]any{"rubric": "anything"}}}

	actions := NewActionRegistry()
	actions.Register("judge.gate", func(context.Context, ActionInvocation) (map[string]any, error) {
		return nil, errors.New("judge gateway unreachable")
	})
	engine := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).WithActionRegistry(actions)

	result, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookBlock {
		t.Fatalf("decision = %q, want block", result.Decision)
	}
	if result.BlockedBy == nil || result.BlockedBy.Name != "Comment quality gate" {
		t.Fatalf("blocked_by = %#v", result.BlockedBy)
	}
	if !strings.Contains(result.Warning, "Comment quality gate") || !strings.Contains(result.Warning, "judge gateway unreachable") {
		t.Fatalf("warning = %q, want the hook name and the action error", result.Warning)
	}
}

func TestReasonWithHookAlwaysNamesTheHook(t *testing.T) {
	blocked := HookResult{BlockedBy: &HookRef{ID: "hook-1", Name: "Require a continuation"}}
	if got := blocked.ReasonWithHook("Create a wakeup."); got != `Hook "Require a continuation" stopped this: Create a wakeup.` {
		t.Fatalf("reason = %q", got)
	}
	if got := blocked.ReasonWithHook(""); got != `Hook "Require a continuation" stopped this.` {
		t.Fatalf("empty-detail reason = %q", got)
	}
	// An unnamed hook falls back to its id rather than to silence.
	unnamed := HookResult{BlockedBy: &HookRef{ID: "hook-2"}}
	if got := unnamed.ReasonWithHook(""); got != `Hook "hook-2" stopped this.` {
		t.Fatalf("unnamed reason = %q", got)
	}
	if got := (HookResult{}).ReasonWithHook("Plain detail."); got != "Plain detail." {
		t.Fatalf("no-hook reason = %q", got)
	}
}
