package workflows

import (
	"context"
	"testing"
)

func TestWorkflowHooksEmergencySwitchDefaultsOn(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOW_HOOKS_ENABLED", "")
	if !hookFeatureEnabled() {
		t.Fatal("Workflow lifecycle gates must default on")
	}
	for _, value := range []string{"1", "true", "yes", "on"} {
		t.Setenv("CEREBRO_WORKFLOW_HOOKS_ENABLED", value)
		if !hookFeatureEnabled() {
			t.Fatalf("%q should enable workflow hooks", value)
		}
	}
	for _, value := range []string{"0", "false", "no", "off"} {
		t.Setenv("CEREBRO_WORKFLOW_HOOKS_ENABLED", value)
		if hookFeatureEnabled() {
			t.Fatalf("%q should activate the emergency stop", value)
		}
	}
}

func TestHookEngineStoreCapturesCompatibleEventWithoutPolicyMatch(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryHookRepository()
	policy := newTestHookPolicy("policy-1", HookAllow, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: "workspace-1"})
	repository.Seed("workspace-1", policy)
	store := NewPostgresHookEngineStore(repository)
	event := HookEvent{EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "workspace-1"}

	if err := store.SaveResult(ctx, event.EventID, event, HookResult{Decision: HookAllow}); err != nil {
		t.Fatal(err)
	}
	retained, err := repository.CompatibleEvents(ctx, "workspace-1", policy.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(retained) != 1 || retained[0].EventID != event.EventID {
		t.Fatalf("retained events = %#v", retained)
	}
}
