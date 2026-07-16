package handler

// CEREBRO-PATCH(agent-capabilities-swap-handler): FIR-3212 Swap slice — a
// before/after preview of moving an agent to a different runtime.
//
// GET /api/agents/{id}/capabilities/swap?runtime_id={target} answers the
// question the Setup screen cannot answer from the capabilities card alone:
// this agent runs on engine A; if I move it to engine B, which of its settings
// stop working, and which of those stop working silently?
//
// The card at /capabilities describes ONE engine — the one the agent already
// runs on. Diffing two of those in the browser would mean teaching the frontend
// the handling vocabulary (silent vs logged drop, prepend vs native prompt),
// which is the knowledge that has to stay next to the matrix it is tested
// against. So the diff is computed here and rendered there.

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	cerebrocapabilities "github.com/multica-ai/multica/server/internal/cerebro/capabilities"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentCapabilitySwap is the swap preview: both engines' own field lists, plus
// the transition between them.
type AgentCapabilitySwap struct {
	AgentID string `json:"agent_id"`
	// Current and Target are the same shape the capabilities card publishes for
	// the agent's live engine, so the UI renders the destination form from
	// Target.ExecOptions without a second request.
	Current AgentCapabilityRuntimeOptions `json:"current"`
	Target  AgentCapabilityRuntimeOptions `json:"target"`
	// SameRuntime marks the no-op case (target == current runtime) so the UI can
	// say "nothing changes" instead of rendering an empty diff that reads like a
	// loading state.
	SameRuntime bool                           `json:"same_runtime"`
	Impact      cerebrocapabilities.SwapImpact `json:"impact"`
}

// buildAgentCapabilitySwap is the pure core (unit-tested without a DB).
func buildAgentCapabilitySwap(agentID string, current, target AgentCapabilityRuntimeOptions) AgentCapabilitySwap {
	return AgentCapabilitySwap{
		AgentID:     agentID,
		Current:     current,
		Target:      target,
		SameRuntime: current.RuntimeID != "" && current.RuntimeID == target.RuntimeID,
		Impact:      cerebrocapabilities.SwapImpactFor(current.Provider, target.Provider),
	}
}

// GetAgentCapabilitySwap handles
// GET /api/agents/{id}/capabilities/swap?runtime_id={target}.
//
// Access control is delegated to loadAgentForUser — the same gate as the
// capabilities card — and the target runtime is resolved within the agent's own
// workspace, so this route cannot be used to probe another workspace's fleet.
func (h *Handler) GetAgentCapabilitySwap(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	targetID := r.URL.Query().Get("runtime_id")
	if targetID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	targetUUID, err := util.ParseUUID(targetID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "runtime_id is not a valid id")
		return
	}

	rt, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          targetUUID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	writeJSON(w, http.StatusOK, buildAgentCapabilitySwap(
		uuidToString(agent.ID),
		h.buildRuntimeExecOptions(r, agent),
		runtimeExecOptionsFromProvider(rt.Provider, rt.CliVersion.String, uuidToString(rt.ID)),
	))
}
