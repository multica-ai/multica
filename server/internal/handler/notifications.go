package handler

// CEREBRO-PATCH(notifications-handler): cerebro modification of upstream file

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

const (
	defaultNotificationsLimit = 50
	maxNotificationsLimit     = 50
)

type NotificationsListResponse struct {
	Items      []InboxItemResponse `json:"items"`
	NextCursor *string             `json:"next_cursor"`
}

func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.resolveMemberFromRequest(r)
	if !ok {
		writeError(w, http.StatusForbidden, "workspace membership required")
		return
	}
	limit, cursor, ok := parseNotificationsPageParams(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.ListNotificationsItems(r.Context(), db.ListNotificationsItemsParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}

	items := make([]InboxItemResponse, 0, limit)
	var nextCursor *string
	var lastCursor string
	for _, item := range rows {
		if cursor != nil && !item.CreatedAt.Time.Before(*cursor) {
			continue
		}
		if !h.canReadNotificationItem(r.Context(), member, item.IssueID) {
			continue
		}
		if len(items) == limit {
			nextCursor = &lastCursor
			break
		}
		items = append(items, notificationRowToResponse(item))
		lastCursor = timestampToString(item.CreatedAt)
	}

	writeJSON(w, http.StatusOK, NotificationsListResponse{Items: items, NextCursor: nextCursor})
}

func (h *Handler) ListInboxNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListNotificationsItems(r.Context(), db.ListNotificationsItemsParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}

	resp := make([]InboxItemResponse, len(rows))
	for i, item := range rows {
		resp[i] = notificationRowToResponse(item)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CountUnreadNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.resolveMemberFromRequest(r)
	if !ok {
		writeError(w, http.StatusForbidden, "workspace membership required")
		return
	}

	rows, err := h.Queries.ListNotificationsItems(r.Context(), db.ListNotificationsItemsParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread notifications")
		return
	}

	var count int64
	now := time.Now()
	for _, item := range rows {
		if item.Read {
			continue
		}
		if item.MutedUntil.Valid && item.MutedUntil.Time.After(now) {
			continue
		}
		if !h.canReadNotificationItem(r.Context(), member, item.IssueID) {
			continue
		}
		count++
	}

	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

func (h *Handler) MarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, ok := h.loadNotificationItemForUser(w, r, id)
	if !ok {
		return
	}
	item, err := h.Queries.MarkInboxRead(r.Context(), item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark notification read")
		return
	}

	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxRead, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"recipient_id": uuidToString(item.RecipientID),
		"route":        "notifications",
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.resolveMemberFromRequest(r)
	if !ok {
		writeError(w, http.StatusForbidden, "workspace membership required")
		return
	}

	rows, err := h.Queries.ListNotificationsItems(r.Context(), db.ListNotificationsItemsParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark all notifications read")
		return
	}

	var count int64
	for _, item := range rows {
		if item.Read {
			continue
		}
		if !h.canReadNotificationItem(r.Context(), member, item.IssueID) {
			continue
		}
		if _, err := h.Queries.MarkInboxRead(r.Context(), item.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to mark all notifications read")
			return
		}
		count++
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

func parseNotificationsPageParams(w http.ResponseWriter, r *http.Request) (int, *time.Time, bool) {
	limit := defaultNotificationsLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return 0, nil, false
		}
		limit = parsed
	}
	if limit > maxNotificationsLimit {
		limit = maxNotificationsLimit
	}

	var cursor *time.Time
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return 0, nil, false
		}
		cursor = &parsed
	}

	return limit, cursor, true
}

func notificationRowToResponse(item db.ListNotificationsItemsRow) InboxItemResponse {
	return InboxItemResponse{
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
		MutedUntil:    timestampToPtr(item.MutedUntil), // CEREBRO-PATCH(cerebro-inbox-fields)
		CreatedAt:     timestampToString(item.CreatedAt),
		IssueStatus:   textToPtr(item.IssueStatus),
		ActorType:     textToPtr(item.ActorType),
		ActorID:       uuidToPtr(item.ActorID),
		Details:       json.RawMessage(item.Details),
	}
}

func (h *Handler) loadNotificationItemForUser(w http.ResponseWriter, r *http.Request, itemID string) (db.InboxItem, bool) {
	item, ok := h.loadInboxItemForUser(w, r, itemID)
	if !ok {
		return db.InboxItem{}, false
	}
	if item.Route != "notifications" {
		writeError(w, http.StatusNotFound, "notification not found")
		return db.InboxItem{}, false
	}
	member, ok := h.resolveMemberFromRequest(r)
	if !ok {
		writeError(w, http.StatusForbidden, "workspace membership required")
		return db.InboxItem{}, false
	}
	if !h.canReadNotificationItem(r.Context(), member, item.IssueID) {
		writeError(w, http.StatusNotFound, "notification not found")
		return db.InboxItem{}, false
	}
	return item, true
}

func (h *Handler) canReadNotificationItem(ctx context.Context, member db.Member, issueID pgtype.UUID) bool {
	if !issueID.Valid {
		return true
	}
	issue, err := h.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return false
	}
	return h.canAccessIssue(ctx, member, issue)
}
