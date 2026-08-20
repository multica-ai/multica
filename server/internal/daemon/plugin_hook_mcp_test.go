package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The local MCP server is the daemon's whole contribution to the agent trigger.
// These cover the two things that decide whether it is safe: that a failing
// hook is a TOOL error rather than a broken transport, and that the tool list
// is exactly what the server said it was.

func testHookTools() []PluginHookTool {
	return []PluginHookTool{
		{InstallationID: "inst-1", HookKey: "summarize", Name: "triage_a1b2__summarize",
			Description: "Summarize the thread.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{InstallationID: "inst-2", HookKey: "summarize", Name: "release_c3d4__summarize",
			Description: "Summarize the release."},
	}
}

func startTestHookMCP(t *testing.T, invoke pluginHookInvoker) (*pluginHookMCPServer, string) {
	t.Helper()
	tools := testHookTools()
	byName := map[string]PluginHookTool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	return &pluginHookMCPServer{
		taskID: "task-1", tools: tools, byName: byName, invoke: invoke, path: "/token",
	}, "/token"
}

func callMCP(t *testing.T, server *pluginHookMCPServer, path, body string) map[string]any {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return decoded
}

// An unreachable plugin endpoint must not fail the agent's task. It comes back
// as a tool result flagged isError, which the agent reads and works around —
// not as a protocol error, which would look to the agent like its tooling is
// broken.
func TestPluginHookToolFailureIsAToolErrorNotATransportError(t *testing.T) {
	server, path := startTestHookMCP(t, func(context.Context, string, string, string, json.RawMessage) (json.RawMessage, error) {
		return nil, errors.New("hook endpoint did not answer")
	})

	response := callMCP(t, server, path,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"triage_a1b2__summarize","arguments":{}}}`)

	if _, isProtocolError := response["error"]; isProtocolError {
		t.Fatalf("a failing hook produced a JSON-RPC error, which reads as broken tooling: %v", response)
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", response)
	}
	if result["isError"] != true {
		t.Fatalf("the tool result must be flagged as an error, got %v", result)
	}
}

// Both plugins' hooks are offered, under their distinct names.
func TestPluginHookToolsListIsWhatTheServerSent(t *testing.T) {
	server, path := startTestHookMCP(t, nil)
	response := callMCP(t, server, path, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", response)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %v, want the two the server sent", result["tools"])
	}
	names := map[string]bool{}
	for _, entry := range tools {
		tool := entry.(map[string]any)
		names[tool["name"].(string)] = true
		// A tool with no declared input still needs a schema or providers
		// reject the whole list.
		if tool["inputSchema"] == nil {
			t.Fatalf("tool %v has no inputSchema", tool["name"])
		}
	}
	if !names["triage_a1b2__summarize"] || !names["release_c3d4__summarize"] {
		t.Fatalf("both plugins' hooks must appear under distinct names, got %v", names)
	}
}

// A name the server did not send is not callable, however plausible it looks.
func TestPluginHookToolRefusesAnUnknownTool(t *testing.T) {
	called := false
	server, path := startTestHookMCP(t, func(context.Context, string, string, string, json.RawMessage) (json.RawMessage, error) {
		called = true
		return nil, nil
	})
	response := callMCP(t, server, path,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"something__else","arguments":{}}}`)

	if _, isError := response["error"]; !isError {
		t.Fatalf("an unknown tool must be refused, got %v", response)
	}
	if called {
		t.Fatal("an unknown tool reached the invoker")
	}
}

// The path is a per-task random token, so knowing the port is not enough.
func TestPluginHookMCPRefusesTheWrongPath(t *testing.T) {
	server, _ := startTestHookMCP(t, nil)
	request, _ := http.NewRequest(http.MethodPost, "/guessed", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown path", recorder.Code)
	}
}
