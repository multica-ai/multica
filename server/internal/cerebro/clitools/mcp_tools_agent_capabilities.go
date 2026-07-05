package clitools

// CEREBRO-PATCH(mcp-agent-capabilities): TECH-3642 `get_agent_capabilities` MCP
// tool — the MCP surface of the unified per-agent capabilities card. Thin client
// over GET /api/agents/{id}/capabilities that returns the endpoint payload
// verbatim, so an agent inspecting another agent via MCP sees byte-for-byte what
// the CLI and the dashboard show.

import (
	"context"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerCerebroAgentCapabilitiesTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name:        "get_agent_capabilities",
		Description: `Get YOUR OWN capabilities card — what you can do (skills), may use (tools, with allow/ask/deny), have access to (credentials — names/types only, never secret values), and are limited by (sandbox + MCP servers). Omit agent_id to inspect yourself; pass agent_id only to inspect another agent before routing work to it. Call this whenever you are unsure what you are allowed to do.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{"type": "string", "description": "Agent ID (UUID). Omit to inspect yourself (defaults to your own agent via MULTICA_AGENT_ID)."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id := strings.TrimSpace(optString(args, "agent_id"))
		if id == "" {
			id = strings.TrimSpace(os.Getenv("MULTICA_AGENT_ID"))
		}
		if id == "" {
			return mcp.ErrorResult("agent_id is required (omit it only when MULTICA_AGENT_ID is set, i.e. running as an agent)"), nil
		}

		var caps any
		if err := client.GetJSON(ctx, "/api/agents/"+id+"/capabilities", &caps); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(caps)
	})
}
