package workflows

import "testing"

func TestHookPermissionsAreReadOnlyForFreshAgent(t *testing.T) {
	evaluator := HookPermissionEvaluator{}
	agent := HookPermissionActor{Type: "agent", ID: "agent-1"}

	if !evaluator.Can(agent, HookPermissionRead) {
		t.Fatal("fresh agent should be able to read visible hooks")
	}
	for _, permission := range []HookPermission{HookPermissionWrite, HookPermissionEnforce, HookPermissionManageManaged} {
		if evaluator.Can(agent, permission) {
			t.Fatalf("fresh agent unexpectedly has %s", permission)
		}
	}
}

func TestHookWriteDoesNotGrantPublish(t *testing.T) {
	evaluator := HookPermissionEvaluator{Explicit: map[string]map[HookPermission]bool{
		"agent-1": {HookPermissionWrite: true},
	}}
	agent := HookPermissionActor{Type: "agent", ID: "agent-1"}
	if !evaluator.Can(agent, HookPermissionWrite) {
		t.Fatal("explicit write grant was ignored")
	}
	if evaluator.Can(agent, HookPermissionEnforce) {
		t.Fatal("write grant must not imply enforce")
	}
}

func TestHookEnforcementIsHumanOnlyAndManagedIsOwnerOnly(t *testing.T) {
	evaluator := HookPermissionEvaluator{Explicit: map[string]map[HookPermission]bool{
		"agent-1":  {HookPermissionEnforce: true, HookPermissionManageManaged: true},
		"member-1": {HookPermissionEnforce: true},
	}}
	if evaluator.Can(HookPermissionActor{Type: "agent", ID: "agent-1", IsOwner: true}, HookPermissionEnforce) {
		t.Fatal("an agent may never enforce a hook")
	}
	if !evaluator.Can(HookPermissionActor{Type: "member", ID: "member-1"}, HookPermissionEnforce) {
		t.Fatal("explicitly authorised member should be able to enforce")
	}
	if evaluator.Can(HookPermissionActor{Type: "member", ID: "member-1"}, HookPermissionManageManaged) {
		t.Fatal("non-owner may not manage managed hooks")
	}
	if !evaluator.Can(HookPermissionActor{Type: "member", ID: "owner-1", IsOwner: true}, HookPermissionManageManaged) {
		t.Fatal("workspace owner should bootstrap managed-hook administration")
	}
}

func TestManageWorkflowsDoesNotImplyHookPermissions(t *testing.T) {
	evaluator := HookPermissionEvaluator{LegacyManageWorkflows: map[string]bool{"agent-1": true}}
	agent := HookPermissionActor{Type: "agent", ID: "agent-1"}
	if evaluator.Can(agent, HookPermissionWrite) || evaluator.Can(agent, HookPermissionEnforce) {
		t.Fatal("manage_workflows must not imply hook permissions")
	}
}
