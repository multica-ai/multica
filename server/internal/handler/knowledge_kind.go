package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/service"
)

// Knowledge kinds and the Librarian's health. Members and agents read the kinds
// (an agent needs the template to write a document); only owners and admins
// edit them. The health endpoint returns the latest run of each Librarian job.

// writeTaxonomyError answers the errors of the taxonomy, policy and eval
// endpoints. It returns false for any other error, which the caller passes on.
func writeTaxonomyError(w http.ResponseWriter, err error) bool {
	var missing *service.KnowledgeMissingSectionsError
	switch {
	case errors.As(err, &missing):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":            "the document lacks the headings its kind requires",
			"knowledge_kind":   missing.Kind,
			"missing_sections": missing.Missing,
		})
	case errors.Is(err, service.ErrKnowledgeKindNotFound):
		writeError(w, http.StatusNotFound, "knowledge kind not found")
	case errors.Is(err, service.ErrKnowledgePolicyNotFound):
		writeError(w, http.StatusNotFound, "no publish policy is set for that scope")
	case errors.Is(err, service.ErrKnowledgeEvalCaseNotFound):
		writeError(w, http.StatusNotFound, "eval case not found")
	case errors.Is(err, service.ErrKnowledgeNotCritic):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		return false
	}
	return true
}

// ListKnowledgeKinds (GET /api/knowledge/kinds) lists the workspace's kinds with
// their templates and required headings.
func (h *Handler) ListKnowledgeKinds(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	kinds, err := h.Knowledge.ListKnowledgeKinds(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kinds": kinds})
}

// UpdateKnowledgeKindRequest is the body of PATCH /api/knowledge/kinds/{key}.
// A field left out is kept.
type UpdateKnowledgeKindRequest struct {
	Name             *string   `json:"name"`
	Template         *string   `json:"template"`
	ReviewDays       *int      `json:"review_days"`
	RequiredSections *[]string `json:"required_sections"`
	DefaultBaseKey   *string   `json:"default_base_key"`
}

// UpdateKnowledgeKind (PATCH /api/knowledge/kinds/{key}) edits a kind. Owners and
// admins only; the router enforces the role and requireKnowledgeAdmin keeps
// agents out.
func (h *Handler) UpdateKnowledgeKind(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if !h.requireKnowledgeAdmin(w, r, workspaceID) {
		return
	}
	var req UpdateKnowledgeKindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	view, err := h.Knowledge.UpdateKnowledgeKind(r.Context(), parseUUID(workspaceID), chi.URLParam(r, "key"), service.KnowledgeKindPatch{
		Name: req.Name, Template: req.Template, ReviewDays: req.ReviewDays, RequiredSections: req.RequiredSections,
		DefaultBaseKey: req.DefaultBaseKey,
	})
	if writeTaxonomyError(w, err) {
		return
	}
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// GetKnowledgeHealth (GET /api/knowledge/health) returns the latest run of each
// Librarian job: triage, freshness, audit, eval.
func (h *Handler) GetKnowledgeHealth(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	jobs, err := h.Knowledge.LatestHealth(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}
