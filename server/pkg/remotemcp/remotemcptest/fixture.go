// Package remotemcptest provides a deterministic MCP server for isolated
// broker and Plugin lifecycle tests. It is deliberately not a production MCP
// endpoint: callers own the httptest server lifecycle and no network escapes
// the test process.
package remotemcptest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

const Credential = "fixture-token"

type Server struct {
	*httptest.Server

	mu     sync.Mutex
	writes []string
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func NewServer() *Server {
	fixture := &Server{}
	fixture.Server = httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	return fixture
}

func (fixture *Server) Writes() []string {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return append([]string(nil), fixture.writes...)
}

func (fixture *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer "+Credential {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "credential rejected"})
		return
	}
	var input request
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	result := map[string]any{}
	switch input.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "multica-remote-mcp-fixture", "version": "1.0.0"},
		}
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
		return
	case "tools/list":
		result = map[string]any{"tools": []any{
			map[string]any{
				"name": "fixture.read", "description": "Read a deterministic fixture value",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			},
			map[string]any{
				"name": "fixture.write", "description": "Append a value to the fixture log",
				"inputSchema": map[string]any{
					"type": "object", "required": []string{"value"},
					"properties": map[string]any{"value": map[string]any{"type": "string"}},
				},
			},
		}}
	case "tools/call":
		var params struct {
			Name      string `json:"name"`
			Arguments struct {
				Value string `json:"value"`
			} `json:"arguments"`
		}
		_ = json.Unmarshal(input.Params, &params)
		switch params.Name {
		case "fixture.read":
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "fixture-value"}}}
		case "fixture.write":
			fixture.mu.Lock()
			fixture.writes = append(fixture.writes, params.Arguments.Value)
			fixture.mu.Unlock()
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "written"}}}
		default:
			writeError(w, input.ID, -32602, "unknown tool")
			return
		}
	default:
		writeError(w, input.ID, -32601, "method not found")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": result})
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}
