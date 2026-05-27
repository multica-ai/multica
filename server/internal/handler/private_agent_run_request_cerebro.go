package handler

// CEREBRO-PATCH(private-agent-run-request-cerebro): FIR-2385 — keeps the
// flag-gating helper for the visible-but-locked ListAgents patch in a
// cerebro-prefixed file so the upstream agent.go change stays a few marked lines.

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// cerebroPrivateAgentRequestsEnabled reports whether FIR-2385 is on for the
// viewing member. Default-on: a missing override row (or any lookup failure)
// resolves to the new behavior, matching the registry.ts default. Only an
// explicit per-user override of `false` restores the legacy hidden behavior.
func (h *Handler) cerebroPrivateAgentRequestsEnabled(ctx context.Context, workspaceID, userID string) bool {
	if h.CerebroQueries == nil {
		return true
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return true
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return true
	}
	on, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
		FlagKey:     "cerebro_private_agent_requests",
	})
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return true
	}
	return on
}

// redactLockedPrivateAgent strips everything but the public identity of a
// private agent surfaced (locked) to a non-owner member. "Visible" means name
// + description only — never the system prompt, runtime config, model, args,
// MCP config, env metadata, or skills, which together describe how the owner's
// personal agent works (FIR-2385).
func redactLockedPrivateAgent(resp *AgentResponse) {
	resp.Instructions = ""
	resp.RuntimeConfig = map[string]any{}
	resp.CustomArgs = nil
	resp.McpConfig = nil
	resp.McpConfigRedacted = true
	resp.HasCustomEnv = false
	resp.CustomEnvKeyCount = 0
	resp.Model = ""
	resp.ThinkingLevel = ""
	resp.PersonaSandbox = ""
	resp.InfisicalFolders = nil
	resp.Skills = nil
	resp.CanTrigger = false
}
