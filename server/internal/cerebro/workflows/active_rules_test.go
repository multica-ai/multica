package workflows

import (
	"context"
	"testing"
)

func TestActiveRuleServiceReturnsOnlyRulesForTheAgentAndIssue(t *testing.T) {
	repo := NewMemoryHookRepository()
	workspaceID := "workspace-1"
	for _, policy := range []HookPolicy{
		activeRulePolicy("workspace", "Workspace rule", HookBinding{Kind: HookScopeWorkspace, ID: workspaceID}),
		activeRulePolicy("agent", "Agent rule", HookBinding{Kind: HookScopeAgent, ID: "agent-1"}),
		activeRulePolicy("issue", "Issue rule", HookBinding{Kind: HookScopeIssue, ID: "issue-1"}),
		activeRulePolicy("other-agent", "Other agent rule", HookBinding{Kind: HookScopeAgent, ID: "agent-2"}),
	} {
		repo.Seed(workspaceID, policy)
	}

	rules, err := NewActiveRuleService(repo).List(context.Background(), ActiveRuleContext{
		WorkspaceID: workspaceID,
		AgentID:     "agent-1",
		IssueID:     "issue-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 {
		t.Fatalf("rules = %#v, want three applicable rules", rules)
	}
	for index, name := range []string{"Agent rule", "Issue rule", "Workspace rule"} {
		if rules[index].Name != name || rules[index].ContractRule == "" || rules[index].ContractSatisfy == "" {
			t.Fatalf("rule %d = %#v, want %q with readable contract", index, rules[index], name)
		}
	}
}

func activeRulePolicy(id, name string, binding HookBinding) HookPolicy {
	policy := newTestHookPolicy(id, HookRequire, HookModeEnforce, binding)
	policy.Name = name
	policy.Events = []HookEventType{HookBeforeTaskComplete}
	policy.ContractRule = name + " must be followed."
	policy.ContractSatisfy = "Complete " + name + "."
	return policy
}
