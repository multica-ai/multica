package clitools

import (
	"context"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// registerApprovalTools exposes the agent-callable approval request tool on
// the Multica MCP server. It proxies to the workspace MCP endpoint so the
// server keeps owning the approval intake flow.
func registerApprovalTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name:        "request_approval",
		Description: "Request a human decision for an action that needs approval.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"capability", "reason"},
			"properties": map[string]any{
				"capability": map[string]any{"type": "string", "description": "The action that needs a human decision."},
				"resource":   map[string]any{"type": "string", "description": "Optional resource the action targets."},
				"reason":     map[string]any{"type": "string", "description": "Why the approval is needed."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		if strings.TrimSpace(client.WorkspaceID) == "" {
			return mcp.ErrorResult("request_approval requires a workspace_id"), nil
		}
		payload := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "request_approval",
				"arguments": map[string]any{
					"capability": optString(args, "capability"),
					"resource":   optString(args, "resource"),
					"reason":     optString(args, "reason"),
				},
			},
		}

		var resp struct {
			Result mcp.CallToolResult `json:"result"`
			Error  *mcp.RPCError      `json:"error,omitempty"`
		}
		path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/mcp"
		if err := client.PostJSON(ctx, path, payload, &resp); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if resp.Error != nil {
			return mcp.ErrorResult(resp.Error.Message), nil
		}
		if resp.Result.IsError {
			if len(resp.Result.Content) > 0 && strings.TrimSpace(resp.Result.Content[0].Text) != "" {
				return mcp.ErrorResult(strings.TrimSpace(resp.Result.Content[0].Text)), nil
			}
			return mcp.ErrorResult("request_approval failed"), nil
		}
		if len(resp.Result.StructuredContent) > 0 {
			return mcp.TextResult(string(resp.Result.StructuredContent)), nil
		}
		if len(resp.Result.Content) > 0 && strings.TrimSpace(resp.Result.Content[0].Text) != "" {
			return mcp.TextResult(strings.TrimSpace(resp.Result.Content[0].Text)), nil
		}
		return mcp.TextResult("{}"), nil
	})
}
