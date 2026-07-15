// CEREBRO-PATCH(cerebro-mini-app-agent-tool): FIR-3172 let agents open interactive app views.
package clitools

import (
	"context"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerMiniAppTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name:        "show_app_view",
		Description: "Start an enabled chat-triggered mini-app workflow and publish its interactive view card into a chat or issue. The workflow pauses until the user submits the card.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"workflow_id", "target_kind", "target_id", "input"},
			"properties": map[string]any{
				"workflow_id": map[string]any{"type": "string", "description": "Mini-app workflow UUID"},
				"target_kind": map[string]any{"type": "string", "enum": []string{"chat", "issue"}},
				"target_id":   map[string]any{"type": "string", "description": "Chat session or issue UUID"},
				"input":       map[string]any{"type": "object", "description": "Workflow trigger payload"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		workflowID, err := requireString(args, "workflow_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		targetKind, err := requireString(args, "target_kind")
		if err != nil || (targetKind != "chat" && targetKind != "issue") {
			return mcp.ErrorResult("target_kind must be chat or issue"), nil
		}
		targetID, err := requireString(args, "target_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		input, ok := args["input"].(map[string]any)
		if !ok {
			return mcp.ErrorResult("input must be an object"), nil
		}
		body := make(map[string]any, len(input)+1)
		for key, value := range input {
			body[key] = value
		}
		body["_multica_target"] = map[string]any{"kind": targetKind, "id": targetID}
		var out any
		if err := client.PostJSON(ctx, "/api/cerebro/app-workflows/"+url.PathEscape(workflowID)+"/chat", body, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})
}
