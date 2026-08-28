package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	clients, err := newOmniRouteMCPClients(json.RawMessage(fmt.Sprintf(`{"mcpServers":{"crm":{"url":%q}}}`, server.URL)), server.Client())
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
	if len(calls) != 3 || calls[0] != "initialize" || calls[1] != "tools/list" || calls[2] != "tools/call" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestNewOmniRouteMCPClientsAcceptsStdioServer(t *testing.T) {
	clients, err := newOmniRouteMCPClients(json.RawMessage(`{"mcpServers":{"local":{"command":"crm"}}}`), nil)
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
    *initialize*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}' ;;
    *tools/list*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"lookup","inputSchema":{"type":"object"}}]}}' ;;
    *tools/call*) printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"stdio-result"}]}}' ;;
  esac
done`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	clients, err := newOmniRouteMCPClients(json.RawMessage(fmt.Sprintf(`{"mcpServers":{"local":{"command":%q}}}`, script)), nil)
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
