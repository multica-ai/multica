package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// putModelHealthRequest is the body for PUT /api/model-health.
type putModelHealthRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	ConcreteModel string `json:"concrete_model"`
	Status        string `json:"status"`
	Reason        string `json:"reason"`
}

// GetModelHealthList handles GET /api/model-health. Optional ?workspace=<id>;
// if absent the query uses a NULL workspace_id (global + unbound rows).
func (h *Handler) GetModelHealthList(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	var wsID pgtype.UUID
	if ws := r.URL.Query().Get("workspace"); ws != "" {
		parsed, ok := parseUUIDOrBadRequest(w, ws, "workspace id")
		if !ok {
			return
		}
		wsID = parsed
	}
	rows, err := h.Queries.ListModelHealth(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list model health")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// PutModelHealth handles PUT /api/model-health. Upserts a model health row and
// returns the resulting row. status "healthy"/"ok" marks healthy; anything else
// marks unhealthy (with an optional reason).
func (h *Handler) PutModelHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	var req putModelHealthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ConcreteModel == "" {
		writeError(w, http.StatusBadRequest, "concrete_model required")
		return
	}
	var wsID pgtype.UUID
	if req.WorkspaceID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace id")
		if !ok {
			return
		}
		wsID = parsed
	}
	switch req.Status {
	case "healthy", "ok":
		if err := h.Queries.MarkModelHealthy(r.Context(), db.MarkModelHealthyParams{WorkspaceID: wsID, Concrete: req.ConcreteModel}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to mark model healthy")
			return
		}
	default:
		var reason pgtype.Text
		if req.Reason != "" {
			reason = pgtype.Text{String: req.Reason, Valid: true}
		}
		if _, err := h.Queries.UpsertModelHealthUnhealthy(r.Context(), db.UpsertModelHealthUnhealthyParams{WorkspaceID: wsID, Concrete: req.ConcreteModel, Reason: reason}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to mark model unhealthy")
			return
		}
	}
	row, err := h.Queries.GetModelHealth(r.Context(), db.GetModelHealthParams{WorkspaceID: wsID, Concrete: req.ConcreteModel})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch model health")
		return
	}
	writeJSON(w, http.StatusOK, row)
}
