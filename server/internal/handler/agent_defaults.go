package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetPersonalAgentDefaults returns the current user's personal agent defaults
// for the workspace. Returns an empty config object when no record exists.
func (h *Handler) GetPersonalAgentDefaults(w http.ResponseWriter, r *http.Request) {
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	cfg, err := h.Queries.GetMemberAgentConfig(r.Context(), db.GetMemberAgentConfigParams{
		MemberID:    member.ID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		// No record → return empty config
		writeJSON(w, http.StatusOK, map[string]any{"config": map[string]any{}})
		return
	}

	var config any
	if err := json.Unmarshal(cfg.Config, &config); err != nil {
		config = map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         uuidToString(cfg.ID),
		"config":     config,
		"created_at": timestampToString(cfg.CreatedAt),
		"updated_at": timestampToString(cfg.UpdatedAt),
	})
}

type updatePersonalAgentDefaultsRequest struct {
	Config any `json:"config"`
}

// UpdatePersonalAgentDefaults creates or updates the current user's personal
// agent defaults for the workspace.
func (h *Handler) UpdatePersonalAgentDefaults(w http.ResponseWriter, r *http.Request) {
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req updatePersonalAgentDefaultsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Config == nil {
		writeError(w, http.StatusBadRequest, "config is required")
		return
	}

	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid config")
		return
	}

	cfg, err := h.Queries.UpsertMemberAgentConfig(r.Context(), db.UpsertMemberAgentConfigParams{
		MemberID:    member.ID,
		WorkspaceID: parseUUID(workspaceID),
		Config:      configJSON,
	})
	if err != nil {
		slog.Warn("upsert member agent config failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to save agent defaults")
		return
	}

	var config any
	if err := json.Unmarshal(cfg.Config, &config); err != nil {
		config = map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         uuidToString(cfg.ID),
		"config":     config,
		"created_at": timestampToString(cfg.CreatedAt),
		"updated_at": timestampToString(cfg.UpdatedAt),
	})
}
