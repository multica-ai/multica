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
