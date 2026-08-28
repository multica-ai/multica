package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestNewOmniRouteMCPClientsRejectsLocalServer(t *testing.T) {
	_, err := newOmniRouteMCPClients(json.RawMessage(`{"mcpServers":{"local":{"command":"crm"}}}`), nil)
	if err == nil {
		t.Fatal("expected local server error")
	}
}
