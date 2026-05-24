package capabilities

import (
	"encoding/json"
	"testing"
)

func TestApplyClaimPolicyUsesWorkspaceDefaultProfile(t *testing.T) {
	settings := []byte(`{
		"agent_capabilities": {
			"default_profile_id": "strict",
			"sandbox_profiles": [
				{"id":"strict","name":"Strict","network_allowlist":["api.anthropic.com:443","api.anthropic.com:443",""]}
			]
		}
	}`)

	got := ApplyClaimPolicy(settings, "runtime-1", "agent-1", nil)
	if got.ProfileID != "strict" {
		t.Fatalf("ProfileID = %q, want strict", got.ProfileID)
	}
	if len(got.SandboxAllowlist) != 1 || got.SandboxAllowlist[0] != "api.anthropic.com:443" {
		t.Fatalf("SandboxAllowlist = %#v", got.SandboxAllowlist)
	}
}

func TestApplyClaimPolicyRuntimeOverrideWins(t *testing.T) {
	settings := []byte(`{
		"agent_capabilities": {
			"default_profile_id": "standard",
			"runtime_profile_overrides": {"runtime-1":"strict"},
			"sandbox_profiles": [
				{"id":"standard","name":"Standard","network_allowlist":["github.com:443"]},
				{"id":"strict","name":"Strict","network_allowlist":["api.anthropic.com:443"]}
			]
		}
	}`)

	got := ApplyClaimPolicy(settings, "runtime-1", "agent-1", nil)
	if got.ProfileID != "strict" {
		t.Fatalf("ProfileID = %q, want strict", got.ProfileID)
	}
	if got.SandboxAllowlist[0] != "api.anthropic.com:443" {
		t.Fatalf("SandboxAllowlist = %#v", got.SandboxAllowlist)
	}
}

func TestApplyClaimPolicyRemovesDeniedMCPServers(t *testing.T) {
	settings := []byte(`{
		"agent_capabilities": {
			"agent_permissions": {
				"agent-1": {"mcp_denied_servers":["github"]}
			}
		}
	}`)
	raw := json.RawMessage(`{"mcpServers":{"github":{"command":"gh"},"browser":{"command":"npx"}}}`)

	got := ApplyClaimPolicy(settings, "runtime-1", "agent-1", raw)
	var decoded struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(got.MCPConfig, &decoded); err != nil {
		t.Fatalf("unmarshal filtered mcp_config: %v", err)
	}
	if _, ok := decoded.MCPServers["github"]; ok {
		t.Fatalf("github server was not removed: %s", got.MCPConfig)
	}
	if _, ok := decoded.MCPServers["browser"]; !ok {
		t.Fatalf("browser server should remain: %s", got.MCPConfig)
	}
}

func TestApplyClaimPolicyEmptyConfigPreservesMCP(t *testing.T) {
	raw := json.RawMessage(`{"mcpServers":{"github":{"command":"gh"}}}`)
	got := ApplyClaimPolicy(nil, "runtime-1", "agent-1", raw)
	if string(got.MCPConfig) != string(raw) {
		t.Fatalf("MCPConfig = %s, want %s", got.MCPConfig, raw)
	}
}
