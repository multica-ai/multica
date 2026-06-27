package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type RuntimeToolScanTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type RuntimeToolScanServer struct {
	Name  string                `json:"name"`
	Tools []RuntimeToolScanTool `json:"tools,omitempty"`
	Error string                `json:"error,omitempty"`
}

func (c *Client) GetRuntimeMcpConfig(ctx context.Context, runtimeID string) (json.RawMessage, error) {
	var resp struct {
		ToolsConfig json.RawMessage `json:"tools_config"`
		McpConfig   json.RawMessage `json:"mcp_config"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/mcp-config", runtimeID), &resp); err != nil {
		return nil, err
	}
	if len(resp.ToolsConfig) > 0 {
		return resp.ToolsConfig, nil
	}
	return resp.McpConfig, nil
}

func (c *Client) ReportRuntimeToolScan(ctx context.Context, runtimeID string, scannedAt time.Time, servers []RuntimeToolScanServer) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/tool-scan", runtimeID), map[string]any{
		"scanned_at": scannedAt.Format(time.RFC3339Nano),
		"servers":    servers,
	}, nil)
}
