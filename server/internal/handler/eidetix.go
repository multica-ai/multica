package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultEidetixEndpoint = "https://eidetix.nodeops.xyz/mcp/sse"

// authorizeEidetixConfig resolves the project from the URL and enforces
// workspace owner/admin. Mirrors the owner/admin write gate used elsewhere.
func (h *Handler) authorizeEidetixConfig(w http.ResponseWriter, r *http.Request) (db.Project, db.Member, bool) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Project{}, db.Member{}, false
	}
	member, ok := h.requireWorkspaceRole(w, r, uuidToString(project.WorkspaceID), "project not found", "owner", "admin")
	if !ok {
		return db.Project{}, db.Member{}, false
	}
	return project, member, true
}

type setEidetixRequest struct {
	Token       string  `json:"token"`
	EndpointURL string  `json:"endpoint_url"`
	GraphLabel  *string `json:"graph_label"`
	Enabled     *bool   `json:"enabled"`
}

type eidetixShowResponse struct {
	Configured  bool   `json:"configured"`
	Enabled     bool   `json:"enabled"`
	EndpointURL string `json:"endpoint_url,omitempty"`
	GraphLabel  string `json:"graph_label,omitempty"`
}

// SetEidetixConfig upserts the project's Eidetix binding. Requires a non-empty
// token. The token is encrypted and never echoed back.
func (h *Handler) SetEidetixConfig(w http.ResponseWriter, r *http.Request) {
	project, _, ok := h.authorizeEidetixConfig(w, r)
	if !ok {
		return
	}
	if h.EidetixSecrets == nil {
		writeError(w, http.StatusServiceUnavailable, "eidetix is not configured on this server (MULTICA_EIDETIX_SECRET_KEY unset)")
		return
	}

	var req setEidetixRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	sealed, err := h.EidetixSecrets.Seal([]byte(req.Token))
	if err != nil {
		slog.Error("eidetix: seal token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt token")
		return
	}

	endpoint := strings.TrimSpace(req.EndpointURL)
	if endpoint == "" {
		endpoint = defaultEidetixEndpoint
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	label := pgtype.Text{}
	if req.GraphLabel != nil && strings.TrimSpace(*req.GraphLabel) != "" {
		label = pgtype.Text{String: strings.TrimSpace(*req.GraphLabel), Valid: true}
	}

	cfg, err := h.Queries.UpsertEidetixProjectConfig(r.Context(), db.UpsertEidetixProjectConfigParams{
		ProjectID:      project.ID,
		Enabled:        enabled,
		EndpointUrl:    endpoint,
		TokenEncrypted: sealed,
		GraphLabel:     label,
	})
	if err != nil {
		slog.Error("eidetix: upsert config failed", "error", err, "project_id", uuidToString(project.ID))
		writeError(w, http.StatusInternalServerError, "failed to save eidetix config")
		return
	}

	writeJSON(w, http.StatusOK, eidetixShowResponse{
		Configured:  true,
		Enabled:     cfg.Enabled,
		EndpointURL: cfg.EndpointUrl,
		GraphLabel:  cfg.GraphLabel.String,
	})
}

// ShowEidetixConfig reports status WITHOUT the token.
func (h *Handler) ShowEidetixConfig(w http.ResponseWriter, r *http.Request) {
	project, _, ok := h.authorizeEidetixConfig(w, r)
	if !ok {
		return
	}
	cfg, err := h.Queries.GetEidetixConfigForProject(r.Context(), project.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, eidetixShowResponse{Configured: false})
		return
	}
	writeJSON(w, http.StatusOK, eidetixShowResponse{
		Configured:  true,
		Enabled:     cfg.Enabled,
		EndpointURL: cfg.EndpointUrl,
		GraphLabel:  cfg.GraphLabel.String,
	})
}

type patchEidetixRequest struct {
	Enabled *bool `json:"enabled"`
}

// PatchEidetixConfig toggles the enabled flag on an existing config.
func (h *Handler) PatchEidetixConfig(w http.ResponseWriter, r *http.Request) {
	project, _, ok := h.authorizeEidetixConfig(w, r)
	if !ok {
		return
	}
	var req patchEidetixRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled (boolean) is required")
		return
	}
	cfg, err := h.Queries.SetEidetixProjectEnabled(r.Context(), db.SetEidetixProjectEnabledParams{
		ProjectID: project.ID,
		Enabled:   *req.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "eidetix not configured for this project")
		return
	}
	writeJSON(w, http.StatusOK, eidetixShowResponse{
		Configured:  true,
		Enabled:     cfg.Enabled,
		EndpointURL: cfg.EndpointUrl,
		GraphLabel:  cfg.GraphLabel.String,
	})
}

// ClearEidetixConfig deletes the project's binding.
func (h *Handler) ClearEidetixConfig(w http.ResponseWriter, r *http.Request) {
	project, _, ok := h.authorizeEidetixConfig(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteEidetixProjectConfig(r.Context(), project.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear eidetix config")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
