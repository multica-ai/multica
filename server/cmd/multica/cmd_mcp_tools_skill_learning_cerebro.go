package main

// TECH-3077: MCP tools for skill self-learning.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerSkillLearningTools(srv *mcp.Server, client *cli.APIClient) {
	// -----------------------------------------------------------------------
	// skill_get_observations
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_get_observations",
		Description: `Get behavioral patterns observed for a skill during automation runs.
Shows what agents consistently do beyond the skill's instructions — the raw data
behind auto-learning change-request proposals.

Returns:
- patterns: delta types with run counts and consistency percentages
- recent: last 20 raw observations

Use this to understand what a skill is learning from in practice.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id"},
			"properties": map[string]any{
				"skill_id": map[string]any{
					"type":        "string",
					"description": "Skill ID or name",
				},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		id, err := requireString(args, "skill_id")
		if err != nil {
			return mcp.ErrorResult(err.Error()), nil
		}
		var result any
		if err := client.GetJSON(ctx, "/api/skills/"+id+"/observations", &result); err != nil {
			return mcp.ErrorResult(fmt.Sprintf("get observations: %v", err)), nil
		}
		b, _ := json.Marshal(result)
		return mcp.TextResult(string(b)), nil
	})

	// -----------------------------------------------------------------------
	// skill_record_observation
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_record_observation",
		Description: `Record a delta behavior observed during a skill run.
Call this from the agent runtime when you detect that an agent did something
beyond a skill's instructions — accessing an extra table, calling an extra sub-skill, etc.

This feeds the self-learning system: after 10 runs with 70%+ consistency,
an auto change-request is proposed to the skill owner.

delta_type values:
- extra_table_access: agent queried a BigQuery table not in skill's data_domains
- skill_chain_addition: agent called a sub-skill not in skill instructions
- extra_verification_step: agent consistently ran an extra check
- output_format_deviation: agent returned output in a different format`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id", "delta_type", "delta_value"},
			"properties": map[string]any{
				"skill_id":      map[string]any{"type": "string", "description": "Skill ID"},
				"delta_type":    map[string]any{"type": "string", "enum": []string{"extra_table_access", "skill_chain_addition", "extra_verification_step", "output_format_deviation"}},
				"delta_value":   map[string]any{"type": "string", "description": "What was observed, e.g. 'firtal_dwh.product_margin_daily'"},
				"skill_version": map[string]any{"type": "string", "description": "Current skill version (from skill frontmatter)"},
				"run_id":        map[string]any{"type": "string", "description": "Optional: current task/run ID"},
				"autopilot_id":  map[string]any{"type": "string", "description": "Optional: autopilot ID if triggered by one"},
				"confidence":    map[string]any{"type": "number", "description": "Confidence 0.0–1.0 (default 1.0)"},
				"outcome":       map[string]any{"type": "string", "description": "success or failure (default success)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		payload := map[string]any{
			"skill_id":      optString(args, "skill_id"),
			"delta_type":    optString(args, "delta_type"),
			"delta_value":   optString(args, "delta_value"),
			"skill_version": optString(args, "skill_version"),
			"run_id":        optString(args, "run_id"),
			"autopilot_id":  optString(args, "autopilot_id"),
			"confidence":    optFloat(args, "confidence", 1.0),
			"outcome":       optString(args, "outcome"),
		}
		if payload["skill_id"] == "" || payload["delta_type"] == "" || payload["delta_value"] == "" {
			return mcp.ErrorResult("skill_id, delta_type, and delta_value are required"), nil
		}
		var result any
		if err := client.PostJSON(ctx, "/api/skill-observations", payload, &result); err != nil {
			return mcp.ErrorResult(fmt.Sprintf("record observation: %v", err)), nil
		}
		return mcp.TextResult("recorded"), nil
	})
}

func optFloat(args map[string]any, key string, def float64) float64 {
	v, ok := args[key]
	if !ok {
		return def
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return def
}
