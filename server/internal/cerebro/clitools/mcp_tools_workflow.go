// CEREBRO-PATCH(cerebro-workflow-mcp): FIR-2937 workflow management tools for agents.
package clitools

import (
	"context"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerWorkflowTools(srv *mcp.Server, client *cli.APIClient) {
	objectSchema := map[string]any{"type": "object", "description": "Complete workflow document accepted by /api/cerebro/workflows. For an Issue workflow set workflow_type=issue_loop and provide loop_spec."}
	idProps := map[string]any{"workflow_id": map[string]any{"type": "string", "description": "Workflow UUID"}}

	srv.RegisterTool(mcp.Tool{Name: "list_workflows", Description: "List workflows in the current workspace, including Issue workflow recipes.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}}, func(ctx context.Context, _ map[string]any) (mcp.CallToolResult, error) {
		var out any
		if err := client.GetJSON(ctx, "/api/cerebro/workflows", &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	srv.RegisterTool(mcp.Tool{Name: "get_workflow", Description: "Get one workflow by UUID.", InputSchema: map[string]any{"type": "object", "required": []string{"workflow_id"}, "properties": idProps}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "workflow_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var out any
		if err := client.GetJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(id), &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	srv.RegisterTool(mcp.Tool{Name: "create_workflow", Description: "Create a standard or Issue workflow from a complete workflow document.", InputSchema: map[string]any{"type": "object", "required": []string{"workflow"}, "properties": map[string]any{"workflow": objectSchema}}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		body, ok := args["workflow"].(map[string]any)
		if !ok {
			return mcp.ErrorResult("workflow must be an object"), nil
		}
		var out any
		if err := client.PostJSON(ctx, "/api/cerebro/workflows", body, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	srv.RegisterTool(mcp.Tool{Name: "update_workflow", Description: "Replace a workflow using a complete workflow document.", InputSchema: map[string]any{"type": "object", "required": []string{"workflow_id", "workflow"}, "properties": map[string]any{"workflow_id": idProps["workflow_id"], "workflow": objectSchema}}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "workflow_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body, ok := args["workflow"].(map[string]any)
		if !ok {
			return mcp.ErrorResult("workflow must be an object"), nil
		}
		var out any
		if err := client.PutJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(id), body, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	srv.RegisterTool(mcp.Tool{Name: "delete_workflow", Description: "Delete a workflow by UUID.", InputSchema: map[string]any{"type": "object", "required": []string{"workflow_id"}, "properties": idProps}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "workflow_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if err := client.DeleteJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(id)); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"deleted": id})
	})

	srv.RegisterTool(mcp.Tool{Name: "toggle_workflow", Description: "Enable or disable a workflow.", InputSchema: map[string]any{"type": "object", "required": []string{"workflow_id", "enabled"}, "properties": map[string]any{"workflow_id": idProps["workflow_id"], "enabled": map[string]any{"type": "boolean"}}}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "workflow_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		enabled, ok := args["enabled"].(bool)
		if !ok {
			return mcp.ErrorResult("enabled must be a boolean"), nil
		}
		var out any
		if err := client.PostJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(id)+"/toggle", map[string]any{"enabled": enabled}, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	srv.RegisterTool(mcp.Tool{Name: "activate_workflow", Description: "Activate an Issue workflow recipe on an existing issue.", InputSchema: map[string]any{"type": "object", "required": []string{"workflow_id", "issue_id"}, "properties": map[string]any{"workflow_id": idProps["workflow_id"], "issue_id": map[string]any{"type": "string", "description": "Canonical issue UUID"}}}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "workflow_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		issueID, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var out any
		if err := client.PostJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(id)+"/activate", map[string]any{"issue_id": issueID}, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	srv.RegisterTool(mcp.Tool{Name: "get_active_workflow", Description: "Get the Issue workflow currently active on an issue.", InputSchema: map[string]any{"type": "object", "required": []string{"issue_id"}, "properties": map[string]any{"issue_id": map[string]any{"type": "string"}}}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		issueID, err := requireString(args, "issue_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var out any
		if err := client.GetJSON(ctx, "/api/cerebro/workflows/for-issue/"+url.PathEscape(issueID), &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})
}
