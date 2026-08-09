package handler

// CEREBRO-PATCH(workflow-hook-house-rules): resolve the live Workflow hook
// contracts at claim time so every runtime receives the same House rules.

import (
	"context"
	"log/slog"

	cerebroworkflows "github.com/multica-ai/multica/server/internal/cerebro/workflows"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ActiveHookRuleData = cerebroworkflows.ActiveHookRule

type WorkflowHookRuleResolver interface {
	RulesForContext(context.Context, cerebroworkflows.ActiveRuleContext) ([]cerebroworkflows.ActiveHookRule, error)
}

func (h *Handler) applyWorkflowHookRules(ctx context.Context, resp *AgentTaskResponse, issue db.Issue) {
	if h == nil || h.WorkflowHookRules == nil || resp == nil || !issue.WorkspaceID.Valid {
		return
	}
	model := ""
	if resp.Agent != nil {
		model = resp.Agent.Model
	}
	rules, err := h.WorkflowHookRules.RulesForContext(ctx, cerebroworkflows.ActiveRuleContext{
		WorkspaceID: uuidToString(issue.WorkspaceID),
		ProjectID:   resp.ProjectID,
		AgentID:     resp.AgentID,
		Model:       model,
		IssueID:     uuidToString(issue.ID),
	})
	if err != nil {
		slog.Warn("failed to resolve active Workflow hook rules for task claim", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	resp.ActiveHookRules = rules
}
