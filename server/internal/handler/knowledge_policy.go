package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/service"
)

// Publish policies (spec 3.3) and the critic's review. Anyone who writes may
// read the policies; owners and admins set them. A review comes from an agent
// in another squad (the service decides that), so members and agents may call
// it.

// SetKnowledgePolicyRequest is the body of PUT /api/knowledge/policies.
type SetKnowledgePolicyRequest struct {
	Scope string `json:"scope"`
	Key   string `json:"key"`
	Mode  string `json:"mode"`
}

// ReviewKnowledgeDocumentRequest is the body of POST
// /api/knowledge/documents/{id}/review.
type ReviewKnowledgeDocumentRequest struct {
	Verdict string `json:"verdict"`
	Note    string `json:"note"`
}

// ListKnowledgePolicies (GET /api/knowledge/policies) lists the set policies.
// A scope without a row uses the default, manual.
func (h *Handler) ListKnowledgePolicies(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	policies, err := h.Knowledge.ListPublishPolicies(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": policies, "default_mode": service.PublishModeManual})
}

// SetKnowledgePolicy (PUT /api/knowledge/policies) saves the policy of a base or a
// kind. Owners and admins only.
func (h *Handler) SetKnowledgePolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorUUID, ok := parseUUIDOrBadRequest(w, userID, "actor id")
	if !ok {
		return
	}
	var req SetKnowledgePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	view, err := h.Knowledge.SetPublishPolicy(r.Context(), parseUUID(workspaceID), actorUUID, req.Scope, req.Key, req.Mode)
	if writeTaxonomyError(w, err) {
		return
	}
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// DeleteKnowledgePolicy (DELETE /api/knowledge/policies?scope=&key=) removes a
// policy, so its scope falls back to manual. Owners and admins only.
func (h *Handler) DeleteKnowledgePolicy(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return
	}
	q := r.URL.Query()
	err := h.Knowledge.DeletePublishPolicy(r.Context(), parseUUID(workspaceID), q.Get("scope"), q.Get("key"))
	if writeTaxonomyError(w, err) {
		return
	}
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ReviewKnowledgeDocument (POST /api/knowledge/documents/{id}/review) records a
// critic's verdict on a draft. An "ok" from another squad's agent publishes it
// when the policy is auto_after_review.
func (h *Handler) ReviewKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var req ReviewKnowledgeDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	view, err := h.Knowledge.ReviewDocument(r.Context(), parseUUID(workspaceID), id,
		service.KnowledgeActor{Type: actorType, ID: actorUUID}, req.Verdict, req.Note)
	if writeTaxonomyError(w, err) {
		return
	}
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
