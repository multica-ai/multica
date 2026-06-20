// Package reminder holds the cerebro-only HTTP handlers for the unified reminder
// entity (FIR-394). A reminder is "remind [a member OR an agent] about [a
// comment / issue / project / nothing] at [a time]". It links BACK to its anchor
// instead of living inside it, so it can never lock a DM/channel thread
// (FIR-249); agent recipients fire as agent wakeups (see the reminder sweeper).
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

// Handler exposes the cerebro-only reminder endpoints. Upstream resolves the
// anchor entities (comment / issue / project / agent); Cerebro owns the reminder
// table.
type Handler struct {
	Upstream *db.Queries
	Cerebro  *cerebrodb.Queries
}

// New constructs the handler.
func New(upstream *db.Queries, cerebro *cerebrodb.Queries) *Handler {
	return &Handler{Upstream: upstream, Cerebro: cerebro}
}

// reminderResponse is the JSON shape returned to the client. Anchor fields are
// optional: the reminder outlives whatever it points at.
type reminderResponse struct {
	ID                string  `json:"id"`
	CreatorID         string  `json:"creator_id"`
	RecipientType     string  `json:"recipient_type"`
	RecipientID       string  `json:"recipient_id"`
	RemindAt          string  `json:"remind_at"`
	Status            string  `json:"status"`
	Text              string  `json:"text"`
	AnchorType        string  `json:"anchor_type"`
	MessageID         *string `json:"message_id"`
	ConversationID    *string `json:"conversation_id"`
	ConversationKind  *string `json:"conversation_kind"`
	ConversationTitle *string `json:"conversation_title"`
	ProjectID         *string `json:"project_id"`
	ProjectTitle      *string `json:"project_title"`
	ChatMessageID     *string `json:"chat_message_id"`
	ChatSessionID     *string `json:"chat_session_id"`
	ChatSessionTitle  *string `json:"chat_session_title"`
	SourcePreview     *string `json:"source_preview"`
	FiredAt           *string `json:"fired_at"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

// sourcePreview prefers the chat-message body for a chat-message anchor so the
// opened card shows the message you were reminded about; otherwise it falls back
// to the comment body.
func sourcePreview(comment pgtype.Text, chat pgtype.Text) *string {
	if chat.Valid {
		s := chat.String
		return &s
	}
	return optText(comment)
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
		CreatorID:         util.UUIDToString(r.CreatorID),
		RecipientType:     r.RecipientType,
		RecipientID:       util.UUIDToString(r.RecipientID),
		RemindAt:          r.RemindAt.Time.Format(time.RFC3339),
		Status:            r.Status,
		Text:              r.Text,
		AnchorType:        r.AnchorType,
		MessageID:         optUUID(r.MessageID),
		ConversationID:    optUUID(r.ConversationID),
		ConversationKind:  optText(r.ConversationKind),
		ConversationTitle: optText(r.ConversationTitle),
		ProjectID:         optUUID(r.ProjectID),
		ProjectTitle:      optText(r.ProjectTitle),
		ChatMessageID:     optUUID(r.ChatMessageID),
		ChatSessionID:     optUUID(r.ChatSessionID),
		ChatSessionTitle:  optText(r.ChatSessionTitle),
		SourcePreview:     sourcePreview(r.SourceContent, r.ChatSourceContent),
		FiredAt:           optTime(r.FiredAt),
		CreatedAt:         r.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:         r.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func listRowToResponse(r cerebrodb.ListRemindersRow) reminderResponse {
	return reminderResponse{
		ID:                util.UUIDToString(r.ID),
		CreatorID:         util.UUIDToString(r.CreatorID),
		RecipientType:     r.RecipientType,
		RecipientID:       util.UUIDToString(r.RecipientID),
		RemindAt:          r.RemindAt.Time.Format(time.RFC3339),
		Status:            r.Status,
		Text:              r.Text,
		AnchorType:        r.AnchorType,
		MessageID:         optUUID(r.MessageID),
		ConversationID:    optUUID(r.ConversationID),
		ConversationKind:  optText(r.ConversationKind),
		ConversationTitle: optText(r.ConversationTitle),
		ProjectID:         optUUID(r.ProjectID),
		ProjectTitle:      optText(r.ProjectTitle),
		ChatMessageID:     optUUID(r.ChatMessageID),
		ChatSessionID:     optUUID(r.ChatSessionID),
		ChatSessionTitle:  optText(r.ChatSessionTitle),
		SourcePreview:     sourcePreview(r.SourceContent, r.ChatSourceContent),
		FiredAt:           optTime(r.FiredAt),
		CreatedAt:         r.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:         r.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// List returns the authenticated member's reminders — i.e. the ones they are the
// recipient of (excludes done).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	wsID, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	rows, err := h.Cerebro.ListReminders(r.Context(), cerebrodb.ListRemindersParams{
		WorkspaceID:   wsID,
		RecipientType: "member",
		RecipientID:   userID,
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

// createBody is the Create request. Recipient defaults to the caller (a member);
// the anchor is whichever of message_id / issue_id / project_id is set, else a
// free reminder carried entirely by text.
type createBody struct {
	RemindAt      string `json:"remind_at"`
	Text          string `json:"text"`
	RecipientType string `json:"recipient_type"`
	RecipientID   string `json:"recipient_id"`
	MessageID     string `json:"message_id"`
	IssueID       string `json:"issue_id"`
	ProjectID     string `json:"project_id"`
	ChatMessageID string `json:"chat_message_id"`
}

// Create adds a reminder for any recipient, anchored to any (or no) entity.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	wsID, userID, ok := requireContext(w, r)
	if !ok {
		return
	}
	var body createBody
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

	// Resolve the recipient (defaults to the caller as a member).
	recipientType := strings.TrimSpace(body.RecipientType)
	if recipientType == "" {
		recipientType = "member"
	}
	if recipientType != "member" && recipientType != "agent" {
		writeError(w, http.StatusBadRequest, "recipient_type must be 'member' or 'agent'")
		return
	}
	recipientID := userID
	if rid := strings.TrimSpace(body.RecipientID); rid != "" {
		parsed, perr := util.ParseUUID(rid)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid recipient_id")
			return
		}
		recipientID = parsed
	} else if recipientType == "agent" {
		writeError(w, http.StatusBadRequest, "recipient_id is required for an agent recipient")
		return
	}
	if recipientType == "agent" {
		if _, aerr := h.Upstream.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          recipientID,
			WorkspaceID: wsID,
		}); aerr != nil {
			writeError(w, http.StatusNotFound, "agent recipient not found")
			return
		}
	}

	// Resolve the anchor — at most one of message/issue/project/chat may be set.
	anchorType := "none"
	var messageID, conversationID, projectID, chatMessageID pgtype.UUID
	text := strings.TrimSpace(body.Text)

	switch {
	case strings.TrimSpace(body.ChatMessageID) != "":
		anchorType = "chat_message"
		cmID, cmerr := util.ParseUUID(strings.TrimSpace(body.ChatMessageID))
		if cmerr != nil {
			writeError(w, http.StatusBadRequest, "invalid chat_message_id")
			return
		}
		chatMsg, gerr := h.Cerebro.GetChatMessageWithWorkspace(r.Context(), cerebrodb.GetChatMessageWithWorkspaceParams{
			ID:          cmID,
			WorkspaceID: wsID,
		})
		if gerr != nil {
			writeError(w, http.StatusNotFound, "chat message not found")
			return
		}
		chatMessageID = cmID
		if text == "" {
			text = suggestReminderText(chatMsg.Content)
		}
	case strings.TrimSpace(body.MessageID) != "":
		anchorType = "comment"
		mID, merr := util.ParseUUID(strings.TrimSpace(body.MessageID))
		if merr != nil {
			writeError(w, http.StatusBadRequest, "invalid message_id")
			return
		}
		comment, cerr := h.Upstream.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          mID,
			WorkspaceID: wsID,
		})
		if cerr != nil {
			writeError(w, http.StatusNotFound, "message not found")
			return
		}
		messageID = mID
		conversationID = comment.IssueID
		if text == "" {
			text = suggestReminderText(comment.Content)
		}
	case strings.TrimSpace(body.IssueID) != "":
		anchorType = "issue"
		iID, ierr := util.ParseUUID(strings.TrimSpace(body.IssueID))
		if ierr != nil {
			writeError(w, http.StatusBadRequest, "invalid issue_id")
			return
		}
		issue, gerr := h.Upstream.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          iID,
			WorkspaceID: wsID,
		})
		if gerr != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		conversationID = issue.ID
	case strings.TrimSpace(body.ProjectID) != "":
		anchorType = "project"
		pID, perr := util.ParseUUID(strings.TrimSpace(body.ProjectID))
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid project_id")
			return
		}
		project, gerr := h.Upstream.GetProject(r.Context(), pID)
		if gerr != nil || util.UUIDToString(project.WorkspaceID) != util.UUIDToString(wsID) {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		projectID = pID
	default:
		// Free reminder: text is the whole payload, so it must be present.
		if text == "" {
			writeError(w, http.StatusBadRequest, "a reminder with no anchor needs text")
			return
		}
	}

	// An agent recipient fires as a wakeup, which needs an issue context. Only a
	// comment- or issue-anchored reminder has one (v1 limitation).
	if recipientType == "agent" && !conversationID.Valid {
		writeError(w, http.StatusBadRequest, "an agent reminder must be anchored to a message or an issue")
		return
	}

	if text == "" {
		text = "Reminder"
	}

	id, err := h.Cerebro.CreateReminder(r.Context(), cerebrodb.CreateReminderParams{
		WorkspaceID:    wsID,
		UserID:         userID,
		RecipientType:  recipientType,
		RecipientID:    recipientID,
		RemindAt:       pgtype.Timestamptz{Time: remindAt, Valid: true},
		Text:           text,
		AnchorType:     anchorType,
		MessageID:      messageID,
		ConversationID: conversationID,
		ProjectID:      projectID,
		ChatMessageID:  chatMessageID,
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

// loadReminder fetches a reminder by URL param "id" and authorizes the caller.
// A member may manage a reminder if they are its recipient or its creator.
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
	caller := util.UUIDToString(userID)
	isRecipient := row.RecipientType == "member" && util.UUIDToString(row.RecipientID) == caller
	isCreator := util.UUIDToString(row.CreatorID) == caller
	if !isRecipient && !isCreator {
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
