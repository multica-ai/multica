package agentoffice

// FIR-3805 — the direct (non-proposal) write path for the per-binding
// "always on" flag.
//
// Two write paths exist for an agent's skills, and this mirrors the split that
// is already there for the binding set itself:
//
//   - The agent page proposes through the versioned change-request flow
//     (CreateChangeRequest carries always_on_skill_ids in the snapshot), so a
//     reviewer sees the flip before it applies.
//   - The CLI writes directly, exactly like `multica agent skills set` does
//     today, and the edit is captured into the version history afterwards by
//     RecordDirectEdit. Nothing is lost from the audit trail; the review simply
//     happens after the fact rather than before it.

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// SetAlwaysOnSkillsRequest replaces the agent's whole always-on set. An id that
// is not currently bound to the agent is rejected rather than ignored, so a
// typo surfaces instead of silently doing nothing.
type SetAlwaysOnSkillsRequest struct {
	AlwaysOnSkillIDs []string `json:"always_on_skill_ids"`
}

// AlwaysOnSkillsResponse echoes the set that is now flagged.
type AlwaysOnSkillsResponse struct {
	AgentID          string   `json:"agent_id"`
	AlwaysOnSkillIDs []string `json:"always_on_skill_ids"`
	ContextVersion   string   `json:"context_version"`
}

// SetAlwaysOnSkills handles PUT /api/agents/{id}/skills/always-on.
func (h *Handler) SetAlwaysOnSkills(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.loadAgent(w, r)
	if !ok {
		return
	}
	if !canManage(agent, member) {
		writeError(w, http.StatusForbidden, "not allowed to manage this agent")
		return
	}

	var req SetAlwaysOnSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ids := make([]pgtype.UUID, 0, len(req.AlwaysOnSkillIDs))
	for _, raw := range req.AlwaysOnSkillIDs {
		id, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid skill id: "+raw)
			return
		}
		ids = append(ids, id)
	}

	// Every id must already be bound to this agent. "Always on" is a property of
	// a binding, so flagging a skill the agent does not have is a request that
	// cannot be satisfied — better a 400 than a silent no-op.
	bindings, err := h.Svc.Cerebro.ListAgentSkillIDsForContext(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	bound := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		bound[util.UUIDToString(b.SkillID)] = true
	}
	for _, raw := range req.AlwaysOnSkillIDs {
		if !bound[raw] {
			writeError(w, http.StatusBadRequest, "skill is not bound to this agent: "+raw)
			return
		}
	}

	if err := h.Svc.Cerebro.SetAgentSkillAlwaysOn(r.Context(), cerebrodb.SetAgentSkillAlwaysOnParams{
		AgentID:          agent.ID,
		AlwaysOnSkillIds: ids,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update always-on skills")
		return
	}

	// Capture the direct edit into the version history. Best-effort by design —
	// the same contract the other direct-edit call sites use: the write has
	// already happened, and failing the request afterwards would be a lie.
	version := agent.ContextVersion
	if v, recorded, err := h.Svc.RecordDirectEdit(r.Context(), agent.ID, member.UserID, "Update always-on skills"); err == nil && recorded {
		version = v
	}

	writeJSON(w, http.StatusOK, AlwaysOnSkillsResponse{
		AgentID:          util.UUIDToString(agent.ID),
		AlwaysOnSkillIDs: req.AlwaysOnSkillIDs,
		ContextVersion:   version,
	})
}
