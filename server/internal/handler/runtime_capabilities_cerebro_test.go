package handler

// CEREBRO-PATCH(runtime-capability-normalize-test): regression coverage for stale live capability snapshots.

import (
	"encoding/json"
	"testing"
)

func TestNormalizedRuntimeCapabilities_BackfillsStaleCodexSnapshot(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"providers":   []string{"codex"},
		"tools":       []string{},
		"mcp_servers": []string{},
	})
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}

	caps := normalizedRuntimeCapabilities("codex", raw, nil)
	tools := anyStringSlice(caps["tools"])
	if !containsString(tools, "bash") || !containsString(tools, "apply_patch") || !containsString(tools, "view") {
		t.Fatalf("codex tools were not backfilled from static registry: %v", tools)
	}
	secrets := anyStringSlice(caps["secret_bindings"])
	if !containsString(secrets, "OPENAI_API_KEY") {
		t.Fatalf("codex secret bindings were not backfilled: %v", secrets)
	}
	hooks := anyStringSlice(caps["hooks"])
	if !containsString(hooks, "OnTaskStart") || !containsString(hooks, "OnTaskEnd") {
		t.Fatalf("codex hooks were not backfilled: %v", hooks)
	}
	if got, want := anyStringSlice(caps["tool_protocols"]), []string{"mcp_stdio"}; !equalStringSlices(got, want) {
		t.Fatalf("tool_protocols: got %v, want %v", got, want)
	}
	if got := caps["supports_ask"]; got != false {
		t.Fatalf("supports_ask: got %v, want false", got)
	}
}

func TestNormalizedRuntimeCapabilities_AddsRuntimeMCPServers(t *testing.T) {
	toolsConfig := []byte(`{"mcpServers":{"browser":{"command":"npx"},"multica":{"command":"multica"}}}`)

	caps := normalizedRuntimeCapabilities("claude", nil, toolsConfig)
	servers := anyStringSlice(caps["mcp_servers"])
	if got, want := servers, []string{"browser", "multica"}; !equalStringSlices(got, want) {
		t.Fatalf("mcp_servers: got %v, want %v", got, want)
	}
	tools := anyStringSlice(caps["tools"])
	if !containsString(tools, "Bash") {
		t.Fatalf("claude tools were not preserved while adding MCP servers: %v", tools)
	}
}

// CEREBRO-PATCH(runtime-agnostic-tool-access): reported transport protocols must beat provider fallbacks.
func TestNormalizedRuntimeCapabilities_PreservesReportedToolProtocol(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"providers":      []string{"larry"},
		"tool_protocols": []string{"managed_http_tool_loop"},
		"supports_ask":   true,
	})
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}

	caps := normalizedRuntimeCapabilities("larry", raw, nil)
	if got, want := anyStringSlice(caps["tool_protocols"]), []string{"managed_http_tool_loop"}; !equalStringSlices(got, want) {
		t.Fatalf("tool_protocols: got %v, want %v", got, want)
	}
	if got := caps["supports_ask"]; got != true {
		t.Fatalf("supports_ask: got %v, want true", got)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
