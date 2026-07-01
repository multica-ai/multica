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
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

// connectionToolsStatusToolName is a self-describing status tool that is ALWAYS
// registered (FIR-2388), so "0 connection tools" is never silent: an agent that
// expected an api-connection tool can call it to learn WHY none loaded.
const connectionToolsStatusToolName = "multica_connection_tools_status"

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
// endpoints and registers one MCP tool per endpoint, PLUS an always-on status
// tool (FIR-2388) so a zero-tool outcome is never silent. Never fatal: any
// failure still registers the status tool and leaves the rest of the tool set
// intact.
func registerConnectionTools(srv *mcp.Server, client *cli.APIClient) {
	var list connectionToolsListResponse
	// Best-effort: a non-agent token, the flag being off, or any transient error
	// resolves to "no tools" (the server returns an empty list, or this call
	// errors). Whatever the outcome, the status tool below explains it.
	listErr := client.GetJSON(context.Background(), "/api/cerebro/connection-tools", &list)

	registered := 0
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
		registered++
	}

	// Always register the status tool, even on error or an empty list, so the
	// agent can ask why it sees no connection tools instead of assuming it has
	// none. No tokens/headers/bodies are ever included in the message.
	status := connectionToolsStatusMessage(listErr, registered)
	srv.RegisterTool(mcp.Tool{
		Name:        connectionToolsStatusToolName,
		Description: "Report whether this runtime's API-connection tools loaded and how many. Call it when you expected a connection tool (for example an Infisical or data-registry endpoint) but do not see it, to learn whether the feature is off, you lack permission, or discovery failed.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult(status), nil
	})
}

// connectionToolsStatusMessage renders the status tool's reply from the discovery
// outcome. It never leaks secrets: on error it surfaces only the transport error
// string (no tokens/headers/bodies pass through this path).
func connectionToolsStatusMessage(listErr error, count int) string {
	switch {
	case listErr != nil:
		return fmt.Sprintf("Connection tools were not loaded: %s. This runtime will show no API-connection tools until that succeeds.", strings.TrimSpace(listErr.Error()))
	case count == 0:
		return "Connection tools loaded: 0. Check that the cerebro_api_connection_tools feature flag is on for this workspace, that you have Allow permission on the connection's endpoints, and that this runtime is running under a task token."
	default:
		return fmt.Sprintf("Connection tools loaded: %d.", count)
	}
}
