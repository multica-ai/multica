package main

// TECH-3077: MCP tool for skill automation impact analysis.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerSkillAutomationTools(srv *mcp.Server, client *cli.APIClient) {
	srv.RegisterTool(mcp.Tool{
		Name: "skill_impact_analysis",
		Description: `Show which automations (autopilots, cron-jobs) depend on a skill.
Use this before modifying a skill to understand what will break.
Returns: automation count, list of automation names/types/schedules.

Example: skill_impact_analysis(skill_id="firtal-sales") shows that the
morning-report autopilot depends on this skill.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id"},
			"properties": map[string]any{
				"skill_id": map[string]any{
					"type":        "string",
					"description": "Skill ID or name (e.g. 'firtal-sales' or a UUID)",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result any
		if err := client.GetJSON(ctx, "/api/skills/"+id+"/impact", &result); err != nil {
			return mcp.ErrorResult(fmt.Sprintf("skill impact: %v", err)), nil
		}
		b, _ := json.Marshal(result)
		return mcp.TextResult(string(b)), nil
	})
}
