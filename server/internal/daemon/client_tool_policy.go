package daemon

import (
	"context"
	"fmt"
)

type ToolPolicyResolveResult struct {
	Allowed    bool   `json:"allowed"`
	Decision   string `json:"decision"`
	Reason     string `json:"reason,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
}

func (c *Client) ResolveToolPolicy(ctx context.Context, workspaceID, agentID, toolName, resourcePattern string, args map[string]any) (ToolPolicyResolveResult, error) {
	var resp ToolPolicyResolveResult
	err := c.postJSON(ctx, fmt.Sprintf("/api/daemon/workspaces/%s/tool-policy/resolve", workspaceID), map[string]any{
		"agent_id":         agentID,
		"tool_name":        toolName,
		"resource_pattern": resourcePattern,
		"args":             args,
	}, &resp)
	return resp, err
}

func (c *Client) PollToolPolicyApproval(ctx context.Context, workspaceID, approvalID string) (bool, string, string, error) {
	var resp struct {
		Allowed  bool   `json:"allowed"`
		Decision string `json:"decision"`
		Reason   string `json:"reason,omitempty"`
	}
	err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/workspaces/%s/tool-policy/resolve/%s", workspaceID, approvalID), &resp)
	return resp.Allowed, resp.Decision, resp.Reason, err
}
