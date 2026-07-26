package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// agentCrossWorkspaceGrantsActivityUpdated is the activity_log `action`
// constant for grant changes (BUS-171). Rows are written with
// `issue_id IS NULL`, same convention as the agent env audit trail.
const agentCrossWorkspaceGrantsActivityUpdated = "agent_cross_workspace_grants_updated"

// AgentCrossWorkspaceGrantsResponse is the wire shape for
// GET/PUT /api/agents/{id}/cross-workspace-grants.
type AgentCrossWorkspaceGrantsResponse struct {
	AgentID           string   `json:"agent_id"`
	CrossWorkspaceIDs []string `json:"cross_workspace_ids"`
}

// UpdateAgentCrossWorkspaceGrantsRequest is the wire shape for the PUT.
// Replaces the allow-list wholesale, same contract as UpdateAgentEnv.
type UpdateAgentCrossWorkspaceGrantsRequest struct {
	CrossWorkspaceIDs []string `json:"cross_workspace_ids"`
}

// authorizeAgentCrossWorkspaceGrants enforces the per-request auth
// contract for the grant-management endpoints: the caller must be a
// human workspace owner/admin. Machine-credential rejection is handled
// by the RequireHumanActor middleware at the route level (see
// router.go) rather than here, since that gate has no agent-specific
// context to add — this function only needs the human-role check.
func (h *Handler) authorizeAgentCrossWorkspaceGrants(w http.ResponseWriter, r *http.Request) (db.Agent, db.Member, bool) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return db.Agent{}, db.Member{}, false
	}

	workspaceID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "agent not found", "owner", "admin")
	if !ok {
		return db.Agent{}, db.Member{}, false
	}

	return agent, member, true
}

// GetAgentCrossWorkspaceGrants returns an agent's cross_workspace_ids
// allow-list. These are workspace UUIDs, not secrets, so unlike
// GetAgentEnv this read is not itself audit-logged — only the grants
// endpoint's writes are, since that's the security-relevant action
// (widening what a running task can reach).
func (h *Handler) GetAgentCrossWorkspaceGrants(w http.ResponseWriter, r *http.Request) {
	agent, _, ok := h.authorizeAgentCrossWorkspaceGrants(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, AgentCrossWorkspaceGrantsResponse{
		AgentID:           uuidToString(agent.ID),
		CrossWorkspaceIDs: uuidStringsOrEmpty(agent.CrossWorkspaceIds),
	})
}

// UpdateAgentCrossWorkspaceGrants replaces an agent's cross_workspace_ids
// wholesale. This is the sole write path for the column — mirrors why
// UpdateAgentCustomEnv is the sole write path for custom_env: a
// dedicated, owner/admin-only, audited endpoint so a privilege widening
// (an agent gaining the ability to mint a token into another workspace)
// can never happen without a queryable trail.
//
// Persist + audit run inside one DB transaction, same fail-closed
// reasoning as UpdateAgentEnv: an audit-write outage cannot leave an
// unaudited grant change on disk.
func (h *Handler) UpdateAgentCrossWorkspaceGrants(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.authorizeAgentCrossWorkspaceGrants(w, r)
	if !ok {
		return
	}

	var req UpdateAgentCrossWorkspaceGrantsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	parsed, ok := parseUUIDSliceOrBadRequest(w, req.CrossWorkspaceIDs, "cross_workspace_ids")
	if !ok {
		return
	}

	ownWorkspaceID := uuidToString(agent.WorkspaceID)
	seen := make(map[string]bool, len(parsed))
	grantIDs := make([]pgtype.UUID, 0, len(parsed))
	for _, id := range parsed {
		s := uuidToString(id)
		if s == ownWorkspaceID {
			writeError(w, http.StatusBadRequest, "cross_workspace_ids may not include the agent's own workspace")
			return
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		grantIDs = append(grantIDs, id)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("agent_cross_workspace_grants update: begin tx failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update cross-workspace grants")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.UpdateAgentCrossWorkspaceIDs(r.Context(), db.UpdateAgentCrossWorkspaceIDsParams{
		ID:                agent.ID,
		CrossWorkspaceIds: grantIDs,
	})
	if err != nil {
		slog.Warn("update agent cross_workspace_ids failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update cross-workspace grants")
		return
	}

	details, _ := json.Marshal(map[string]any{
		"agent_id":            uuidToString(agent.ID),
		"agent_name":          agent.Name,
		"cross_workspace_ids": uuidStringsOrEmpty(grantIDs),
	})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: agent.WorkspaceID,
		IssueID:     pgtype.UUID{}, // grant changes are not tied to an issue
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(uuidToString(member.UserID)),
		Action:      agentCrossWorkspaceGrantsActivityUpdated,
		Details:     details,
	}); err != nil {
		slog.Error("agent_cross_workspace_grants_updated audit write failed; rolling back update",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; cross-workspace grants update rolled back")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("agent_cross_workspace_grants update: tx commit failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update cross-workspace grants")
		return
	}

	writeJSON(w, http.StatusOK, AgentCrossWorkspaceGrantsResponse{
		AgentID:           uuidToString(updated.ID),
		CrossWorkspaceIDs: uuidStringsOrEmpty(updated.CrossWorkspaceIds),
	})
}
