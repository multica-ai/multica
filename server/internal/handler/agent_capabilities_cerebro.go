package handler

// CEREBRO-PATCH(agent-capabilities-claim): local fallback capability policy wiring.

import (
	"bytes"
	"log/slog"

	cerebrocapabilities "github.com/multica-ai/multica/server/internal/cerebro/capabilities"
)

func applyCapabilityPolicyToClaim(resp *AgentTaskResponse, workspaceSettings []byte, runtimeID string) {
	if resp == nil || resp.Agent == nil {
		return
	}
	policy := cerebrocapabilities.ApplyClaimPolicy(
		workspaceSettings,
		runtimeID,
		resp.Agent.ID,
		resp.Agent.McpConfig,
	)
	if len(policy.SandboxAllowlist) > 0 {
		resp.Agent.SandboxAllowlist = policy.SandboxAllowlist
	}
	if policy.MCPConfig != nil && !bytes.Equal(policy.MCPConfig, resp.Agent.McpConfig) {
		resp.Agent.McpConfig = policy.MCPConfig
		slog.Info("agent capability policy filtered mcp_config", "agent_id", resp.Agent.ID, "runtime_id", runtimeID)
	}
}
