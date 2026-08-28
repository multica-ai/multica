package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
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
	clients  map[string]*omniRouteMCPClient
}

func buildOmniRouteToolRegistry(ctx context.Context, raw json.RawMessage, httpClient *http.Client, cwd string, allowed []string, configured bool) (omniRouteToolRegistry, error) {
	clients, err := newOmniRouteMCPClients(raw, httpClient, cwd)
	if err != nil {
		return omniRouteToolRegistry{}, err
	}
	registry := omniRouteToolRegistry{bindings: make(map[string]omniRouteToolBinding), clients: clients}
	for _, client := range clients {
		tools, err := client.ListTools(ctx)
		if err != nil {
			registry.close()
			return omniRouteToolRegistry{}, err
		}
		for _, tool := range tools {
			rawName := tool.Function.Name
			if rawName == "" {
				continue
			}
			name := omniRouteMCPToolName(client.serverName, rawName)
			if configured && !omniRouteToolAllowed(name, allowed) && !omniRouteToolAllowed(rawName, allowed) {
				continue
			}
			if _, exists := registry.bindings[name]; exists {
				registry.close()
				return omniRouteToolRegistry{}, fmt.Errorf("duplicate MCP tool name %q", name)
			}
			tool.Function.Name = name
			registry.tools = append(registry.tools, tool)
			registry.bindings[name] = omniRouteToolBinding{client: client, name: rawName}
		}
	}
	return registry, nil
}

func (r omniRouteToolRegistry) close() {
	for _, client := range r.clients {
		client.close()
	}
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
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type omniRouteMCPConfig struct {
	MCPServers map[string]omniRouteMCPServerConfig `json:"mcpServers"`
}

type omniRouteMCPClient struct {
	serverName  string
	serverURL   string
	command     []string
	env         map[string]string
	headers     map[string]string
	cwd         string
	http        *http.Client
	seq         atomic.Int64
	sessionID   atomic.Value
	stdioMu     sync.Mutex
	stdioCmd    *exec.Cmd
	stdioIn     io.WriteCloser
	stdioOut    *json.Decoder
	initialized atomic.Bool
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

func newOmniRouteMCPClients(raw json.RawMessage, httpClient *http.Client, cwd string) (map[string]*omniRouteMCPClient, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var config omniRouteMCPConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("omniroute MCP: parse config: %w", err)
	}
	clients := make(map[string]*omniRouteMCPClient, len(config.MCPServers))
	names := make([]string, 0, len(config.MCPServers))
	for name := range config.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		server := config.MCPServers[name]
		if httpClient == nil {
			httpClient = http.DefaultClient
		}
		url := strings.TrimSpace(server.URL)
		command := strings.TrimSpace(server.Command)
		if url == "" && command == "" {
			return nil, fmt.Errorf("omniroute MCP server %q: requires url or command", name)
		}
		if url != "" && command != "" {
			return nil, fmt.Errorf("omniroute MCP server %q: url and command are mutually exclusive", name)
		}
		client := &omniRouteMCPClient{serverName: name, serverURL: strings.TrimRight(url, "/"), headers: server.Headers, http: httpClient, cwd: cwd}
		if command != "" {
			client.command = append([]string{command}, server.Args...)
			client.env = server.Env
		}
		clients[name] = client
	}
	return clients, nil
}

func (c *omniRouteMCPClient) rpc(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	if len(c.command) > 0 {
		return c.rpcStdio(ctx, method, params)
	}
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
	responseBody, err := readOmniRouteMCPResponse(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", method, err)
	}
	var response omniRouteJSONRPCResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", method, err)
	}
	if err := validateOmniRouteMCPResponse(response, id, method); err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("%s RPC %d: %s", method, response.Error.Code, response.Error.Message)
	}
	return response.Result, nil
}

func readOmniRouteMCPResponse(body io.Reader, contentType string) ([]byte, error) {
	if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		return io.ReadAll(io.LimitReader(body, 10*1024*1024))
	}
	scanner := bufio.NewScanner(io.LimitReader(body, 10*1024*1024))
	scanner.Buffer(make([]byte, 4096), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data != "" {
			return []byte(data), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.ErrUnexpectedEOF
}

func (c *omniRouteMCPClient) rpcStdio(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.stdioMu.Lock()
	defer c.stdioMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.startStdioLocked(); err != nil {
		return nil, err
	}
	id := c.seq.Add(1)
	body, err := json.Marshal(omniRouteJSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", method, err)
	}
	if _, err := c.stdioIn.Write(append(body, '\n')); err != nil {
		c.stopStdioLocked()
		return nil, fmt.Errorf("write %s request: %w", method, err)
	}
	responseCh := make(chan struct {
		response omniRouteJSONRPCResponse
		err      error
	}, 1)
	decoder := c.stdioOut
	go func() {
		var response omniRouteJSONRPCResponse
		responseCh <- struct {
			response omniRouteJSONRPCResponse
			err      error
		}{response: response, err: decoder.Decode(&response)}
	}()
	select {
	case <-ctx.Done():
		c.stopStdioLocked()
		return nil, ctx.Err()
	case decoded := <-responseCh:
		if decoded.err != nil {
			c.stopStdioLocked()
			return nil, fmt.Errorf("decode %s response: %w", method, decoded.err)
		}
		if err := validateOmniRouteMCPResponse(decoded.response, id, method); err != nil {
			c.stopStdioLocked()
			return nil, err
		}
		if decoded.response.Error != nil {
			return nil, fmt.Errorf("%s RPC %d: %s", method, decoded.response.Error.Code, decoded.response.Error.Message)
		}
		return decoded.response.Result, nil
	}
}

func validateOmniRouteMCPResponse(response omniRouteJSONRPCResponse, id int64, method string) error {
	if response.JSONRPC != "2.0" {
		return fmt.Errorf("%s response used JSON-RPC version %q", method, response.JSONRPC)
	}
	var responseID int64
	if err := json.Unmarshal(response.ID, &responseID); err != nil || responseID != id {
		return fmt.Errorf("%s response id mismatch: got %s, want %d", method, strings.TrimSpace(string(response.ID)), id)
	}
	return nil
}

func (c *omniRouteMCPClient) startStdioLocked() error {
	if c.stdioCmd != nil {
		return nil
	}
	cmd := exec.Command(c.command[0], c.command[1:]...)
	cmd.Env = omniRouteChildEnv(c.env)
	if c.cwd != "" {
		cmd.Dir = c.cwd
	}
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdio stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdio stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start MCP server: %w", err)
	}
	c.stdioCmd = cmd
	c.stdioIn = stdin
	c.stdioOut = json.NewDecoder(stdout)
	return nil
}

func (c *omniRouteMCPClient) stopStdioLocked() {
	if c.stdioCmd == nil {
		return
	}
	_ = c.stdioIn.Close()
	_ = c.stdioCmd.Process.Kill()
	_ = c.stdioCmd.Wait()
	c.stdioCmd = nil
	c.stdioIn = nil
	c.stdioOut = nil
	c.initialized.Store(false)
	c.sessionID.Store("")
}

func (c *omniRouteMCPClient) close() {
	c.stdioMu.Lock()
	defer c.stdioMu.Unlock()
	c.stopStdioLocked()
}

func (c *omniRouteMCPClient) initialize(ctx context.Context) error {
	if c.initialized.Load() {
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
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		return err
	}
	c.initialized.Store(true)
	return nil
}

func (c *omniRouteMCPClient) notify(ctx context.Context, method string, params interface{}) error {
	body, err := json.Marshal(omniRouteJSONRPCRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encode %s notification: %w", method, err)
	}
	if len(c.command) > 0 {
		c.stdioMu.Lock()
		defer c.stdioMu.Unlock()
		if err := c.startStdioLocked(); err != nil {
			return err
		}
		if _, err := c.stdioIn.Write(append(body, '\n')); err != nil {
			c.stopStdioLocked()
			return fmt.Errorf("write %s notification: %w", method, err)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	if sessionID, ok := c.sessionID.Load().(string); ok && sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s request: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s HTTP %d: %s", method, resp.StatusCode, sanitizedHTTPError(resp.Body))
	}
	return nil
}

func omniRouteMCPToolName(serverName, toolName string) string {
	serverName = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, serverName)
	toolName = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, toolName)
	return "mcp__" + serverName + "__" + toolName
}

func omniRouteChildEnv(overrides map[string]string) []string {
	env := make([]string, 0)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if omniRouteChildEnvBlocked(key) {
				continue
			}
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		if omniRouteChildEnvBlocked(key) {
			continue
		}
		env = append(env, key+"="+value)
	}
	return env
}

func omniRouteChildEnvBlocked(key string) bool {
	switch strings.ToUpper(strings.TrimSpace(key)) {
	case "OMNIROUTE_API_KEY", "MULTICA_TOKEN", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GOOGLE_API_KEY", "GITHUB_TOKEN":
		return true
	default:
		return false
	}
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
