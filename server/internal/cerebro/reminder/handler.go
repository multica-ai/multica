// Package reminder holds the cerebro-only HTTP handlers for reminders modelled
// as their own entity (FIR-394). A reminder links BACK to the message and
// conversation it was set on instead of living inside the conversation, so it
// can never again lock a DM/channel thread (FIR-249).
package reminder

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler exposes the cerebro-only reminder endpoints. Upstream is used to
// resolve the source comment (to fix the conversation and auto-suggest text);
// Cerebro owns the reminder table.
type Handler struct {
	Upstream *db.Queries
	Cerebro  *cerebrodb.Queries
}

// New constructs the handler.
func New(upstream *db.Queries, cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Upstream: upstream, Cerebro: cerebro}
}

// reminderResponse is the JSON shape returned to the client. Source fields are
// optional: the reminder outlives the message it points at.
type reminderResponse struct {
	ID                string  `json:"id"`
	RemindAt          string  `json:"remind_at"`
	Status            string  `json:"status"`
	Text              string  `json:"text"`
	MessageID         *string `json:"message_id"`
	ConversationID    *string `json:"conversation_id"`
	ConversationKind  *string `json:"conversation_kind"`
	ConversationTitle *string `json:"conversation_title"`
	SourcePreview     *string `json:"source_preview"`
	FiredAt           *string `json:"fired_at"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

func optUUID(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := util.UUIDToString(u)
	return &s
}

func optText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

func optTime(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.Format(time.RFC3339)
	return &s
}

func getRowToResponse(r cerebrodb.GetReminderRow) reminderResponse {
	return reminderResponse{
		ID:                util.UUIDToString(r.ID),
		RemindAt:          r.RemindAt.Time.Format(time.RFC3339),
		Status:            r.Status,
		Text:              r.Text,
		MessageID:         optUUID(r.MessageID),
		ConversationID:    optUUID(r.ConversationID),
		ConversationKind:  optText(r.ConversationKind),
		ConversationTitle: optText(r.ConversationTitle),
		SourcePreview:     optText(r.SourceContent),
		FiredAt:           optTime(r.FiredAt),
		CreatedAt:         r.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:         r.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func listRowToResponse(r cerebrodb.ListRemindersRow) reminderResponse {
	return reminderResponse{
		ID:                util.UUIDToString(r.ID),
		RemindAt:          r.RemindAt.Time.Format(time.RFC3339),
		Status:            r.Status,
		Text:              r.Text,
		MessageID:         optUUID(r.MessageID),
		ConversationID:    optUUID(r.ConversationID),
		ConversationKind:  optText(r.ConversationKind),
		ConversationTitle: optText(r.ConversationTitle),
		SourcePreview:     optText(r.SourceContent),
		FiredAt:           optTime(r.FiredAt),
		CreatedAt:         r.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:         r.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// List returns the authenticated member's reminders (excludes done).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	wsID, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListReminders(r.Context(), cerebrodb.ListRemindersParams{
		WorkspaceID: wsID,
		UserID:      userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list reminders")
		return
	}
	resp := make([]reminderResponse, len(rows))
	for i, row := range rows {
		resp[i] = listRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// Create adds a reminder anchored to a message.
// Body: {"message_id": "uuid", "remind_at": "RFC3339", "text": "...optional..."}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	wsID, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	var body struct {
		MessageID string `json:"message_id"`
		RemindAt  string `json:"remind_at"`
		Text      string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	remindAt, err := time.Parse(time.RFC3339, body.RemindAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "remind_at must be RFC3339")
		return
	}
	if !remindAt.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "remind_at must be in the future")
		return
	}
	messageUUID, err := util.ParseUUID(strings.TrimSpace(body.MessageID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid message_id")
		return
	}
	// Resolve the source comment in this workspace: it fixes the conversation the
	// reminder belongs to and supplies the default reminder text.
	comment, err := h.Upstream.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          messageUUID,
		WorkspaceID: wsID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		text = suggestReminderText(comment.Content)
	}
	if text == "" {
		text = "Reminder"
	}

	id, err := h.Cerebro.CreateReminder(r.Context(), cerebrodb.CreateReminderParams{
		WorkspaceID:    wsID,
		UserID:         userID,
		RemindAt:       pgtype.Timestamptz{Time: remindAt, Valid: true},
		Text:           text,
		MessageID:      messageUUID,
		ConversationID: comment.IssueID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create reminder")
		return
	}
	row, err := h.Cerebro.GetReminder(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load reminder")
		return
	}
	writeJSON(w, http.StatusCreated, getRowToResponse(row))
}

// Get returns a single reminder (the opened reminder card).
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	row, ok := h.loadReminder(w, r, userID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, getRowToResponse(row))
}

// Snooze reschedules a reminder ("Udsæt"). Body: {"until": "RFC3339"}
func (h *Handler) Snooze(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	row, ok := h.loadReminder(w, r, userID)
	if !ok {
		return
	}
	var body struct {
		Until string `json:"until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	until, err := time.Parse(time.RFC3339, body.Until)
	if err != nil {
		writeError(w, http.StatusBadRequest, "until must be RFC3339")
		return
	}
	if !until.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "until must be in the future")
		return
	}
	if _, err := h.Cerebro.SnoozeReminder(r.Context(), cerebrodb.SnoozeReminderParams{
		ID:       row.ID,
		RemindAt: pgtype.Timestamptz{Time: until, Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snooze reminder")
		return
	}
	updated, err := h.Cerebro.GetReminder(r.Context(), row.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load reminder")
		return
	}
	writeJSON(w, http.StatusOK, getRowToResponse(updated))
}

// MarkDone dismisses a reminder ("Færdig").
func (h *Handler) MarkDone(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	row, ok := h.loadReminder(w, r, userID)
	if !ok {
		return
	}
	if _, err := h.Cerebro.MarkReminderDone(r.Context(), row.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark reminder done")
		return
	}
	updated, err := h.Cerebro.GetReminder(r.Context(), row.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load reminder")
		return
	}
	writeJSON(w, http.StatusOK, getRowToResponse(updated))
}

// Delete removes a reminder permanently.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	row, ok := h.loadReminder(w, r, userID)
	if !ok {
		return
	}
	if err := h.Cerebro.DeleteReminder(r.Context(), row.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete reminder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadReminder fetches a reminder by URL param "id" and authorizes ownership.
func (h *Handler) loadReminder(w http.ResponseWriter, r *http.Request, userID pgtype.UUID) (cerebrodb.GetReminderRow, bool) {
	id, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid reminder id")
		return cerebrodb.GetReminderRow{}, false
	}
	row, err := h.Cerebro.GetReminder(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "reminder not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to fetch reminder")
		}
		return cerebrodb.GetReminderRow{}, false
	}
	if util.UUIDToString(row.UserID) != util.UUIDToString(userID) {
		writeError(w, http.StatusForbidden, "not your reminder")
		return cerebrodb.GetReminderRow{}, false
	}
	return row, true
}

// requireContext extracts and validates workspace ID and user ID.
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

// suggestReminderText derives a short default reminder label from the source
// message body — first non-empty line, trimmed to a sensible length.
func suggestReminderText(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 120 {
			return strings.TrimSpace(string(runes[:120])) + "…"
		}
		return line
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
