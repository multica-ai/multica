package main

// CEREBRO-PATCH(mcp-agent-capabilities): TECH-3642 `get_agent_capabilities` MCP
// tool — the MCP surface of the unified per-agent capabilities card. Thin client
// over GET /api/agents/{id}/capabilities that returns the endpoint payload
// verbatim, so an agent inspecting another agent via MCP sees byte-for-byte what
// the CLI and the dashboard show.

import (
	"context"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerCerebroAgentCapabilitiesTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name: "get_agent_capabilities",
		Description: `Get an agent's capabilities card: what it can do (skills), may use (tools), has access to (credentials — names/types only, never secret values), and is limited by (sandbox + MCP servers). Use this to understand what an agent is set up to handle before assigning or routing work to it.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"agent_id"},
			"properties": map[string]any{
				"agent_id": map[string]any{"type": "string", "description": "Agent ID (UUID)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "agent_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}

		var caps any
		if err := client.GetJSON(ctx, "/api/agents/"+id+"/capabilities", &caps); err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		return jsonText(caps)
	})
}
