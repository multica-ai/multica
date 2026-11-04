package handler

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
)

// The repo-docs sync setting (plan 10.12). The router keeps these routes to
// owners and admins. A run now does the work the daily job does for the
// workspace and answers with the setting, including the failures of that run.

// UpdateRepoSyncRequest is the body of PUT /api/knowledge/repo-sync. Empty branch,
// docs path and base take their defaults; the service validates the rest.
type UpdateRepoSyncRequest struct {
	Enabled          bool   `json:"enabled"`
	RepoURL          string `json:"repo_url"`
	Branch           string `json:"branch"`
	DocsPath         string `json:"docs_path"`
	KnowledgeBaseKey string `json:"knowledge_base_key"`
}

// GetKnowledgeRepoSync (GET /api/knowledge/repo-sync) returns the setting with
// the outcome of its last run.
func (h *Handler) GetKnowledgeRepoSync(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	status, err := h.Knowledge.GetRepoSync(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// UpdateKnowledgeRepoSync (PUT /api/knowledge/repo-sync) saves the setting. Turning
// it on does not clone the repository; the daily job or a run now does.
func (h *Handler) UpdateKnowledgeRepoSync(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	var req UpdateRepoSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status, err := h.Knowledge.SetRepoSync(r.Context(), parseUUID(workspaceID), service.RepoSyncSettings{
		Enabled: req.Enabled, RepoURL: req.RepoURL, Branch: req.Branch,
		DocsPath: req.DocsPath, KBKey: req.KnowledgeBaseKey,
	})
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// RunKnowledgeRepoSync (POST /api/knowledge/repo-sync/run) syncs now. A disabled
// setting does nothing and answers with the setting as it is. Per-file failures
// are on the setting's last_error, not an HTTP error.
func (h *Handler) RunKnowledgeRepoSync(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if err := h.Knowledge.SyncRepoDocs(r.Context(), parseUUID(workspaceID)); err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	status, err := h.Knowledge.GetRepoSync(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeKnowledgeError(w, r, workspaceID, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}
