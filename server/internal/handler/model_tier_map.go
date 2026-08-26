package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// modelTierMapResponse is tier -> concrete.
type modelTierMapResponse map[string]string

func modelTierMapToResponse(rows []db.ModelTierMap) modelTierMapResponse {
	m := make(modelTierMapResponse, len(rows))
	for _, r := range rows {
		m[r.Tier] = r.Concrete
	}
	return m
}

// GetModelMap handles GET /api/model-map (global, workspace_id IS NULL).
func (h *Handler) GetModelMap(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	rows, err := h.Queries.ListGlobalModelTierMap(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list model map")
		return
	}
	writeJSON(w, http.StatusOK, modelTierMapToResponse(rows))
}

// PatchModelMap handles PATCH /api/model-map. Body: {"cheap":"x","balanced":"y","premium":"z"}.
// ponytail: skip validation beyond non-empty, upsert each tier.
func (h *Handler) PatchModelMap(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req) == 0 {
		writeError(w, http.StatusBadRequest, "empty model map")
		return
	}
	for tier, concrete := range req {
		if tier == "" || concrete == "" {
			writeError(w, http.StatusBadRequest, "tier and concrete required")
			return
		}
		if _, err := h.Queries.UpsertGlobalModelTier(r.Context(), db.UpsertGlobalModelTierParams{Tier: tier, Concrete: concrete}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to upsert model map")
			return
		}
	}
	rows, err := h.Queries.ListGlobalModelTierMap(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list model map")
		return
	}
	writeJSON(w, http.StatusOK, modelTierMapToResponse(rows))
}

// GetWorkspaceModelMap handles GET /api/workspaces/{id}/model-map and GET /api/workspaces/{id}/settings.
func (h *Handler) GetWorkspaceModelMap(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		workspaceID = chi.URLParam(r, "workspaceId")
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	rows, err := h.Queries.ListWorkspaceModelTierMap(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace model map")
		return
	}
	writeJSON(w, http.StatusOK, modelTierMapToResponse(rows))
}

// PatchWorkspaceModelMap handles PATCH /api/workspaces/{id}/model-map and PATCH /api/workspaces/{id}/settings.
func (h *Handler) PatchWorkspaceModelMap(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		workspaceID = chi.URLParam(r, "workspaceId")
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// allow empty to clear? minimal: require non-empty
	if len(req) == 0 {
		writeError(w, http.StatusBadRequest, "empty model map")
		return
	}
	for tier, concrete := range req {
		if tier == "" || concrete == "" {
			// empty concrete means delete override
			if tier != "" && concrete == "" {
				_ = h.Queries.DeleteWorkspaceModelTier(r.Context(), db.DeleteWorkspaceModelTierParams{WorkspaceID: wsUUID, Tier: tier})
				continue
			}
			writeError(w, http.StatusBadRequest, "tier and concrete required")
			return
		}
		if _, err := h.Queries.UpsertWorkspaceModelTier(r.Context(), db.UpsertWorkspaceModelTierParams{WorkspaceID: wsUUID, Tier: tier, Concrete: concrete}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to upsert workspace model map")
			return
		}
	}
	rows, err := h.Queries.ListWorkspaceModelTierMap(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace model map")
		return
	}
	writeJSON(w, http.StatusOK, modelTierMapToResponse(rows))
}
