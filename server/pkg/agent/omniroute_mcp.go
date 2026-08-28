package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

type omniRouteMCP interface {
	ListTools(context.Context) ([]omniRouteToolSpec, error)
	CallTool(context.Context, string, map[string]interface{}) (string, error)
}

type omniRouteToolBinding struct {
	client *omniRouteMCPClient
	name   string
}

type omniRouteToolRegistry struct {
	tools    []omniRouteToolSpec
	bindings map[string]omniRouteToolBinding
}

func buildOmniRouteToolRegistry(ctx context.Context, raw json.RawMessage, httpClient *http.Client, allowed []string, configured bool) (omniRouteToolRegistry, error) {
	clients, err := newOmniRouteMCPClients(raw, httpClient)
	if err != nil {
		return omniRouteToolRegistry{}, err
	}
	registry := omniRouteToolRegistry{bindings: make(map[string]omniRouteToolBinding)}
	for _, client := range clients {
		tools, err := client.ListTools(ctx)
		if err != nil {
			return omniRouteToolRegistry{}, err
		}
		for _, tool := range tools {
			name := tool.Function.Name
			if name == "" || (configured && !omniRouteToolAllowed(name, allowed)) {
				continue
			}
			if _, exists := registry.bindings[name]; exists {
				return omniRouteToolRegistry{}, fmt.Errorf("duplicate MCP tool name %q", name)
			}
			registry.tools = append(registry.tools, tool)
			registry.bindings[name] = omniRouteToolBinding{client: client, name: name}
		}
	}
	return registry, nil
}

func omniRouteToolAllowed(name string, allowed []string) bool {
	for _, pattern := range allowed {
		pattern = strings.TrimSpace(pattern)
		if pattern == name || pattern == "*" {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(name, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

type omniRouteMCPServerConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type omniRouteMCPConfig struct {
	MCPServers map[string]omniRouteMCPServerConfig `json:"mcpServers"`
}

type omniRouteMCPClient struct {
	serverURL string
	headers   map[string]string
	http      *http.Client
	seq       atomic.Int64
	sessionID atomic.Value
}

type omniRouteJSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type omniRouteJSONRPCResponse struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      json.RawMessage    `json:"id"`
	Result  json.RawMessage    `json:"result"`
	Error   *omniRouteRPCError `json:"error,omitempty"`
}

type omniRouteRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type omniRouteMCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

func newOmniRouteMCPClients(raw json.RawMessage, httpClient *http.Client) (map[string]*omniRouteMCPClient, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var config omniRouteMCPConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("omniroute MCP: parse config: %w", err)
	}
	clients := make(map[string]*omniRouteMCPClient, len(config.MCPServers))
	for name, server := range config.MCPServers {
		if strings.TrimSpace(server.URL) == "" {
			return nil, fmt.Errorf("omniroute MCP server %q: only remote servers with url are supported", name)
		}
		if httpClient == nil {
			httpClient = http.DefaultClient
		}
		clients[name] = &omniRouteMCPClient{serverURL: strings.TrimRight(server.URL, "/"), headers: server.Headers, http: httpClient}
	}
	return clients, nil
}

func (c *omniRouteMCPClient) rpc(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.seq.Add(1)
	body, err := json.Marshal(omniRouteJSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	if sessionID, ok := c.sessionID.Load().(string); ok && sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request: %w", method, err)
	}
	defer resp.Body.Close()
	if value := resp.Header.Get("Mcp-Session-Id"); value != "" {
		c.sessionID.Store(value)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s HTTP %d: %s", method, resp.StatusCode, sanitizedHTTPError(resp.Body))
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", method, err)
	}
	var response omniRouteJSONRPCResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", method, err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%s RPC %d: %s", method, response.Error.Code, response.Error.Message)
	}
	return response.Result, nil
}

func (c *omniRouteMCPClient) initialize(ctx context.Context) error {
	if _, ok := c.sessionID.Load().(string); ok {
		return nil
	}
	_, err := c.rpc(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "multica", "version": "phase2"},
	})
	if err != nil {
		return err
	}
	// Notifications do not require a response. Servers that use a session
	// should already have returned Mcp-Session-Id from initialize.
	return nil
}

func (c *omniRouteMCPClient) ListTools(ctx context.Context) ([]omniRouteToolSpec, error) {
	if err := c.initialize(ctx); err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	result, err := c.rpc(ctx, "tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []omniRouteMCPTool `json:"tools"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode tools/list result: %w", err)
	}
	out := make([]omniRouteToolSpec, 0, len(payload.Tools))
	for _, tool := range payload.Tools {
		out = append(out, omniRouteToolSpec{Type: "function", Function: omniRouteFunctionSpec{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema}})
	}
	return out, nil
}

func (c *omniRouteMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	if err := c.initialize(ctx); err != nil {
		return "", fmt.Errorf("initialize: %w", err)
	}
	result, err := c.rpc(ctx, "tools/call", map[string]interface{}{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var payload struct {
		Content []struct {
			Type string      `json:"type"`
			Text string      `json:"text,omitempty"`
			Data interface{} `json:"data,omitempty"`
		} `json:"content"`
		IsError bool `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", fmt.Errorf("decode tools/call result: %w", err)
	}
	var parts []string
	for _, item := range payload.Content {
		if item.Text != "" {
			parts = append(parts, item.Text)
		} else if item.Data != nil {
			encoded, _ := json.Marshal(item.Data)
			parts = append(parts, string(encoded))
		}
	}
	output := strings.Join(parts, "\n")
	if payload.IsError {
		return output, fmt.Errorf("MCP tool %q returned an error", name)
	}
	return output, nil
}
