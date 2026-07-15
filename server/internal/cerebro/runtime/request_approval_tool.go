package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	"github.com/multica-ai/multica/server/internal/util"
)

// FirtalRequestApprovalTool exposes the shared approval intake to managed agents.
type FirtalRequestApprovalTool struct {
	tctx ToolContext
}

func (t *FirtalRequestApprovalTool) Name() string { return "request_approval" }

func (t *FirtalRequestApprovalTool) Description() string {
	return "Request a human decision for an action that needs approval."
}

func (t *FirtalRequestApprovalTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"capability", "reason"},
		"properties": map[string]any{
			"capability": map[string]any{"type": "string", "description": "The action that needs a human decision."},
			"resource":   map[string]any{"type": "string", "description": "Optional resource the action targets."},
			"reason":     map[string]any{"type": "string", "description": "Why the approval is needed."},
		},
	}
}

func (t *FirtalRequestApprovalTool) Call(ctx context.Context, args map[string]any) (string, error) {
	row, err := approvals.RequestFromAgent(ctx, t.tctx.ApprovalRequester, approvals.AgentRequest{
		WorkspaceID: t.tctx.WorkspaceID, AgentID: t.tctx.AgentID, TaskID: t.tctx.TaskID,
		IssueID: t.tctx.IssueID, ChatSessionID: t.tctx.ChatSessionID, TriggerCommentID: t.tctx.TriggerCommentID,
		Capability: stringValue(args, "capability"), Resource: stringValue(args, "resource"),
		Reason: stringValue(args, "reason"), Surface: t.tctx.Surface, AuditSurface: approvals.SurfaceMCP,
	})
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(map[string]any{
		"id": util.UUIDToString(row.ID), "status": row.Status,
		"capability": row.Capability, "resource": row.Resource,
	})
	if err != nil {
		return "", fmt.Errorf("request_approval: marshal result: %w", err)
	}
	return string(raw), nil
}

func stringValue(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}
