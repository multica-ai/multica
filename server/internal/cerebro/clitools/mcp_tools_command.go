// CEREBRO-PATCH(cerebro-command-mcp): FIR-3493 command library parity for local and gateway MCP.
package clitools

import (
	"context"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerCommandTools(srv *mcp.Server, client *cli.APIClient) {
	commandSchema := map[string]any{
		"type":        "object",
		"description": "Reusable workflow command definition.",
		"required":    []string{"key", "title", "argv"},
		"properties": map[string]any{
			"key":         map[string]any{"type": "string"},
			"title":       map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"argv": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
	}
	idSchema := map[string]any{"type": "string", "description": "Command UUID"}

	srv.RegisterTool(mcp.Tool{Name: "list_commands", Description: "List reusable workflow commands in the current workspace.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}}, func(ctx context.Context, _ map[string]any) (mcp.CallToolResult, error) {
		var out any
		if err := client.GetJSON(ctx, "/api/cerebro/commands", &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	srv.RegisterTool(mcp.Tool{Name: "get_command", Description: "Get one reusable workflow command by UUID.", InputSchema: map[string]any{"type": "object", "required": []string{"command_id"}, "properties": map[string]any{"command_id": idSchema}}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "command_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var out any
		if err := client.GetJSON(ctx, "/api/cerebro/commands/"+url.PathEscape(id), &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})

	registerCommandWrite(srv, client, "create_command", "Create a reusable workflow command.", "POST", "/api/cerebro/commands", commandSchema, nil)
	registerCommandWrite(srv, client, "update_command", "Replace a reusable workflow command.", "PUT", "/api/cerebro/commands/{command_id}", commandSchema, idSchema)

	srv.RegisterTool(mcp.Tool{Name: "delete_command", Description: "Delete a reusable workflow command. Saved workflows keep their snapshotted argv.", InputSchema: map[string]any{"type": "object", "required": []string{"command_id"}, "properties": map[string]any{"command_id": idSchema}}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "command_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if err := client.DeleteJSON(ctx, "/api/cerebro/commands/"+url.PathEscape(id)); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"deleted": id})
	})
}

func registerCommandWrite(srv *mcp.Server, client *cli.APIClient, name, description, method, path string, commandSchema, idSchema map[string]any) {
	properties := map[string]any{"command": commandSchema}
	required := []string{"command"}
	if idSchema != nil {
		properties["command_id"] = idSchema
		required = append(required, "command_id")
	}
	srv.RegisterTool(mcp.Tool{Name: name, Description: description, InputSchema: map[string]any{"type": "object", "required": required, "properties": properties}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		body, ok := args["command"].(map[string]any)
		if !ok {
			return mcp.ErrorResult("command must be an object"), nil
		}
		resolved := path
		if idSchema != nil {
			id, err := requireString(args, "command_id")
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			resolved = "/api/cerebro/commands/" + url.PathEscape(id)
		}
		var out any
		var err error
		if method == "PUT" {
			err = client.PutJSON(ctx, resolved, body, &out)
		} else {
			err = client.PostJSON(ctx, resolved, body, &out)
		}
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})
}
