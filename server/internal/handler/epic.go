package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type EpicResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Color       string  `json:"color"`
	Status      string  `json:"status"`
	IssueCount  int64   `json:"issue_count"`
	DoneCount   int64   `json:"done_count"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func epicToResponse(e db.Epic) EpicResponse {
	return EpicResponse{
		ID:          uuidToString(e.ID),
		WorkspaceID: uuidToString(e.WorkspaceID),
		Title:       e.Title,
		Description: textToPtr(e.Description),
		Color:       e.Color,
		Status:      e.Status,
		CreatedAt:   timestampToString(e.CreatedAt),
		UpdatedAt:   timestampToString(e.UpdatedAt),
	}
}

func (h *Handler) loadEpicIssueStats(ctx context.Context, epicID pgtype.UUID) (int64, int64) {
	stats, err := h.Queries.GetEpicIssueStats(ctx, []pgtype.UUID{epicID})
	if err != nil || len(stats) == 0 {
		return 0, 0
	}
	return stats[0].TotalCount, stats[0].DoneCount
}

type CreateEpicRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
}

type UpdateEpicRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Status      *string `json:"status"`
}

var validEpicStatuses = []string{"open", "closed"}

func (h *Handler) ListEpics(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	epics, err := h.Queries.ListEpics(r.Context(), db.ListEpicsParams{
		WorkspaceID: wsUUID,
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list epics")
		return
	}

	// Batch-fetch issue stats for all epics
	statsMap := make(map[string]db.GetEpicIssueStatsRow)
	if len(epics) > 0 {
		epicIDs := make([]pgtype.UUID, len(epics))
		for i, e := range epics {
			epicIDs[i] = e.ID
		}
		stats, err := h.Queries.GetEpicIssueStats(r.Context(), epicIDs)
		if err == nil {
			for _, s := range stats {
				statsMap[uuidToString(s.EpicID)] = s
			}
		}
	}

	resp := make([]EpicResponse, len(epics))
	for i, e := range epics {
		resp[i] = epicToResponse(e)
		if s, ok := statsMap[resp[i].ID]; ok {
			resp[i].IssueCount = s.TotalCount
			resp[i].DoneCount = s.DoneCount
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"epics": resp, "total": len(resp)})
}

func (h *Handler) GetEpic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "epic id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	epic, err := h.Queries.GetEpicInWorkspace(r.Context(), db.GetEpicInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}
	resp := epicToResponse(epic)
	resp.IssueCount, resp.DoneCount = h.loadEpicIssueStats(r.Context(), epic.ID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateEpic(w http.ResponseWriter, r *http.Request) {
	var req CreateEpicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	color := "#6366f1"
	if req.Color != nil {
		color = *req.Color
	}

	epic, err := h.Queries.CreateEpic(r.Context(), db.CreateEpicParams{
		WorkspaceID: wsUUID,
		Title:       req.Title,
		Description: ptrToText(req.Description),
		Color:       color,
		Status:      "open",
	})
	if err != nil {
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "epic create rejected: a field value failed a database constraint")
			return
		}
		slog.Error("epic create failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create epic")
		return
	}
	resp := epicToResponse(epic)
	h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{"epic": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateEpic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "epic id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	prevEpic, err := h.Queries.GetEpicInWorkspace(r.Context(), db.GetEpicInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateEpicRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateEpicParams{
		ID:          prevEpic.ID,
		Description: prevEpic.Description,
		Color:       pgtype.Text{String: prevEpic.Color, Valid: true},
	}
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Status != nil {
		if !validateProjectEnum(w, "status", *req.Status, validEpicStatuses) {
			return
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Color != nil {
		params.Color = pgtype.Text{String: *req.Color, Valid: true}
	}
	if _, ok := rawFields["description"]; ok {
		if req.Description != nil {
			params.Description = pgtype.Text{String: *req.Description, Valid: true}
		} else {
			params.Description = pgtype.Text{Valid: false}
		}
	}
	epic, err := h.Queries.UpdateEpic(r.Context(), params)
	if err != nil {
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "epic update rejected: a field value failed a database constraint")
			return
		}
		slog.Error("epic update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update epic")
		return
	}
	resp := epicToResponse(epic)
	resp.IssueCount, resp.DoneCount = h.loadEpicIssueStats(r.Context(), epic.ID)
	h.publish(protocol.EventProjectUpdated, workspaceID, "member", userID, map[string]any{"epic": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteEpic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "epic id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	epic, err := h.Queries.GetEpicInWorkspace(r.Context(), db.GetEpicInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteEpic(r.Context(), db.DeleteEpicParams{
		ID:          epic.ID,
		WorkspaceID: epic.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete epic")
		return
	}
	h.publish(protocol.EventProjectDeleted, workspaceID, "member", userID, map[string]any{"epic_id": uuidToString(epic.ID)})
	w.WriteHeader(http.StatusNoContent)
}
