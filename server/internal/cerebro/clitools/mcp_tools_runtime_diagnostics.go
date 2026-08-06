package clitools

// CEREBRO-PATCH(mcp-runtime-access-diagnostics): FIR-4293 read-only MCP
// projection of the same provider probe + MCP discovery REST contract used by
// Runtime UI and `multica runtime diagnostics`.

import (
	"context"
	"net/url"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerRuntimeAccessDiagnosticsTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name:        "get_runtime_access_diagnostics",
		Description: "Read one Runtime's provider capability probe and MCP tools/list diagnostics, including inventory versions, affected capabilities, source policy and recovery. This is diagnostic only and never changes Settings → Permissions.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"runtime_id"},
			"properties": map[string]any{
				"runtime_id": map[string]any{"type": "string", "description": "Runtime UUID."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		runtimeID, err := requireString(args, "runtime_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var report any
		if err := client.GetJSON(ctx, "/api/runtimes/"+url.PathEscape(runtimeID)+"/access-diagnostics", &report); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(report)
	})
}
