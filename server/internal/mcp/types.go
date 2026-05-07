package mcp

// CEREBRO-PATCH(mcp-types): cerebro modification of upstream file

import "encoding/json"

// JSON-RPC 2.0 types for the Model Context Protocol (MCP).

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool describes an MCP tool exposed to the client.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// CallToolParams is the params for tools/call.
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResult is the result of a tool invocation.
type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content is a single content block in a tool result.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// TextResult creates a CallToolResult with a single text content block.
func TextResult(text string) CallToolResult {
	return CallToolResult{
		Content: []Content{{Type: "text", Text: text}},
	}
}

// ErrorResult creates a CallToolResult indicating an error.
func ErrorResult(msg string) CallToolResult {
	return CallToolResult{
		Content: []Content{{Type: "text", Text: msg}},
		IsError: true,
	}
}

// InitializeResult is the response to the initialize method.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

// Capabilities describes what the server supports.
type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

// ToolsCapability declares tool support.
type ToolsCapability struct{}

// ServerInfo identifies the server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolsListResult is the response to tools/list.
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}
