// CEREBRO-PATCH(cerebro-agentvault-claim-file): Agent Vault claim hook (TECH-3196).
//
// Kept in a cerebro-prefixed file so the upstream daemon.go claim path stays to
// a single marked gate line. At task claim, if the workspace has the
// cerebro_agent_vault flag on AND the agent has rows in the per-agent access
// table, the broker mints a vault-scoped token and returns the proxy env that
// routes the agent through the Multica relay. Merged into the agent's
// custom_env (which the daemon injects and redacts from agent-actor reads).
//
// Fail-open: any error spawns the agent WITHOUT broker env rather than blocking
// the claim — the agent simply lacks the brokered access; no secret is exposed.
package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
	"github.com/multica-ai/multica/server/internal/logger"
)

// AgentVaultBroker is the seam for claim-time per-agent secret brokering. The
// cerebro agentvault.Service satisfies it. Declared here so handler.go can hold
// the field without importing the agentvault package.
type AgentVaultBroker interface {
	PrepareSpawnEnv(ctx context.Context, workspaceID, agentID, agentName string) (agentvault.SpawnEnv, error)
}

// cerebroAgentVaultEnabled reports whether the workspace flag cerebro_agent_vault
// is on. Default OFF: a missing row or any lookup failure resolves to false, so
// the broker ships dormant and a deploy never changes behavior.
func (h *Handler) cerebroAgentVaultEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if h.CerebroQueries == nil {
		return false
	}
	rows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.FlagKey == "cerebro_agent_vault" {
			return row.Enabled
		}
	}
	return false
}

// mergeAgentVaultEnvForClaim merges per-agent Agent Vault proxy env into the
// claim's custom_env. No-op when the broker is unwired, the flag is off, or the
// agent has no granted vaults. Fail-open on error.
func (h *Handler) mergeAgentVaultEnvForClaim(r *http.Request, agentID, workspaceID pgtype.UUID, agentName string, customEnv map[string]string) map[string]string {
	if h.AgentVaultBroker == nil {
		return customEnv
	}
	if !h.cerebroAgentVaultEnabled(r.Context(), workspaceID) {
		return customEnv
	}
	env, err := h.AgentVaultBroker.PrepareSpawnEnv(r.Context(), uuidToString(workspaceID), uuidToString(agentID), agentName)
	if err != nil {
		slog.Warn("agentvault: claim env prep failed; spawning without broker env",
			append(logger.RequestAttrs(r), "agent_id", uuidToString(agentID), "error", err)...)
		return customEnv
	}
	if len(env) == 0 {
		return customEnv
	}
	if customEnv == nil {
		customEnv = make(map[string]string, len(env))
	}
	for k, v := range env {
		customEnv[k] = v
	}
	return customEnv
}
