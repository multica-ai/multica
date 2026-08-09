package handler

import (
	"context"
	"testing"

	cerebroworkflows "github.com/multica-ai/multica/server/internal/cerebro/workflows"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type activeRuleResolverStub struct {
	got   cerebroworkflows.ActiveRuleContext
	rules []cerebroworkflows.ActiveHookRule
}

func (s *activeRuleResolverStub) RulesForContext(_ context.Context, scope cerebroworkflows.ActiveRuleContext) ([]cerebroworkflows.ActiveHookRule, error) {
	s.got = scope
	return s.rules, nil
}

func TestApplyWorkflowHookRulesCarriesApplicableContractsIntoTheClaim(t *testing.T) {
	resolver := &activeRuleResolverStub{rules: []cerebroworkflows.ActiveHookRule{{
		ID: "rule-1", Name: "Require a next step", ContractRule: "Leave a next step.", ContractSatisfy: "Create a wakeup.", Events: []cerebroworkflows.HookEventType{cerebroworkflows.HookBeforeTaskComplete},
	}}}
	h := &Handler{WorkflowHookRules: resolver}
	resp := &AgentTaskResponse{AgentID: "11111111-1111-1111-1111-111111111111", Agent: &TaskAgentData{Model: "gpt-5"}, ProjectID: "22222222-2222-2222-2222-222222222222"}
	issue := db.Issue{ID: parseUUID("33333333-3333-3333-3333-333333333333"), WorkspaceID: parseUUID("44444444-4444-4444-4444-444444444444")}

	h.applyWorkflowHookRules(context.Background(), resp, issue)

	if resolver.got.AgentID != resp.AgentID || resolver.got.IssueID != uuidToString(issue.ID) || resolver.got.Model != "gpt-5" || resolver.got.ProjectID != resp.ProjectID {
		t.Fatalf("resolver scope = %#v", resolver.got)
	}
	if len(resp.ActiveHookRules) != 1 || resp.ActiveHookRules[0].Name != "Require a next step" {
		t.Fatalf("active hook rules = %#v", resp.ActiveHookRules)
	}
}
