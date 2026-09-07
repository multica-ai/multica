package handler

import "net/http"

// Ordinary teardown must not impersonate a native failure or revoke an
// accepted execution target. Ownership must first be released explicitly.
func (h *Handler) controllerRuntimeMutation(w http.ResponseWriter, r *http.Request, runtimeID string) bool {
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return true
	}
	runtimeID = uuidToString(runtimeUUID)
	if h.DB == nil {
		return false
	}
	var owned bool
	err := h.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM issue_controller c,jsonb_array_elements(c.config->'targets') t WHERE t->>'runtime_id'=$1)`, runtimeID).Scan(&owned)
	if err != nil {
		writeError(w, 503, "controller runtime authority unavailable")
		return true
	}
	if owned {
		writeError(w, 403, "runtime is bound to controlled issue authority")
		return true
	}
	return false
}
