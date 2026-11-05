package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type SetSquadRuntimeRequest struct {
	RuntimeID string `json:"runtime_id"`
}

// SquadRuntimeAgentResult names one agent a squad-wide runtime change did not
// move, and why. The agent keeps its current runtime.
type SquadRuntimeAgentResult struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Reason    string `json:"reason"`
}

type SetSquadRuntimeResponse struct {
	RuntimeID string `json:"runtime_id"`
	// Agents now on the runtime because this request moved them.
	Updated []SquadRuntimeAgentResult `json:"updated"`
	// Agents already on the runtime; nothing was written for them.
	Unchanged []SquadRuntimeAgentResult `json:"unchanged"`
	// Agents this request could not move, each with the reason.
	Skipped []SquadRuntimeAgentResult `json:"skipped"`
}

// squadRuntimeRebindBlock reports why one agent cannot move onto runtime, or ""
// when it can. It applies the checks UpdateAgent runs when an agent's runtime
// changes, so a batch never leaves an agent with settings its new runtime
// rejects. decision is the target runtime's ACP thinking evidence, computed once
// per batch by the caller. clearModel is true when the agent's model belongs to a
// different provider family; UpdateAgent clears it the same way.
func squadRuntimeRebindBlock(a db.Agent, member db.Member, rt db.AgentRuntime, provider string, decision acpEffortEvidence) (reason string, clearModel bool) {
	if !roleAllowed(member.Role, "owner", "admin") && uuidToString(a.OwnerID) != uuidToString(member.UserID) {
		return "only the agent owner can manage this agent", false
	}
	if rt.Visibility == "private" && !service.RuntimeAllowsUser(rt, a.OwnerID) {
		return "the agent owner has no access to this runtime", false
	}
	if len(a.CustomArgs) > 0 {
		var customArgs []string
		if err := json.Unmarshal(a.CustomArgs, &customArgs); err != nil {
			return "existing custom_args could not be read", false
		}
		if err := agent.ValidateCustomArgs(provider, customArgs); err != nil {
			return err.Error(), false
		}
	}
	if a.ThinkingLevel.Valid && a.ThinkingLevel.String != "" {
		if !agent.IsKnownThinkingValue(provider, a.ThinkingLevel.String) {
			return existingThinkingLevelRejection(provider, a.ThinkingLevel.String), false
		}
		switch decision {
		case acpEffortAbsent:
			return existingThinkingCapabilityRejection(provider, a.ThinkingLevel.String), false
		case acpEffortUnknown:
			return existingThinkingCapabilityUnknownRejection(provider, a.ThinkingLevel.String), false
		}
	}
	if a.ServiceTier.Valid && a.ServiceTier.String != "" && !agent.IsKnownServiceTier(provider, a.ServiceTier.String) {
		return "existing service_tier is not valid for the new runtime", false
	}
	clearModel = a.Model.Valid && agent.ModelKnownIncompatibleWithProvider(provider, a.Model.String)
	return "", clearModel
}

// SetSquadRuntime points every live agent member of a squad at one runtime.
// Agents the request cannot move are reported with a reason and left alone;
// the rest change in a single transaction.
func (h *Handler) SetSquadRuntime(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageSquad(member, squad) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if squad.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "squad is archived")
		return
	}

	var req SetSquadRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: squad.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; ask its owner to grant you access before moving agents onto it")
		return
	}
	// Qoder agents carry a provider-specific config that UpdateAgent validates
	// field by field. Moving them in bulk would skip that validation.
	if runtime.Provider == "qoder_cloud" {
		writeError(w, http.StatusBadRequest, "qoder runtimes are set per agent, not for a whole squad")
		return
	}

	squadMembers, err := h.Queries.ListSquadMembers(r.Context(), squad.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list squad members")
		return
	}
	decision := h.acpThinkingDecision(r.Context(), runtime.Provider, runtime.ID)

	resp := SetSquadRuntimeResponse{
		RuntimeID: uuidToString(runtime.ID),
		Updated:   []SquadRuntimeAgentResult{},
		Unchanged: []SquadRuntimeAgentResult{},
		Skipped:   []SquadRuntimeAgentResult{},
	}
	type rebind struct {
		agent      db.Agent
		clearModel bool
	}
	var plan []rebind
	for _, sm := range squadMembers {
		if sm.MemberType != "agent" {
			continue
		}
		a, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          sm.MemberID,
			WorkspaceID: squad.WorkspaceID,
		})
		if err != nil || a.ArchivedAt.Valid {
			// Archived or vanished members have no runtime to move; the roster
			// already leaves them out.
			continue
		}
		result := SquadRuntimeAgentResult{AgentID: uuidToString(a.ID), AgentName: a.Name}
		if a.RuntimeID == runtime.ID {
			resp.Unchanged = append(resp.Unchanged, result)
			continue
		}
		reason, clearModel := squadRuntimeRebindBlock(a, member, runtime, runtime.Provider, decision)
		if reason != "" {
			result.Reason = reason
			resp.Skipped = append(resp.Skipped, result)
			continue
		}
		plan = append(plan, rebind{agent: a, clearModel: clearModel})
		resp.Updated = append(resp.Updated, result)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updatedAgents := make([]db.Agent, 0, len(plan))
	for _, p := range plan {
		params := db.UpdateAgentParams{
			ID:          p.agent.ID,
			RuntimeID:   runtime.ID,
			RuntimeMode: pgtype.Text{String: runtime.RuntimeMode, Valid: true},
		}
		if p.clearModel {
			params.Model = pgtype.Text{String: "", Valid: true}
		}
		updated, err := qtx.UpdateAgent(r.Context(), params)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update agent runtime")
			return
		}
		updatedAgents = append(updatedAgents, updated)
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	for _, updated := range updatedAgents {
		agentResp := h.agentToResponse(updated)
		if err := h.enrichAgentResponseWithTargets(r.Context(), &agentResp, updated.ID); err != nil {
			slog.Warn("squad runtime: load invocation targets failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(updated.ID))...)
			continue
		}
		if err := h.attachAgentSkills(r.Context(), &agentResp, updated.ID); err != nil {
			slog.Warn("squad runtime: load agent skills failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(updated.ID))...)
			continue
		}
		h.publish(protocol.EventAgentStatus, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(agentResp)})
	}
	h.publish(protocol.EventSquadUpdated, workspaceID, "member", userID, map[string]any{
		"squad_id": uuidToString(squad.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}
