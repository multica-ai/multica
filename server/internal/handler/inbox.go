package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// InboxItemResponse is the JSON shape returned for an inbox_item row.
//
// CEREBRO-PATCH(cerebro-inbox-fields): Route and ProjectID are cerebro-only
// inbox_item columns added by cerebro DB migrations; the rest of this struct
// is upstream.
type InboxItemResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	RecipientType string          `json:"recipient_type"`
	RecipientID   string          `json:"recipient_id"`
	Type          string          `json:"type"`
	Severity      string          `json:"severity"`
	Route         string          `json:"route"`
	IssueID       *string         `json:"issue_id"`
	ProjectID     *string         `json:"project_id"`
	Title         string          `json:"title"`
	Body          *string         `json:"body"`
	Read          bool            `json:"read"`
	Archived      bool            `json:"archived"`
	CreatedAt     string          `json:"created_at"`
	IssueStatus   *string         `json:"issue_status"`
	ActorType     *string         `json:"actor_type"`
	ActorID       *string         `json:"actor_id"`
	Details       json.RawMessage `json:"details"`
}

func inboxToResponse(i db.InboxItem) InboxItemResponse {
	return InboxItemResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		RecipientType: i.RecipientType,
		RecipientID:   uuidToString(i.RecipientID),
		Type:          i.Type,
		Severity:      i.Severity,
		Route:         i.Route, // CEREBRO-PATCH(cerebro-inbox-fields)
		IssueID:       uuidToPtr(i.IssueID),
		Title:         i.Title,
		Body:          textToPtr(i.Body),
		Read:          i.Read,
		Archived:      i.Archived,
		CreatedAt:     timestampToString(i.CreatedAt),
		ActorType:     textToPtr(i.ActorType),
		ActorID:       uuidToPtr(i.ActorID),
		Details:       json.RawMessage(i.Details),
	}
}

func inboxRowToResponse(r db.ListInboxItemsRow) InboxItemResponse {
	return InboxItemResponse{
		ID:            uuidToString(r.ID),
		WorkspaceID:   uuidToString(r.WorkspaceID),
		RecipientType: r.RecipientType,
		RecipientID:   uuidToString(r.RecipientID),
		Type:          r.Type,
		Severity:      r.Severity,
		Route:         r.Route, // CEREBRO-PATCH(cerebro-inbox-fields)
		IssueID:       uuidToPtr(r.IssueID),
		Title:         r.Title,
		Body:          textToPtr(r.Body),
		Read:          r.Read,
		Archived:      r.Archived,
		CreatedAt:     timestampToString(r.CreatedAt),
		IssueStatus:   textToPtr(r.IssueStatus),
		ActorType:     textToPtr(r.ActorType),
		ActorID:       uuidToPtr(r.ActorID),
		Details:       json.RawMessage(r.Details),
	}
}

func (h *Handler) enrichInboxResponse(ctx context.Context, resp InboxItemResponse, issueID pgtype.UUID) InboxItemResponse {
	if !issueID.Valid {
		return resp
	}
	issue, err := h.Queries.GetIssue(ctx, issueID)
	if err == nil {
		s := issue.Status
		resp.IssueStatus = &s
	}
	return resp
}

func (h *Handler) ListInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	// CEREBRO-PATCH(cerebro-inbox-folders): folder branch uses cerebro-only
	// queries (folders feature). Upstream removed inbox folders in JEH-650
	// but cerebro-inbox keeps them.
	folderID := r.URL.Query().Get("folder")
	if folderID != "" {
		rows, err := h.Queries.ListInboxItemsInFolder(r.Context(), db.ListInboxItemsInFolderParams{
			ID:          parseUUID(folderID),
			WorkspaceID: wsUUID,
			UserID:      parseUUID(userID),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list inbox")
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
		return
	}

	if r.URL.Query().Get("archived") == "1" {
		archived, err := h.Queries.ListArchivedInboxFeed(r.Context(), db.ListArchivedInboxFeedParams{
			WorkspaceID:   wsUUID,
			RecipientType: "member",
			RecipientID:   parseUUID(userID),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list inbox")
			return
		}
		resp := make([]InboxItemResponse, len(archived))
		for i, item := range archived {
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
		return
	}

	items, err := h.Queries.ListInboxFeed(r.Context(), db.ListInboxFeedParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list inbox")
		return
	}

	resp := make([]InboxItemResponse, len(items))
	for i, item := range items {
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

func (h *Handler) MarkInboxRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prev, ok := h.loadInboxItemForUser(w, r, id)
	if !ok {
		return
	}
	item, err := h.Queries.MarkInboxRead(r.Context(), prev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}

	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxRead, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"recipient_id": uuidToString(item.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ArchiveInboxItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prev, ok := h.loadInboxItemForUser(w, r, id)
	if !ok {
		return
	}
	item, err := h.Queries.ArchiveInboxItem(r.Context(), prev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive")
		return
	}

	// Archive all sibling inbox items for the same issue (issue-level archive)
	if item.IssueID.Valid {
		h.Queries.ArchiveInboxByIssue(r.Context(), db.ArchiveInboxByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
		})
	}

	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxArchived, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"issue_id":     uuidToPtr(item.IssueID),
		"recipient_id": uuidToString(item.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, resp)
}

// CountUnreadInboxTotal returns the user's total unread inbox count across
// every workspace they belong to. Used by the PWA service worker / web app to
// drive the OS app-icon badge, which is single-icon and cannot reflect
// workspace scope. Lives outside the workspace-scoped routes so it is callable
// without an X-Workspace-Slug header.
func (h *Handler) CountUnreadInboxTotal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	count, err := h.Queries.CountUnreadInboxForUserAllWorkspaces(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread inbox")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

func (h *Handler) CountUnreadInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	count, err := h.Queries.CountUnreadInbox(r.Context(), db.CountUnreadInboxParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread inbox")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

func (h *Handler) MarkAllInboxRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	count, err := h.Queries.MarkAllInboxRead(r.Context(), db.MarkAllInboxReadParams{
		WorkspaceID: wsUUID,
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark all inbox read")
		return
	}

	slog.Info("inbox: mark all read", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchRead, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) ArchiveAllInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	count, err := h.Queries.ArchiveAllInbox(r.Context(), db.ArchiveAllInboxParams{
		WorkspaceID: wsUUID,
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive all inbox")
		return
	}

	slog.Info("inbox: archive all", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchArchived, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) ArchiveAllReadInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	count, err := h.Queries.ArchiveAllReadInbox(r.Context(), db.ArchiveAllReadInboxParams{
		WorkspaceID: wsUUID,
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive all read inbox")
		return
	}

	slog.Info("inbox: archive all read", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchArchived, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) ArchiveCompletedInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	count, err := h.Queries.ArchiveCompletedInbox(r.Context(), db.ArchiveCompletedInboxParams{
		WorkspaceID: wsUUID,
		RecipientID: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive completed inbox")
		return
	}

	slog.Info("inbox: archive completed", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchArchived, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

// ListActiveIssueTasks moved to server/internal/cerebro/notifications/handler.go
// (cerebro-only handler — wired in router.go via cerebroNotificationsHandler).
