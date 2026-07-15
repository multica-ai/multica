// CEREBRO-PATCH(cerebro-eval-mcp): FIR-3308 eval catalog parity for local and gateway MCP.
package clitools

import (
	"context"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerEvalTools(srv *mcp.Server, client *cli.APIClient) {
	object := map[string]any{"type": "object", "description": "Complete JSON document accepted by the Cerebro Evals API."}
	id := func(name, description string) map[string]any {
		return map[string]any{name: map[string]any{"type": "string", "description": description}}
	}

	registerEvalGet(srv, client, "list_evals", "List versioned eval contracts in the current workspace.", "/api/cerebro/evals", nil)
	registerEvalGet(srv, client, "get_eval", "Get one versioned eval contract.", "/api/cerebro/evals/{eval_id}", id("eval_id", "Eval UUID"))
	registerEvalMutation(srv, client, "create_eval", "Create a versioned eval contract.", "POST", "/api/cerebro/evals", "eval", object, nil)
	registerEvalMutation(srv, client, "update_eval", "Replace a versioned eval contract.", "PUT", "/api/cerebro/evals/{eval_id}", "eval", object, id("eval_id", "Eval UUID"))
	registerEvalDelete(srv, client, "delete_eval", "Delete an eval contract.", "/api/cerebro/evals/{eval_id}", "eval_id")
	registerEvalGet(srv, client, "list_eval_runs", "List immutable runs for one eval.", "/api/cerebro/evals/{eval_id}/runs", id("eval_id", "Eval UUID"))
	registerEvalMutation(srv, client, "record_eval_run", "Record an immutable eval run, including issue/workflow links and result evidence.", "POST", "/api/cerebro/evals/{eval_id}/runs", "run", object, id("eval_id", "Eval UUID"))
	registerEvalGet(srv, client, "list_eval_bindings", "List workflow-to-eval gate bindings.", "/api/cerebro/evals/bindings", nil)
	registerEvalMutation(srv, client, "bind_eval", "Bind an eval to a workflow phase as blocking or advisory.", "POST", "/api/cerebro/evals/bindings", "binding", object, nil)
	registerEvalDelete(srv, client, "unbind_eval", "Delete a workflow-to-eval binding.", "/api/cerebro/evals/bindings/{binding_id}", "binding_id")
}

func registerEvalGet(srv *mcp.Server, client *cli.APIClient, name, description, path string, properties map[string]any) {
	if properties == nil {
		properties = map[string]any{}
	}
	required := make([]string, 0, len(properties))
	for key := range properties {
		required = append(required, key)
	}
	srv.RegisterTool(mcp.Tool{Name: name, Description: description, InputSchema: map[string]any{"type": "object", "properties": properties, "required": required}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		resolved, err := resolveEvalPath(path, args)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var out any
		if err := client.GetJSON(ctx, resolved, &out); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(out)
	})
}

func registerEvalMutation(srv *mcp.Server, client *cli.APIClient, name, description, method, path, bodyKey string, bodySchema map[string]any, properties map[string]any) {
	if properties == nil {
		properties = map[string]any{}
	}
	properties[bodyKey] = bodySchema
	required := make([]string, 0, len(properties))
	for key := range properties {
		required = append(required, key)
	}
	srv.RegisterTool(mcp.Tool{Name: name, Description: description, InputSchema: map[string]any{"type": "object", "properties": properties, "required": required}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		resolved, err := resolveEvalPath(path, args)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		body, ok := args[bodyKey].(map[string]any)
		if !ok {
			return mcp.ErrorResult(bodyKey + " must be an object"), nil
		}
		var out any
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

func registerEvalDelete(srv *mcp.Server, client *cli.APIClient, name, description, path, idKey string) {
	srv.RegisterTool(mcp.Tool{Name: name, Description: description, InputSchema: map[string]any{"type": "object", "required": []string{idKey}, "properties": map[string]any{idKey: map[string]any{"type": "string"}}}}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		resolved, err := resolveEvalPath(path, args)
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		if err := client.DeleteJSON(ctx, resolved); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(map[string]any{"deleted": args[idKey]})
	})
}

func resolveEvalPath(path string, args map[string]any) (string, error) {
	for _, key := range []string{"eval_id", "binding_id"} {
		token := "{" + key + "}"
		if !strings.Contains(path, token) {
			continue
		}
		value, err := requireString(args, key)
		if err != nil {
			return "", err
		}
		path = strings.Replace(path, token, url.PathEscape(value), 1)
	}
	return path, nil
}
