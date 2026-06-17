package handler

// CEREBRO-PATCH(agent-capabilities-card-test): TECH-3642 unit tests for the
// capabilities-card limits parser.

import (
	"encoding/json"
	"sort"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

// CEREBRO-PATCH(agent-capabilities-secret-set-test): TECH-3738 Bid A — the
// agent custom_env secret set is names-only and redacts for non-privileged
// callers; runtime secret status distinguishes "nothing" from "unknown".

func TestBuildAgentSecretSet_RevealVsRedacted(t *testing.T) {
	agent := db.Agent{CustomEnv: []byte(`{"OPENAI_API_KEY":"x","SLIPLANE_KEY":"y"}`)}

	// Owner/admin caller: names revealed, sorted, not redacted.
	revealed := buildAgentSecretSet(agent, true)
	if revealed.Status != capStatusKnown {
		t.Fatalf("expected status=known, got %q", revealed.Status)
	}
	if revealed.Count != 2 || revealed.Redacted {
		t.Fatalf("expected count=2 not redacted, got count=%d redacted=%v", revealed.Count, revealed.Redacted)
	}
	if len(revealed.Names) != 2 || revealed.Names[0] != "OPENAI_API_KEY" || revealed.Names[1] != "SLIPLANE_KEY" {
		t.Fatalf("expected sorted names, got %v", revealed.Names)
	}

	// Non-privileged caller: count preserved, names withheld, redacted=true.
	redacted := buildAgentSecretSet(agent, false)
	if redacted.Count != 2 || !redacted.Redacted {
		t.Fatalf("expected count=2 redacted=true, got count=%d redacted=%v", redacted.Count, redacted.Redacted)
	}
	if len(redacted.Names) != 0 {
		t.Fatalf("expected no names leaked to non-privileged caller, got %v", redacted.Names)
	}
}

func TestBuildAgentSecretSet_EmptyIsNotConfigured(t *testing.T) {
	got := buildAgentSecretSet(db.Agent{CustomEnv: []byte(`{}`)}, true)
	if got.Status != capStatusNotConfigured {
		t.Fatalf("expected status=not_configured for empty custom_env, got %q", got.Status)
	}
	if got.Count != 0 || got.Names == nil {
		t.Fatalf("expected count=0 and non-nil names slice, got count=%d names=%v", got.Count, got.Names)
	}
}

func TestRuntimeSecretSetFromCaps_Status(t *testing.T) {
	// Known: bindings present, normal discovery method.
	known := runtimeSecretSetFromCaps(map[string]any{
		"secret_bindings":  []string{"ANTHROPIC_API_KEY"},
		"discovery_method": "static",
	}, "rt-1", true)
	if known.Status != capStatusKnown || known.Count != 1 || len(known.Names) != 1 {
		t.Fatalf("expected known/1/[name], got %+v", known)
	}
	if known.RuntimeID != "rt-1" {
		t.Fatalf("expected runtime_id propagated, got %q", known.RuntimeID)
	}

	// Not configured: empty bindings but the runtime WAS mapped.
	none := runtimeSecretSetFromCaps(map[string]any{
		"secret_bindings":  []string{},
		"discovery_method": "static",
	}, "rt-2", true)
	if none.Status != capStatusNotConfigured {
		t.Fatalf("expected not_configured for mapped-but-empty, got %q", none.Status)
	}

	// Unknown: an unmapped runtime — absence of bindings is NOT confirmed-empty.
	unknown := runtimeSecretSetFromCaps(map[string]any{
		"discovery_method": "unmapped",
	}, "rt-3", true)
	if unknown.Status != capStatusUnknown {
		t.Fatalf("expected unknown for unmapped runtime, got %q", unknown.Status)
	}

	// Redaction applies to runtime bindings too.
	redacted := runtimeSecretSetFromCaps(map[string]any{
		"secret_bindings":  []string{"ANTHROPIC_API_KEY"},
		"discovery_method": "static",
	}, "rt-4", false)
	if !redacted.Redacted || len(redacted.Names) != 0 || redacted.Count != 1 {
		t.Fatalf("expected redacted with count=1 and no names, got %+v", redacted)
	}
}
