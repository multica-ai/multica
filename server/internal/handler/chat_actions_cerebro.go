// CEREBRO-PATCH(chat-actions-cerebro): JEH-799 chat session header actions
// (rename / archive / convert-to-issue). Net-new fork file alongside chat.go;
// the upstream chat.go is unmodified by this patch.
package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// UpdateChatSessionRequest carries the fields the chat-session header can
// edit. Only the fields the user actually changed are sent; nil means leave
// the existing value alone.
type UpdateChatSessionRequest struct {
	Title  *string `json:"title,omitempty"`
	Status *string `json:"status,omitempty"`
}

// UpdateChatSession applies a partial update to a chat session — title rename
// from the inline header, or status change between 'active' and 'archived'.
// At least one field must be provided. Empty title is rejected so the dropdown
// trigger never falls back to the "New chat" placeholder for a session that
// has actually been used.
func (h *Handler) UpdateChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req UpdateChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == nil && req.Status == nil {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	if req.Status != nil && *req.Status != "active" && *req.Status != "archived" {
		writeError(w, http.StatusBadRequest, "status must be 'active' or 'archived'")
		return
	}

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	updated := session
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		next, err := h.Queries.UpdateChatSessionTitle(r.Context(), db.UpdateChatSessionTitleParams{
			ID:    session.ID,
			Title: title,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update chat session title")
			return
		}
		updated = next
	}
	if req.Status != nil && *req.Status != updated.Status {
		next, err := h.Queries.UpdateChatSessionStatus(r.Context(), db.UpdateChatSessionStatusParams{
			ID:     session.ID,
			Status: *req.Status,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update chat session status")
			return
		}
		updated = next
	}

	resolvedSessionID := uuidToString(updated.ID)
	h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionUpdatedPayload{
		ChatSessionID: resolvedSessionID,
		Title:         updated.Title,
		Status:        updated.Status,
	})

	writeJSON(w, http.StatusOK, chatSessionToResponse(updated))
}

// ConvertChatSessionToIssueResponse is the result of converting a chat session
// into a full issue — the caller uses identifier+id to navigate to the new
// issue card. The chat session is left intact (status stays as-is) so the
// user keeps the conversation history alongside the issue.
type ConvertChatSessionToIssueResponse struct {
	IssueID    string `json:"issue_id"`
	Identifier string `json:"identifier"`
	Number     int32  `json:"number"`
}

// ConvertChatSessionToIssue creates a new issue seeded with the chat session
// title and a markdown-formatted transcript of the conversation. The chat
// session itself is unchanged — both views remain available, and the user
// can manually archive the chat afterwards if desired.
//
// The new issue is created by the same user as the chat creator (the request
// author, after ownership is verified by loadChatSessionForUser).
func (h *Handler) ConvertChatSessionToIssue(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	messages, err := h.Queries.ListChatMessages(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chat messages")
		return
	}

	title := strings.TrimSpace(session.Title)
	if title == "" {
		// First user message is the natural title fallback for sessions that
		// were never renamed.
		for _, m := range messages {
			if m.Role == "user" {
				title = firstLine(m.Content, 80)
				break
			}
		}
	}
	if title == "" {
		title = "Chat session"
	}
	description := buildChatTranscriptMarkdown(messages)

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("increment issue counter failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	creatorType, actualCreatorID := h.resolveActor(r, userID, workspaceID)
	descPgText := pgtype.Text{}
	if description != "" {
		descPgText = pgtype.Text{String: description, Valid: true}
	}

	issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID: wsUUID,
		Title:       title,
		Description: descPgText,
		Status:      "todo",
		Priority:    "none",
		CreatorType: creatorType,
		CreatorID:   parseUUID(actualCreatorID),
		Position:    0,
		Number:      issueNumber,
	})
	if err != nil {
		slog.Warn("create issue from chat failed", "error", err, "session_id", sessionID)
		writeError(w, http.StatusInternalServerError, "failed to create issue: "+err.Error())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)

	h.publishToAudience(protocol.EventIssueCreated, workspaceID, creatorType, actualCreatorID, map[string]any{"issue": resp}, h.audienceForIssue(r.Context(), issue))

	writeJSON(w, http.StatusCreated, ConvertChatSessionToIssueResponse{
		IssueID:    uuidToString(issue.ID),
		Identifier: resp.Identifier,
		Number:     issue.Number,
	})
}

// buildChatTranscriptMarkdown turns a chat-message slice into a markdown
// transcript suitable for an issue description. Roles are bolded so the
// reader can scan; failure-marker assistant rows are omitted because they
// carry server-synthesized failure copy that would be noise as issue context.
func buildChatTranscriptMarkdown(messages []db.ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("_Converted from a chat session._\n\n")
	for _, m := range messages {
		if m.FailureReason.Valid {
			continue
		}
		role := strings.ToTitle(m.Role[:1]) + m.Role[1:]
		fmt.Fprintf(&b, "**%s:**\n\n%s\n\n", role, strings.TrimSpace(m.Content))
	}
	return strings.TrimRight(b.String(), "\n")
}

// firstLine returns the first non-empty line of `s`, truncated to `max` chars
// (with an ellipsis if truncated). Used as a title fallback for sessions that
// were never renamed.
func firstLine(s string, max int) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > max {
			return line[:max] + "…"
		}
		return line
	}
	return ""
}
