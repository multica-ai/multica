package capabilities

import (
	"encoding/json"
	"sort"
	"strings"
)

const SettingsKey = "agent_capabilities"

type WorkspaceSettings struct {
	AgentCapabilities *Config `json:"agent_capabilities,omitempty"`
}

type Config struct {
	DefaultProfileID       string                      `json:"default_profile_id,omitempty"`
	SandboxProfiles        []SandboxProfile            `json:"sandbox_profiles,omitempty"`
	RuntimeProfileOverride map[string]string           `json:"runtime_profile_overrides,omitempty"`
	AgentPermissions       map[string]AgentPermissions `json:"agent_permissions,omitempty"`
}

type SandboxProfile struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	NetworkAllowlist []string `json:"network_allowlist,omitempty"`
}

type AgentPermissions struct {
	MCPDeniedServers []string `json:"mcp_denied_servers,omitempty"`
}

type ClaimPolicy struct {
	SandboxAllowlist []string
	MCPConfig        json.RawMessage
	ProfileID        string
}

func ApplyClaimPolicy(settingsJSON []byte, runtimeID string, agentID string, mcpConfig json.RawMessage) ClaimPolicy {
	cfg := parseConfig(settingsJSON)
	policy := ClaimPolicy{MCPConfig: cloneRaw(mcpConfig)}
	if cfg == nil {
		return policy
	}

	profileID := strings.TrimSpace(cfg.DefaultProfileID)
	if cfg.RuntimeProfileOverride != nil {
		if override := strings.TrimSpace(cfg.RuntimeProfileOverride[runtimeID]); override != "" {
			profileID = override
		}
	}
	if profileID != "" {
		for _, profile := range cfg.SandboxProfiles {
			if profile.ID == profileID {
				policy.ProfileID = profile.ID
				policy.SandboxAllowlist = normalizeStrings(profile.NetworkAllowlist)
				break
			}
		}
	}

	if cfg.AgentPermissions != nil {
		perm := cfg.AgentPermissions[agentID]
		policy.MCPConfig = denyMCPServers(policy.MCPConfig, perm.MCPDeniedServers)
	}
	return policy
}

func parseConfig(settingsJSON []byte) *Config {
	if len(settingsJSON) == 0 {
		return nil
	}
	var settings WorkspaceSettings
	if err := json.Unmarshal(settingsJSON, &settings); err != nil {
		return nil
	}
	return settings.AgentCapabilities
}

func denyMCPServers(raw json.RawMessage, denied []string) json.RawMessage {
	deniedSet := make(map[string]struct{}, len(denied))
	for _, name := range denied {
		name = strings.TrimSpace(name)
		if name != "" {
			deniedSet[name] = struct{}{}
		}
	}
	if len(deniedSet) == 0 || len(raw) == 0 {
		return cloneRaw(raw)
	}

	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cloneRaw(raw)
	}
	var servers map[string]json.RawMessage
	if rawServers, ok := cfg["mcpServers"]; ok {
		_ = json.Unmarshal(rawServers, &servers)
	}
	if len(servers) == 0 {
		return cloneRaw(raw)
	}
	for name := range deniedSet {
		delete(servers, name)
	}
	nextServers, err := json.Marshal(servers)
	if err != nil {
		return cloneRaw(raw)
	}
	cfg["mcpServers"] = nextServers
	out, err := json.Marshal(cfg)
	if err != nil {
		return cloneRaw(raw)
	}
	return out
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}
