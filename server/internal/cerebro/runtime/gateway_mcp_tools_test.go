package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// TestBuildGatewayMCPToolsRegistersNamespacedTools proves the resolver's
// out.MCPServers document is turned into callable, correctly-namespaced gateway
// tools whose Call dispatches to the live server.
func TestBuildGatewayMCPTools(t *testing.T) {
	fake := &fakeMCPServer{sessionID: "s1"}

	doc := map[string]any{
		"mcpServers": map[string]any{
			"infisical-admin": map[string]any{
				"type":    "http",
				"url":     fakeMCPURL,
				"headers": map[string]string{"Authorization": "Bearer x"},
			},
		},
	}
	raw, _ := json.Marshal(doc)

	tools := buildGatewayMCPTools(context.Background(), raw, inProcessClient(fake.handler()), nil)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name() != "mcp__infisical-admin__get_secret" {
		t.Fatalf("unexpected tool name: %q", tools[0].Name())
	}
	if tools[0].InputSchema()["type"] != "object" {
		t.Fatalf("expected object schema, got %+v", tools[0].InputSchema())
	}

	out, err := tools[0].Call(context.Background(), map[string]any{"key": "K"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out != "value-for-K" {
		t.Fatalf("unexpected call output: %q", out)
	}
}

// TestBuildGatewayMCPToolsEmpty covers the nil/empty document and unreachable
// server: no tools, no panic, task keeps running with its other tools.
func TestBuildGatewayMCPToolsEmpty(t *testing.T) {
	if got := buildGatewayMCPTools(context.Background(), nil, nil, nil); got != nil {
		t.Fatalf("expected nil for empty document, got %+v", got)
	}
	// A well-formed document whose server errors on discovery is skipped
	// (fail-open on availability), not fatal.
	dead := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	doc := `{"mcpServers":{"dead":{"type":"http","url":"` + fakeMCPURL + `"}}}`
	if got := buildGatewayMCPTools(context.Background(), json.RawMessage(doc), inProcessClient(dead), nil); got != nil {
		t.Fatalf("expected unreachable connection to be skipped, got %+v", got)
	}
}
