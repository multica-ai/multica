package clitools

import (
	"context"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerWorkflowHookTools(srv *mcp.Server, client *cli.APIClient) {
	hookSchema := map[string]any{
		"type":        "object",
		"description": "Complete Workflow hook policy document",
		"properties": map[string]any{
			"condition_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"all", "any"},
				"default":     "all",
				"description": "Whether all conditions or any condition must match.",
			},
			"revision": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "Current immutable Draft revision for a revision-safe update.",
			},
		},
	}
	testSchema := map[string]any{
		"type":        "object",
		"description": "Exact saved Draft revision and one retained compatible event. Test is planning-only and has no side effects.",
		"properties": map[string]any{
			"event_id": map[string]any{
				"type":        "string",
				"description": "Retained event UUID returned in compatible_events by get_workflow_hook.",
			},
			"revision": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "Exact saved Draft revision to test.",
			},
		},
		"required": []string{"event_id", "revision"},
	}
	registerHookGet(srv, client, "list_workflow_hooks", "List visible Workflow hooks.", "", false)
	registerHookGet(srv, client, "get_workflow_hook", "Get one Workflow hook.", "", true)
	registerHookMutation(srv, client, "create_workflow_hook", "Create a Workflow hook in Dry run.", httpPost, "", hookSchema, false)
	registerHookMutation(srv, client, "update_workflow_hook", "Update a Workflow hook as a new Dry run version.", httpPut, "", hookSchema, true)
	registerHookMutation(srv, client, "test_workflow_hook", "Test an exact saved Workflow Hook Draft revision against a retained event. Planning-only; no side effects.", httpPost, "/test", testSchema, true)
	registerHookMutation(srv, client, "publish_workflow_hook", "Publish a tested Workflow hook. Human-only permission is enforced by the server.", httpPost, "/publish", nil, true)
	registerHookGet(srv, client, "get_effective_workflow_hooks", "Get the effective bindings for one Workflow hook.", "/effective", true)
	registerHookGet(srv, client, "list_workflow_hook_runs", "List audit runs for one Workflow hook.", "/runs", true)
}

type hookHTTPMethod string

const (
	httpPost hookHTTPMethod = "POST"
	httpPut  hookHTTPMethod = "PUT"
)

func registerHookGet(srv *mcp.Server, client *cli.APIClient, name, description, suffix string, needsID bool) {
	properties := map[string]any{}
	required := []string{}
	if needsID {
		properties["hook_id"] = map[string]any{"type": "string", "description": "Workflow hook UUID"}
		required = append(required, "hook_id")
	}
	srv.RegisterTool(mcp.Tool{Name: name, Description: description, InputSchema: map[string]any{"type": "object", "properties": properties, "required": required}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		path := "/api/cerebro/workflow-hooks"
		if needsID {
			id, err := requireString(args, "hook_id")
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			path += "/" + url.PathEscape(id) + suffix
		}
		var out any
		if err := client.GetJSON(ctx, path, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})
}

func registerHookMutation(srv *mcp.Server, client *cli.APIClient, name, description string, method hookHTTPMethod, suffix string, documentSchema map[string]any, needsID bool) {
	properties := map[string]any{}
	required := []string{}
	if documentSchema != nil {
		properties["hook"] = documentSchema
		required = append(required, "hook")
	}
	if needsID {
		properties["hook_id"] = map[string]any{"type": "string", "description": "Workflow hook UUID"}
		required = append(required, "hook_id")
	}
	srv.RegisterTool(mcp.Tool{Name: name, Description: description, InputSchema: map[string]any{"type": "object", "properties": properties, "required": required}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		path := "/api/cerebro/workflow-hooks"
		if needsID {
			id, err := requireString(args, "hook_id")
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			path += "/" + url.PathEscape(id) + suffix
		}
		body, _ := args["hook"].(map[string]any)
		if body == nil {
			body = map[string]any{}
		}
		var out any
		var err error
		if method == httpPut {
			err = client.PutJSON(ctx, path, body, &out)
		} else {
			err = client.PostJSON(ctx, path, body, &out)
		}
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})
}
