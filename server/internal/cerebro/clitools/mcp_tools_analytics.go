package clitools

// CEREBRO-PATCH(analytics-mcp): FIR-2996 canonical analytics MCP tools.

import (
	"context"
	"encoding/json"
	"fmt"

	cerebroanalytics "github.com/multica-ai/multica/server/internal/cerebro/analytics"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func registerAnalyticsTools(server *mcp.Server, client *cli.APIClient) {
	server.RegisterTool(mcp.Tool{
		Name:        "analytics_catalog",
		Description: "List the populations, metrics, dimensions, grains, and filter operators accepted by analytics_query.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, _ map[string]any) (mcp.CallToolResult, error) {
		var catalog cerebroanalytics.Catalog
		if err := client.GetJSON(ctx, "/api/analytics/catalog", &catalog); err != nil {
			return mcp.ErrorResult(fmt.Sprintf("analytics catalog: %v", err)), nil
		}
		return analyticsToolResult(catalog), nil
	})

	server.RegisterTool(mcp.Tool{
		Name:        "analytics_query",
		Description: "Query workspace run analytics using catalog metrics, dimensions, filters, time grain, sorting, and cursor pagination.",
		InputSchema: analyticsQueryInputSchema(),
	}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		body, err := json.Marshal(args)
		if err != nil {
			return mcp.ErrorResult("invalid analytics query"), nil
		}
		var query cerebroanalytics.Query
		if err := json.Unmarshal(body, &query); err != nil {
			return mcp.ErrorResult("invalid analytics query"), nil
		}
		var result cerebroanalytics.QueryResult
		if err := client.PostJSON(ctx, "/api/analytics/query", query, &result); err != nil {
			return mcp.ErrorResult(fmt.Sprintf("analytics query: %v", err)), nil
		}
		return analyticsToolResult(result), nil
	})
}

func analyticsToolResult(value any) mcp.CallToolResult {
	data, _ := json.Marshal(value)
	return mcp.CallToolResult{Content: []mcp.Content{{Type: "text", Text: string(data)}}, StructuredContent: data}
}

func analyticsQueryInputSchema() map[string]any {
	stringArray := func() map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	}
	return map[string]any{
		"type":     "object",
		"required": []string{"population"},
		"properties": map[string]any{
			"population": map[string]any{"type": "string", "enum": []string{"agent", "gateway", "all"}},
			"metrics":    stringArray(), "dimensions": stringArray(),
			"filters": map[string]any{"type": "array", "items": map[string]any{"type": "object", "required": []string{"dimension", "operator", "values"}, "properties": map[string]any{"dimension": map[string]any{"type": "string"}, "operator": map[string]any{"type": "string"}, "values": stringArray()}}},
			"grain":   map[string]any{"type": "string"}, "sort": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "page": map[string]any{"type": "object"}, "timezone": map[string]any{"type": "string"},
		},
	}
}
