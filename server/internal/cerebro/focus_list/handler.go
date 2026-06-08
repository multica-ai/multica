// Package focus_list holds cerebro-only HTTP handlers for the personal focus
// list — a lightweight to-do surface pinned to the top of the inbox.
// FIR-2947.
package focus_list

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler exposes the cerebro-only focus-list HTTP endpoints.
type Handler struct {
	Cerebro *cerebrodb.Queries
}

// New constructs the handler.
func New(cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Cerebro: cerebro}
}

// focusListItemResponse is the JSON shape returned to the client.
type focusListItemResponse struct {
	ID           string  `json:"id"`
	Text         string  `json:"text"`
	IssueID      *string `json:"issue_id"`
	SnoozedUntil *string `json:"snoozed_until"`
	DoneAt       *string `json:"done_at"`
	Position     float64 `json:"position"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func toItemResponse(i cerebrodb.CerebroFocusListItem) focusListItemResponse {
	r := focusListItemResponse{
		ID:        util.UUIDToString(i.ID),
		Text:      i.Text,
		Position:  i.Position,
		CreatedAt: i.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt: i.UpdatedAt.Time.Format(time.RFC3339),
	}
	if i.IssueID.Valid {
		s := util.UUIDToString(i.IssueID)
		r.IssueID = &s
	}
	if i.SnoozedUntil.Valid {
		s := i.SnoozedUntil.Time.Format(time.RFC3339)
		r.SnoozedUntil = &s
	}
	if i.DoneAt.Valid {
		s := i.DoneAt.Time.Format(time.RFC3339)
		r.DoneAt = &s
	}
	return r
}

// List returns all focus list items for the authenticated member.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	wsID, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	items, err := h.Cerebro.ListFocusListItems(r.Context(), cerebrodb.ListFocusListItemsParams{
		WorkspaceID: wsID,
		MemberID:    userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list focus list items")
		return
	}
	resp := make([]focusListItemResponse, len(items))
	for i, item := range items {
		resp[i] = toItemResponse(item)
	}
	writeJSON(w, http.StatusOK, resp)
}

// Create adds a new item to the focus list.
// Body: {"text": "...", "issue_id": "uuid-or-null"}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	wsID, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	var body struct {
		Text    string  `json:"text"`
		IssueID *string `json:"issue_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if body.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	var issueUUID pgtype.UUID
	if body.IssueID != nil && *body.IssueID != "" {
		parsed, err := util.ParseUUID(*body.IssueID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue_id")
			return
		}
		issueUUID = parsed
	}

	item, err := h.Cerebro.CreateFocusListItem(r.Context(), cerebrodb.CreateFocusListItemParams{
		WorkspaceID: wsID,
		MemberID:    userID,
		Text:        body.Text,
		IssueID:     issueUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create focus list item")
		return
	}
	writeJSON(w, http.StatusCreated, toItemResponse(item))
}

// Update patches text and/or issue_id on an existing item.
// Body: {"text": "...", "issue_id": "uuid-or-null"}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	itemID, item, authorized := h.loadItem(w, r, userID)
	if !authorized {
		return
	}
	_ = itemID

	var body struct {
		Text    *string `json:"text"`
		IssueID *string `json:"issue_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	var textParam pgtype.Text
	if body.Text != nil {
		textParam = pgtype.Text{String: *body.Text, Valid: true}
	}

	var issueUUID pgtype.UUID
	if body.IssueID != nil && *body.IssueID != "" {
		parsed, err := util.ParseUUID(*body.IssueID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue_id")
			return
		}
		issueUUID = parsed
	}

	updated, err := h.Cerebro.UpdateFocusListItem(r.Context(), cerebrodb.UpdateFocusListItemParams{
		ID:      item.ID,
		Text:    textParam,
		IssueID: issueUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update focus list item")
		return
	}
	writeJSON(w, http.StatusOK, toItemResponse(updated))
}

// MarkDone marks the item as done (sets done_at to NOW()).
func (h *Handler) MarkDone(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	_, item, authorized := h.loadItem(w, r, userID)
	if !authorized {
		return
	}
	updated, err := h.Cerebro.MarkFocusListItemDone(r.Context(), item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark item done")
		return
	}
	writeJSON(w, http.StatusOK, toItemResponse(updated))
}

// Snooze postpones the item until the given timestamp.
// Body: {"until": "2026-06-05T09:00:00+02:00"}
func (h *Handler) Snooze(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	_, item, authorized := h.loadItem(w, r, userID)
	if !authorized {
		return
	}
	var body struct {
		Until string `json:"until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	parsed, err := time.Parse(time.RFC3339, body.Until)
	if err != nil {
		writeError(w, http.StatusBadRequest, "until must be RFC3339")
		return
	}
	if !parsed.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "until must be in the future")
		return
	}
	updated, err := h.Cerebro.SnoozeFocusListItem(r.Context(), cerebrodb.SnoozeFocusListItemParams{
		ID:           item.ID,
		SnoozedUntil: pgtype.Timestamptz{Time: parsed, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snooze focus list item")
		return
	}
	writeJSON(w, http.StatusOK, toItemResponse(updated))
}

// Reorder rewrites the positions of the given items in the order received.
// Body: {"ids": ["uuid1", "uuid2", ...]}
// Each id is verified to belong to the caller before any update lands.
func (h *Handler) Reorder(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if len(body.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	type idAndUUID struct {
		raw  string
		uuid pgtype.UUID
	}
	parsed := make([]idAndUUID, 0, len(body.IDs))
	for _, raw := range body.IDs {
		u, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid item id")
			return
		}
		parsed = append(parsed, idAndUUID{raw: raw, uuid: u})
	}
	// Authorize every id up-front; partial reordering would leave the list in
	// a half-applied state.
	for _, p := range parsed {
		item, err := h.Cerebro.GetFocusListItem(r.Context(), p.uuid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "focus list item not found")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to fetch focus list item")
			}
			return
		}
		if util.UUIDToString(item.MemberID) != util.UUIDToString(userID) {
			writeError(w, http.StatusForbidden, "not your focus list item")
			return
		}
	}
	for idx, p := range parsed {
		if err := h.Cerebro.SetFocusListItemPosition(r.Context(), cerebrodb.SetFocusListItemPositionParams{
			ID:       p.uuid,
			Position: float64(idx + 1),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reorder focus list item")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// Delete removes the item permanently.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	_, item, authorized := h.loadItem(w, r, userID)
	if !authorized {
		return
	}
	if err := h.Cerebro.DeleteFocusListItem(r.Context(), item.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete focus list item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadItem fetches and authorizes a focus list item by URL param "id".
// Returns the UUID, the item, and whether authorization passed.
func (h *Handler) loadItem(w http.ResponseWriter, r *http.Request, userID pgtype.UUID) (pgtype.UUID, cerebrodb.CerebroFocusListItem, bool) {
	itemUUID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return pgtype.UUID{}, cerebrodb.CerebroFocusListItem{}, false
	}
	item, err := h.Cerebro.GetFocusListItem(r.Context(), itemUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "focus list item not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to fetch focus list item")
		}
		return pgtype.UUID{}, cerebrodb.CerebroFocusListItem{}, false
	}
	if util.UUIDToString(item.MemberID) != util.UUIDToString(userID) {
		writeError(w, http.StatusForbidden, "not your focus list item")
		return pgtype.UUID{}, cerebrodb.CerebroFocusListItem{}, false
	}
	return itemUUID, item, true
}

// requireContext extracts and validates workspace ID and user ID from the request.
func requireContext(w http.ResponseWriter, r *http.Request) (wsID pgtype.UUID, userID pgtype.UUID, ok bool) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	wsIDStr := middleware.WorkspaceIDFromContext(r.Context())
	if wsIDStr == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	wsUUID, err := util.ParseUUID(wsIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	userUUID, err := util.ParseUUID(userIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return wsUUID, userUUID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
