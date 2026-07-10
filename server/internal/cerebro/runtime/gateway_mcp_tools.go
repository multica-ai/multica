package runtime

// gateway_mcp_tools.go — FIR-2441 fix-list #4.
//
// Bridges the resolver's out.MCPServers (the Allow-only mcp_http connections)
// into a cloud runtime's (Firtal Gateway) in-process tool registry, so external
// MCP tools finally reach an agent running on the gateway — previously they
// reached only local runtimes via the CLI's --mcp-config. Each discovered tool
// becomes a registry Tool named "mcp__<connection>__<tool>" (the same token the
// Deny list and the CLIs use), dispatched through mcpHTTPClient at call time.
//
// Availability is fail-open, security is not: a connection whose live server
// cannot be reached (or whose tools/list fails) is skipped with a warning so the
// task still runs with its other tools. That is safe because the resolver has
// already applied policy — out.MCPServers only ever contains fully-Allow
// connections, so skipping one can only REMOVE access, never widen it.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// mcpServersDoc is the shape connections.MCPServerEntry marshals into
// out.MCPServers: {"mcpServers": {"<name>": {"type","url","headers"}}}.
type mcpServersDoc struct {
	MCPServers map[string]mcpServerEntryDoc `json:"mcpServers"`
}

type mcpServerEntryDoc struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// gatewayMCPTool adapts one discovered mcp_http tool to the gateway Tool
// interface. The exposed name is namespaced (mcp__conn__tool); Call dispatches
// the bare tool name against the connection's client.
type gatewayMCPTool struct {
	exposedName string
	toolName    string
	description string
	inputSchema map[string]any
	client      *mcpHTTPClient
}

func (t *gatewayMCPTool) Name() string        { return t.exposedName }
func (t *gatewayMCPTool) Description() string { return t.description }

func (t *gatewayMCPTool) InputSchema() map[string]any {
	if t.inputSchema != nil {
		return t.inputSchema
	}
	// The gateway Anthropic schema normalizer expects an object schema; a tool
	// that declared none still gets a valid one.
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t *gatewayMCPTool) Call(ctx context.Context, args map[string]any) (string, error) {
	res, err := t.client.CallTool(ctx, t.toolName, args)
	if err != nil {
		return "", err
	}
	text := mcpResultText(res)
	if res.IsError {
		if text == "" {
			text = "tool reported an error"
		}
		return "", errFromText(text)
	}
	return text, nil
}

// errFromText wraps a tool-level MCP error string as an error, mirroring how the
// bridge adapter surfaces mcp.Server IsError results.
func errFromText(text string) error { return &mcpToolError{text} }

type mcpToolError struct{ msg string }

func (e *mcpToolError) Error() string { return e.msg }

// buildGatewayMCPTools discovers and adapts every tool on the Allow-only
// mcp_http connections in servers (the resolver's out.MCPServers document). It
// returns nil when the document is empty or unparseable. hc may be nil (a
// default client is used). A per-connection discovery failure is logged and
// skipped, never fatal.
func buildGatewayMCPTools(ctx context.Context, servers json.RawMessage, hc *http.Client, logger *slog.Logger) []Tool {
	if len(servers) == 0 {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	var doc mcpServersDoc
	if err := json.Unmarshal(servers, &doc); err != nil {
		logger.Warn("gateway mcp tools: unparseable mcpServers document (skipping external mcp tools)", "error", err)
		return nil
	}
	var out []Tool
	for name, entry := range doc.MCPServers {
		if entry.URL == "" {
			continue
		}
		client := newMCPHTTPClient(entry.URL, entry.Headers, hc)
		tools, err := client.ListTools(ctx)
		if err != nil {
			// Fail-open on availability: the task keeps its other tools. Security
			// is unaffected — the resolver already restricted this to Allow-only.
			logger.Warn("gateway mcp tools: connection tool discovery failed (skipped)",
				"connection", name, "error", err)
			continue
		}
		for _, t := range tools {
			out = append(out, &gatewayMCPTool{
				exposedName: toolpolicy.MCPToolToken(name, t.Name),
				toolName:    t.Name,
				description: t.Description,
				inputSchema: t.InputSchema,
				client:      client,
			})
		}
	}
	return out
}
