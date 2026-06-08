package main

// TECH-3077: MCP tools for skill metadata filtering and audit.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerSkillMetadataTools(srv *mcp.Server, client *cli.APIClient) {
	// -----------------------------------------------------------------------
	// skill_list_filtered
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name: "skill_list_filtered",
		Description: `List workspace skills filtered by category, domain, tag, status, or data domain.
Returns skills with their metadata (category, domain, type, status, tags).
Use this instead of skill_list when you know the domain or category you need.

Examples:
- Find all data analysis skills: domain="data"
- Find all deploy skills: category="Deploy & Infra"
- Find skills tagged "firtal": tag="firtal"
- Find skills that access a BQ table: data_domain="firtal-dwh"`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"category":    map[string]any{"type": "string", "description": "Filter by category, e.g. 'Data & Analyse', 'Deploy & Infra', 'Agent-governance'"},
				"domain":      map[string]any{"type": "string", "description": "Filter by domain: data | deploy | design | governance | marketing | security | platform | communication | research"},
				"tag":         map[string]any{"type": "string", "description": "Filter by a single tag value"},
				"status":      map[string]any{"type": "string", "description": "Filter by status: stable | beta | deprecated"},
				"data_domain": map[string]any{"type": "string", "description": "Filter by BigQuery project or project.dataset, e.g. 'firtal-dwh' or 'firtal-dwh.object_order'"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		var parts []string
		for _, k := range []string{"category", "domain", "tag", "status", "data_domain"} {
			if v, ok := args[k].(string); ok && v != "" {
				parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
		}
		if len(parts) == 0 {
			return mcp.ErrorResult("at least one filter parameter is required — use skill_list for unfiltered results"), nil
		}
		path := "/api/skills?" + strings.Join(parts, "&")
		var result any
		if err := client.GetJSON(ctx, path, &result); err != nil {
			return mcp.ErrorResult(fmt.Sprintf("list skills: %v", err)), nil
		}
		b, _ := json.Marshal(result)
		return mcp.TextResult(string(b)), nil
	})

	// -----------------------------------------------------------------------
	// skill_audit
	// -----------------------------------------------------------------------
	srv.RegisterTool(mcp.Tool{
		Name:        "skill_audit",
		Description: `List skills that are missing required metadata fields (category, domain, or status). Use before running a bulk metadata update.`,
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ map[string]any) (mcp.CallToolResult, error) {
		var result any
		if err := client.GetJSON(ctx, "/api/skills/audit", &result); err != nil {
			return mcp.ErrorResult(fmt.Sprintf("skill audit: %v", err)), nil
		}
		b, _ := json.Marshal(result)
		return mcp.TextResult(string(b)), nil
	})
}
