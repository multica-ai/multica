package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var validIssueStatuses = map[string]struct{}{
	"backlog":     {},
	"todo":        {},
	"in_progress": {},
	"in_review":   {},
	"done":        {},
	"blocked":     {},
	"cancelled":   {},
}

type ColumnConfigResponse struct {
	ID                 string   `json:"id"`
	WorkspaceID        string   `json:"workspace_id"`
	Status             string   `json:"status"`
	Instructions       string   `json:"instructions"`
	AllowedTransitions []string `json:"allowed_transitions"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}

func columnConfigToResponse(cfg db.WorkspaceColumnConfig) ColumnConfigResponse {
	transitions := cfg.AllowedTransitions
	if transitions == nil {
		transitions = []string{}
	}

	return ColumnConfigResponse{
		ID:                 uuidToString(cfg.ID),
		WorkspaceID:        uuidToString(cfg.WorkspaceID),
		Status:             cfg.Status,
		Instructions:       cfg.Instructions,
		AllowedTransitions: transitions,
		CreatedAt:          timestampToString(cfg.CreatedAt),
		UpdatedAt:          timestampToString(cfg.UpdatedAt),
	}
}

type UpsertColumnConfigRequest struct {
	Instructions       string   `json:"instructions"`
	AllowedTransitions []string `json:"allowed_transitions"`
}

func (h *Handler) ListColumnConfigs(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")

	configs, err := h.Queries.ListColumnConfigs(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list column configs")
		return
	}

	resp := make([]ColumnConfigResponse, 0, len(configs))
	for _, cfg := range configs {
		resp = append(resp, columnConfigToResponse(cfg))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpsertColumnConfig(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	status := strings.TrimSpace(chi.URLParam(r, "status"))
	if !isValidIssueStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	var req UpsertColumnConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	transitions, err := normalizeAllowedTransitions(req.AllowedTransitions)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	cfg, err := h.Queries.UpsertColumnConfig(r.Context(), db.UpsertColumnConfigParams{
		WorkspaceID:        parseUUID(workspaceID),
		Status:             status,
		Instructions:       req.Instructions,
		AllowedTransitions: transitions,
	})
	if err != nil {
		slog.Warn("failed to upsert column config", "workspace_id", workspaceID, "status", status, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save column config")
		return
	}

	writeJSON(w, http.StatusOK, columnConfigToResponse(cfg))
}

func normalizeAllowedTransitions(transitions []string) ([]string, error) {
	if transitions == nil {
		return []string{}, nil
	}

	normalized := make([]string, len(transitions))
	for i, transition := range transitions {
		transition = strings.TrimSpace(transition)
		if !isValidIssueStatus(transition) {
			return nil, errors.New("allowed_transitions contains invalid status")
		}
		normalized[i] = transition
	}

	return normalized, nil
}

func isValidIssueStatus(status string) bool {
	_, ok := validIssueStatuses[status]
	return ok
}
