package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	cerebroanalytics "github.com/multica-ai/multica/server/internal/cerebro/analytics"
)

type analyticsVisualRequest struct {
	Name       string          `json:"name"`
	VisualType string          `json:"visual_type"`
	Query      json.RawMessage `json:"query"`
	Display    json.RawMessage `json:"display,omitempty"`
	Position   int             `json:"position,omitempty"`
}

type analyticsVisualResponse struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	VisualType string          `json:"visual_type"`
	Query      json.RawMessage `json:"query"`
	Display    json.RawMessage `json:"display"`
	Position   int             `json:"position"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func validateAnalyticsVisual(request analyticsVisualRequest) error {
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len(request.Name) > 120 {
		return fmt.Errorf("visual name must be between 1 and 120 characters")
	}
	switch request.VisualType {
	case "activity", "table", "bars":
	default:
		return fmt.Errorf("invalid visual type")
	}
	var query cerebroanalytics.Query
	if err := json.Unmarshal(request.Query, &query); err != nil {
		return fmt.Errorf("invalid visual query")
	}
	if err := query.Normalize(); err != nil {
		return err
	}
	if len(request.Display) > 0 && !json.Valid(request.Display) {
		return fmt.Errorf("invalid visual display")
	}
	return nil
}

func (h *Handler) ListAnalyticsVisuals(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	rows, err := h.DB.Query(r.Context(), `SELECT id::text,name,visual_type,query,display,position,created_at,updated_at FROM cerebro_analytics_visual WHERE workspace_id=$1 AND owner_id=$2 ORDER BY position,created_at`, parseUUID(workspaceID), member.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list analytics visuals")
		return
	}
	defer rows.Close()
	visuals := []analyticsVisualResponse{}
	for rows.Next() {
		var visual analyticsVisualResponse
		if err := rows.Scan(&visual.ID, &visual.Name, &visual.VisualType, &visual.Query, &visual.Display, &visual.Position, &visual.CreatedAt, &visual.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read analytics visuals")
			return
		}
		visuals = append(visuals, visual)
	}
	writeJSON(w, http.StatusOK, visuals)
}

func (h *Handler) CreateAnalyticsVisual(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	request, ok := decodeAnalyticsVisualRequest(w, r)
	if !ok {
		return
	}
	if len(request.Display) == 0 {
		request.Display = json.RawMessage(`{}`)
	}
	var visual analyticsVisualResponse
	err := h.DB.QueryRow(r.Context(), `INSERT INTO cerebro_analytics_visual(workspace_id,owner_id,name,visual_type,query,display,position) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text,name,visual_type,query,display,position,created_at,updated_at`, parseUUID(workspaceID), member.UserID, strings.TrimSpace(request.Name), request.VisualType, request.Query, request.Display, request.Position).Scan(&visual.ID, &visual.Name, &visual.VisualType, &visual.Query, &visual.Display, &visual.Position, &visual.CreatedAt, &visual.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create analytics visual")
		return
	}
	writeJSON(w, http.StatusCreated, visual)
}

func (h *Handler) UpdateAnalyticsVisual(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	visualID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "visualId"), "visual id")
	if !ok {
		return
	}
	request, ok := decodeAnalyticsVisualRequest(w, r)
	if !ok {
		return
	}
	if len(request.Display) == 0 {
		request.Display = json.RawMessage(`{}`)
	}
	var visual analyticsVisualResponse
	err := h.DB.QueryRow(r.Context(), `UPDATE cerebro_analytics_visual SET name=$1,visual_type=$2,query=$3,display=$4,position=$5,updated_at=now() WHERE id=$6 AND workspace_id=$7 AND owner_id=$8 RETURNING id::text,name,visual_type,query,display,position,created_at,updated_at`, strings.TrimSpace(request.Name), request.VisualType, request.Query, request.Display, request.Position, visualID, parseUUID(workspaceID), member.UserID).Scan(&visual.ID, &visual.Name, &visual.VisualType, &visual.Query, &visual.Display, &visual.Position, &visual.CreatedAt, &visual.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "analytics visual not found")
		return
	}
	writeJSON(w, http.StatusOK, visual)
}

func (h *Handler) DeleteAnalyticsVisual(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	visualID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "visualId"), "visual id")
	if !ok {
		return
	}
	tag, err := h.DB.Exec(r.Context(), `DELETE FROM cerebro_analytics_visual WHERE id=$1 AND workspace_id=$2 AND owner_id=$3`, visualID, parseUUID(workspaceID), member.UserID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "analytics visual not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeAnalyticsVisualRequest(w http.ResponseWriter, r *http.Request) (analyticsVisualRequest, bool) {
	var request analyticsVisualRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAnalyticsQueryBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid analytics visual")
		return request, false
	}
	if err := validateAnalyticsVisual(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return request, false
	}
	return request, true
}
