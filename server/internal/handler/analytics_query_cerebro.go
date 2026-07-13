package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	cerebroanalytics "github.com/multica-ai/multica/server/internal/cerebro/analytics"
)

const maxAnalyticsQueryBody = 1 << 20

func (h *Handler) GetAnalyticsCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, cerebroanalytics.ContractCatalog())
}

func (h *Handler) QueryAnalytics(w http.ResponseWriter, r *http.Request) {
	var query cerebroanalytics.Query
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAnalyticsQueryBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&query); err != nil {
		writeError(w, http.StatusBadRequest, "invalid analytics query")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid analytics query")
		return
	}
	if err := query.Normalize(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	result, err := cerebroanalytics.NewExecutor(h.DB).Execute(r.Context(), query, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query analytics")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) BackfillAnalytics(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if member.Role != "owner" && member.Role != "admin" {
		writeError(w, http.StatusForbidden, "owner or admin access required")
		return
	}
	if h.AnalyticsBackfiller == nil {
		writeError(w, http.StatusServiceUnavailable, "analytics backfill is unavailable")
		return
	}
	var request struct {
		Cursor string `json:"cursor,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAnalyticsQueryBody)).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid analytics backfill")
		return
	}
	if request.Limit == 0 {
		request.Limit = 250
	}
	next, count, err := h.AnalyticsBackfiller.BackfillWorkspace(r.Context(), workspaceID, request.Cursor, request.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to backfill analytics")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cursor": next, "count": count, "complete": count < request.Limit})
}
