package handler

// CEREBRO-PATCH(agent-capabilities-card-test): TECH-3642 unit tests for the
// capabilities-card limits parser.

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestBuildAgentCapabilityLimits_Empty(t *testing.T) {
	got := buildAgentCapabilityLimits(nil, nil)
	if got.HasMcpConfig {
		t.Fatalf("expected HasMcpConfig=false for empty config")
	}
	if got.Sandbox != nil {
		t.Fatalf("expected nil sandbox for empty runtime_config, got %s", got.Sandbox)
	}
	if len(got.McpServers) != 0 {
		t.Fatalf("expected no mcp servers, got %v", got.McpServers)
	}
}

func TestBuildAgentCapabilityLimits_SandboxAndMcp(t *testing.T) {
	runtimeConfig := []byte(`{"sandbox":{"network_allowlist":["api.anthropic.com:443"]},"other":1}`)
	mcpConfig := []byte(`{"mcpServers":{"multica":{"command":"x"},"bigquery":{"command":"y"}}}`)

	got := buildAgentCapabilityLimits(runtimeConfig, mcpConfig)

	if !got.HasMcpConfig {
		t.Fatalf("expected HasMcpConfig=true")
	}
	if len(got.Sandbox) == 0 {
		t.Fatalf("expected sandbox raw json to be populated")
	}
	var sb map[string]any
	if err := json.Unmarshal(got.Sandbox, &sb); err != nil {
		t.Fatalf("sandbox is not valid json: %v", err)
	}
	if _, ok := sb["network_allowlist"]; !ok {
		t.Fatalf("expected network_allowlist key in sandbox, got %v", sb)
	}

	sort.Strings(got.McpServers)
	want := []string{"bigquery", "multica"}
	if len(got.McpServers) != len(want) || got.McpServers[0] != want[0] || got.McpServers[1] != want[1] {
		t.Fatalf("expected mcp servers %v, got %v", want, got.McpServers)
	}
}

func TestBuildAgentCapabilityLimits_MalformedIsSafe(t *testing.T) {
	// A non-JSON blob must not panic and must yield an empty section.
	got := buildAgentCapabilityLimits([]byte("not json"), []byte("also not json"))
	if got.Sandbox != nil {
		t.Fatalf("expected nil sandbox for malformed runtime_config")
	}
	// mcp_config is present (non-empty) so HasMcpConfig is true even if unparseable.
	if !got.HasMcpConfig {
		t.Fatalf("expected HasMcpConfig=true when mcp_config bytes present")
	}
	if len(got.McpServers) != 0 {
		t.Fatalf("expected no mcp servers from malformed config, got %v", got.McpServers)
	}
}
