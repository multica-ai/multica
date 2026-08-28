package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOmniRouteMCPClientListsAndCallsTools(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req omniRouteJSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		calls = append(calls, req.Method)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "session-1")
		}
		result := map[string]interface{}{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{"protocolVersion": "2025-03-26", "capabilities": map[string]interface{}{}}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = map[string]interface{}{"tools": []interface{}{map[string]interface{}{
				"name": "lookup", "description": "Look up a record", "inputSchema": map[string]interface{}{"type": "object"},
			}}}
		case "tools/call":
			result = map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "text", "text": "found"}}}
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()

	clients, err := newOmniRouteMCPClients(json.RawMessage(fmt.Sprintf(`{"mcpServers":{"crm":{"url":%q}}}`, server.URL)), server.Client(), "")
	if err != nil {
		t.Fatalf("new clients: %v", err)
	}
	tools, err := clients["crm"].ListTools(context.Background())
	if err != nil || len(tools) != 1 || tools[0].Function.Name != "lookup" {
		t.Fatalf("tools=%#v err=%v", tools, err)
	}
	output, err := clients["crm"].CallTool(context.Background(), "lookup", map[string]interface{}{"id": "1"})
	if err != nil || output != "found" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if len(calls) != 4 || calls[0] != "initialize" || calls[1] != "notifications/initialized" || calls[2] != "tools/list" || calls[3] != "tools/call" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestOmniRouteMCPClientRejectsMismatchedResponseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": 999, "result": map[string]interface{}{}})
	}))
	defer server.Close()

	clients, err := newOmniRouteMCPClients(json.RawMessage(fmt.Sprintf(`{"mcpServers":{"bad":{"url":%q}}}`, server.URL)), server.Client(), "")
	if err != nil {
		t.Fatalf("new clients: %v", err)
	}
	if _, err := clients["bad"].ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "id mismatch") {
		t.Fatalf("expected response id mismatch, got %v", err)
	}
}

func TestNewOmniRouteMCPClientsAcceptsStdioServer(t *testing.T) {
	clients, err := newOmniRouteMCPClients(json.RawMessage(`{"mcpServers":{"local":{"command":"crm"}}}`), nil, "")
	if err != nil || len(clients) != 1 || len(clients["local"].command) != 1 {
		t.Fatalf("clients=%#v err=%v", clients, err)
	}
}

func TestOmniRouteMCPClientSupportsStdio(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "mcp.sh")
	content := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *notifications/initialized*) : ;;
    *initialize*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}' ;;
    *tools/list*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"lookup","inputSchema":{"type":"object"}}]}}' ;;
    *tools/call*) printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"stdio-result"}]}}' ;;
  esac
done`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	clients, err := newOmniRouteMCPClients(json.RawMessage(fmt.Sprintf(`{"mcpServers":{"local":{"command":%q}}}`, script)), nil, "")
	if err != nil {
		t.Fatalf("new clients: %v", err)
	}
	defer clients["local"].close()
	tools, err := clients["local"].ListTools(context.Background())
	if err != nil || len(tools) != 1 || tools[0].Function.Name != "lookup" {
		t.Fatalf("tools=%#v err=%v", tools, err)
	}
	output, err := clients["local"].CallTool(context.Background(), "lookup", map[string]interface{}{"id": "1"})
	if err != nil || output != "stdio-result" {
		t.Fatalf("output=%q err=%v", output, err)
	}
}

func TestOmniRouteMCPStdioUsesTaskCwdAndDoesNotInheritProviderSecrets(t *testing.T) {
	root := t.TempDir()
	report := filepath.Join(root, "child-report")
	t.Setenv("OMNIROUTE_API_KEY", "host-secret")
	t.Setenv("MULTICA_TOKEN", "task-secret")
	script := filepath.Join(root, "mcp.sh")
	content := `#!/bin/sh
printf '%s|%s|%s\n' "$PWD" "${OMNIROUTE_API_KEY:-missing}" "${MULTICA_TOKEN:-missing}" > "$PROBE_REPORT"
while IFS= read -r line; do
  case "$line" in
    *'"method":"notifications/initialized"'*) : ;;
    *'"method":"initialize"'*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}' ;;
    *'"method":"tools/list"'*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}' ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	raw := json.RawMessage(fmt.Sprintf(`{"mcpServers":{"local":{"command":%q,"env":{"PROBE_REPORT":%q,"OMNIROUTE_API_KEY":"config-secret","MULTICA_TOKEN":"config-token"}}}}`, script, report))
	clients, err := newOmniRouteMCPClients(raw, nil, root)
	if err != nil {
		t.Fatalf("new clients: %v", err)
	}
	defer clients["local"].close()
	if _, err := clients["local"].ListTools(context.Background()); err != nil {
		t.Fatalf("list tools: %v", err)
	}
	recorded, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read child report: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(string(recorded)), "|")
	recordedCwd, err := filepath.EvalSymlinks(parts[0])
	if err != nil {
		t.Fatalf("resolve child cwd %q: %v", parts[0], err)
	}
	expectedCwd, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve expected cwd: %v", err)
	}
	if len(parts) != 3 || recordedCwd != expectedCwd || parts[1] != "missing" || parts[2] != "missing" {
		t.Fatalf("child environment report = %q", recorded)
	}
}
