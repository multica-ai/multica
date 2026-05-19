package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type WikiPageActivityResponse struct {
	ID        string          `json:"id"`
	PageID    string          `json:"page_id"`
	ActorID   *string         `json:"actor_id"`
	Action    string          `json:"action"`
	Details   json.RawMessage `json:"details"`
	CreatedAt string          `json:"created_at"`
}

func wikiPageActivityToResponse(a db.WikiPageActivity) WikiPageActivityResponse {
	details := json.RawMessage(a.Details)
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	return WikiPageActivityResponse{
		ID:        uuidToString(a.ID),
		PageID:    uuidToString(a.PageID),
		ActorID:   uuidToPtr(a.ActorID),
		Action:    a.Action,
		Details:   details,
		CreatedAt: timestampToString(a.CreatedAt),
	}
}

func (h *Handler) ListWikiPageActivities(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	pageID := chi.URLParam(r, "id")
	pageUUID, ok := parseUUIDOrBadRequest(w, pageID, "wiki page id")
	if !ok {
		return
	}

	// Verify the page belongs to this workspace
	if _, err := h.Queries.GetWikiPage(r.Context(), db.GetWikiPageParams{
		ID:          pageUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "wiki page not found")
		return
	}

	limit := int32(50)
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = int32(parsed)
		}
	}

	activities, err := h.Queries.ListWikiPageActivities(r.Context(), db.ListWikiPageActivitiesParams{
		PageID: pageUUID,
		Limit:  limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}

	resp := make([]WikiPageActivityResponse, len(activities))
	for i, a := range activities {
		resp[i] = wikiPageActivityToResponse(a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": resp, "total": len(resp)})
}

func (h *Handler) recordWikiActivity(r *http.Request, wsUUID, pageUUID, actorID pgtype.UUID, action string, details map[string]any) {
	detailsJSON, _ := json.Marshal(details)
	_, _ = h.Queries.CreateWikiPageActivity(r.Context(), db.CreateWikiPageActivityParams{
		WorkspaceID: wsUUID,
		PageID:      pageUUID,
		ActorID:     actorID,
		Action:      action,
		Details:     detailsJSON,
	})
}
