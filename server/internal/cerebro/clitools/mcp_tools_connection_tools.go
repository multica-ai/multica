package clitools

// CEREBRO-PATCH(mcp-connection-tools): FIR-2273 — register api-type workspace
// connection endpoints as MCP tools on the Multica MCP server (`multica mcp
// serve`), so a LOCAL runtime (a laptop) can call them WITHOUT switching to the
// Firtal Gateway.
//
// At registration time we GET /api/cerebro/connection-tools — the server returns
// only the endpoints THIS agent is allowed (feature-flagged + default-deny per
// agent). For each, we register an MCP tool whose handler POSTs {tool, arguments}
// to /api/cerebro/connection-tools/call; the server re-checks the gate and
// dispatches the HTTP call SERVER-SIDE (the credential never reaches this
// process, and `.internal` URLs stay reachable from the backend).
//
// This is ADDITIVE and best-effort: if the list call fails, returns nothing, or
// the caller is not an agent (no mat_ task token), we register nothing and never
// break the rest of the MCP tool loop. Tool names are kept verbatim from the
// server (e.g. infisical_admin__get_secrets) so the model sees one consistent
// name across the Gateway and the MCP server.

import (
	"context"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// connectionToolDescriptor mirrors the server's toolDescriptor JSON shape.
type connectionToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type connectionToolsListResponse struct {
	Tools []connectionToolDescriptor `json:"tools"`
}

type connectionToolsCallResponse struct {
	Result string `json:"result"`
}

// registerConnectionTools discovers the caller's allowed api-type connection
// endpoints and registers one MCP tool per endpoint. Never fatal: any failure
// leaves the rest of the tool set intact.
func registerConnectionTools(srv *mcp.Server, client *cli.APIClient) {
	var list connectionToolsListResponse
	// Best-effort: a non-agent token, the flag being off, or any transient error
	// resolves to "no tools" (the server returns an empty list, or this call
	// errors) — register nothing and move on.
	if err := client.GetJSON(context.Background(), "/api/cerebro/connection-tools", &list); err != nil {
		return
	}
	for _, t := range list.Tools {
		if t.Name == "" {
			continue
		}
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		toolName := t.Name // capture per iteration for the closure
		srv.RegisterTool(mcp.Tool{
			Name:        toolName,
			Description: t.Description,
			InputSchema: schema,
		}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
			if args == nil {
				args = map[string]any{}
			}
			body := map[string]any{"tool": toolName, "arguments": args}
			var resp connectionToolsCallResponse
			if err := client.PostJSON(ctx, "/api/cerebro/connection-tools/call", body, &resp); err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			return mcp.TextResult(resp.Result), nil
		})
	}
}
