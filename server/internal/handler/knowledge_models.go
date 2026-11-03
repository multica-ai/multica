package handler

import (
	"net/http"
)

// TestKnowledgeModels (POST /api/knowledge/models/test) runs each configured
// model once and reports which failed and why. A deployment with no model
// configured answers configured: false, not an error.
func (h *Handler) TestKnowledgeModels(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return
	}
	checks, err := h.Knowledge.TestModels(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeKnowledgeAdminError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, checks)
}

// RevealKnowledgeOwnerPassword (POST /api/knowledge/owner-account/reveal) shows
// the caller's WeKnora password once, for signing in to WeKnora's own UI. The
// response is never cached, and a second request is refused.
func (h *Handler) RevealKnowledgeOwnerPassword(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	login, err := h.Knowledge.RevealOwnerPassword(r.Context(), parseUUID(workspaceID), userUUID)
	if err != nil {
		writeKnowledgeAdminError(w, r, workspaceID, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, login)
}
