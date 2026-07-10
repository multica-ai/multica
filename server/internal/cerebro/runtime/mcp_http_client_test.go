package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// inProcessClient wraps a handler in an *http.Client that dispatches every
// request in-process via loopbackTransport, so the MCP client tests need no
// listening socket (the sandbox forbids binding one).
func inProcessClient(h http.Handler) *http.Client {
	return &http.Client{Transport: loopbackTransport{handler: h}}
}

// fakeMCPURL is any well-formed URL; loopbackTransport ignores the host and
// dispatches to the handler directly.
const fakeMCPURL = "http://mcp.local/mcp"

// fakeMCPServer is a minimal streamable-HTTP MCP server for the client tests. It
// answers initialize/tools/list/tools/call and can reply in either plain JSON or
// SSE, mirroring the two response modes the real transport uses.
type fakeMCPServer struct {
	sse        bool
	sessionID  string
	lastHeader http.Header
	// callArgs records the arguments of the last tools/call.
	callArgs map[string]any
}

func (f *fakeMCPServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.lastHeader = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)

		// Notifications carry no id — just 202 them.
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var result any
		switch req.Method {
		case "initialize":
			if f.sessionID != "" {
				w.Header().Set("Mcp-Session-Id", f.sessionID)
			}
			result = map[string]any{
				"protocolVersion": mcpClientProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "fake", "version": "1"},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []map[string]any{
					{
						"name":        "get_secret",
						"description": "fetch a secret",
						"inputSchema": map[string]any{
							"type":       "object",
							"properties": map[string]any{"key": map[string]any{"type": "string"}},
						},
					},
				},
			}
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			f.callArgs = p.Arguments
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "value-for-" + fmt.Sprint(p.Arguments["key"])}},
			}
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
			return
		}

		env := map[string]any{"jsonrpc": "2.0", "id": jsonRawToAny(req.ID), "result": result}
		if f.sse {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			payload, _ := json.Marshal(env)
			// Interleave an unrelated notification frame first to prove the client
			// skips frames whose id does not match.
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(env)
	}
}

// jsonRawToAny decodes a raw JSON id into a comparable value so the response
// echoes the request id in the same numeric form.
func jsonRawToAny(raw json.RawMessage) any {
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

func TestMCPHTTPClientListAndCall_JSON(t *testing.T) {
	runClientRoundtrip(t, false)
}

func TestMCPHTTPClientListAndCall_SSE(t *testing.T) {
	runClientRoundtrip(t, true)
}

func runClientRoundtrip(t *testing.T, sse bool) {
	t.Helper()
	fake := &fakeMCPServer{sse: sse, sessionID: "sess-123"}
	c := newMCPHTTPClient(fakeMCPURL, map[string]string{"Authorization": "Bearer real-token"}, inProcessClient(fake.handler()))

	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "get_secret" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	if tools[0].InputSchema["type"] != "object" {
		t.Fatalf("expected object input schema, got %+v", tools[0].InputSchema)
	}

	res, err := c.CallTool(context.Background(), "get_secret", map[string]any{"key": "DB_PASSWORD"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := mcpResultText(res); got != "value-for-DB_PASSWORD" {
		t.Fatalf("unexpected call result: %q", got)
	}

	// The connection's real auth header must reach the server, and the negotiated
	// session id must ride on the follow-up calls.
	if got := fake.lastHeader.Get("Authorization"); got != "Bearer real-token" {
		t.Fatalf("auth header not forwarded: %q", got)
	}
	if got := fake.lastHeader.Get("Mcp-Session-Id"); got != "sess-123" {
		t.Fatalf("session id not echoed on follow-up call: %q", got)
	}
}

func TestMCPHTTPClientToolError(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		if req.Method == "tools/call" {
			result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "boom"}},
				"isError": true,
			}
		} else {
			result = map[string]any{"tools": []any{}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": jsonRawToAny(req.ID), "result": result})
	})

	c := newMCPHTTPClient(fakeMCPURL, nil, inProcessClient(h))
	res, err := c.CallTool(context.Background(), "explode", nil)
	if err != nil {
		t.Fatalf("transport error unexpected: %v", err)
	}
	if !res.IsError || !strings.Contains(mcpResultText(res), "boom") {
		t.Fatalf("expected tool-level error result, got %+v", res)
	}
}

func TestMCPHTTPClientRPCError(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.Method == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": jsonRawToAny(req.ID), "result": map[string]any{}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": jsonRawToAny(req.ID),
			"error": map[string]any{"code": -32601, "message": "method not found"},
		})
	})

	c := newMCPHTTPClient(fakeMCPURL, nil, inProcessClient(h))
	if _, err := c.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("expected rpc error surfaced, got %v", err)
	}
}

// TestMCPHTTPClientRefusesRedirect verifies the default client fails closed on a
// redirect instead of re-sending the connection's auth headers (X-API-Key /
// CF-Access-*) to whatever host the upstream points us at. A streamable-HTTP MCP
// endpoint never legitimately redirects, so following one is only ever a leak.
func TestMCPHTTPClientRefusesRedirect(t *testing.T) {
	c := newMCPHTTPClient(fakeMCPURL, map[string]string{"X-API-Key": "secret"}, nil)
	if c.http.CheckRedirect == nil {
		t.Fatal("default mcp http client must set CheckRedirect to refuse redirects")
	}
	req, err := http.NewRequest(http.MethodPost, "https://attacker.example/steal", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if err := c.http.CheckRedirect(req, nil); err == nil {
		t.Fatal("CheckRedirect must return an error to stop the redirect, got nil")
	}
}
