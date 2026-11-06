package handler

import (
	"encoding/json"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
)

func (h *Handler) GetAIDOwnerBinding(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") != "" {
		writeError(w, 403, "machine credentials cannot inspect AIDO binding")
		return
	}
	agent, _, ok := h.authorizeAgentEnv(w, r)
	if !ok {
		return
	}
	binding, err := h.Queries.GetAIDOwnerBinding(r.Context(), agent.ID)
	if err != nil {
		writeError(w, 404, "AIDO owner binding not configured")
		return
	}
	writeJSON(w, 200, binding)
}

// BindAIDOwner is an explicit human-admin operation, not agent metadata. The
// runtime must already be the dedicated OS identity's runtime. This endpoint
// narrows API authority; it cannot attest a host UID or provision credentials.
func (h *Handler) BindAIDOwner(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") != "" {
		writeError(w, 403, "machine credentials cannot bind AIDO owner runtime")
		return
	}
	agent, _, ok := h.authorizeAgentEnv(w, r)
	if !ok {
		return
	}
	var body struct {
		RuntimeID string `json:"runtime_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, 400, "invalid binding request")
		return
	}
	runtimeID, ok := parseUUIDOrBadRequest(w, body.RuntimeID, "runtime id")
	if !ok {
		return
	}
	if !agent.RuntimeID.Valid || agent.RuntimeID != runtimeID {
		writeError(w, 409, "agent must use the dedicated runtime before binding")
		return
	}
	runtime, err := h.Queries.GetAgentRuntime(r.Context(), runtimeID)
	if err != nil || runtime.WorkspaceID != agent.WorkspaceID {
		writeError(w, 403, "runtime outside agent workspace")
		return
	}
	binding, err := h.Queries.CreateAIDOwnerBinding(r.Context(), db.CreateAIDOwnerBindingParams{AgentID: agent.ID, RuntimeID: runtimeID})
	if err != nil {
		writeError(w, 409, "binding exists or cannot be established")
		return
	}
	writeJSON(w, http.StatusCreated, binding)
}
