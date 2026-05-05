package handler

// CEREBRO-PATCH(notifications-handler): cerebro modification of upstream file

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork-specific: notifications page endpoints. Items in inbox_item with
// route='notifications' show up on a dedicated, less-intrusive page anchored
// in the bottom of the sidebar — separate from the action_required inbox.
//
// Single-item operations (mark-read, archive) reuse the inbox handlers since
// the route flag is set at insert time and doesn't affect per-item edits.

func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	rows, err := h.Queries.ListNotificationsItems(r.Context(), db.ListNotificationsItemsParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}

	resp := make([]InboxItemResponse, len(rows))
	for i, item := range rows {
		resp[i] = InboxItemResponse{
			ID:            uuidToString(item.ID),
			WorkspaceID:   uuidToString(item.WorkspaceID),
			RecipientType: item.RecipientType,
			RecipientID:   uuidToString(item.RecipientID),
			Type:          item.Type,
			Severity:      item.Severity,
			Route:         item.Route,
			IssueID:       uuidToPtr(item.IssueID),
			ProjectID:     uuidToPtr(item.ProjectID),
			Title:         item.Title,
			Body:          textToPtr(item.Body),
			Read:          item.Read,
			Archived:      item.Archived,
			CreatedAt:     timestampToString(item.CreatedAt),
			IssueStatus:   textToPtr(item.IssueStatus),
			ActorType:     textToPtr(item.ActorType),
			ActorID:       uuidToPtr(item.ActorID),
			Details:       json.RawMessage(item.Details),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CountUnreadNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	count, err := h.Queries.CountUnreadNotifications(r.Context(), db.CountUnreadNotificationsParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread notifications")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

func (h *Handler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	count, err := h.Queries.MarkAllNotificationsRead(r.Context(), db.MarkAllNotificationsReadParams{
		WorkspaceID: parseUUID(workspaceID),
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark all notifications read")
		return
	}

	slog.Info("notifications: mark all read", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchRead, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
		"route":        "notifications",
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) ArchiveAllNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	count, err := h.Queries.ArchiveAllNotifications(r.Context(), db.ArchiveAllNotificationsParams{
		WorkspaceID: parseUUID(workspaceID),
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive all notifications")
		return
	}

	slog.Info("notifications: archive all", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchArchived, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
		"route":        "notifications",
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}
