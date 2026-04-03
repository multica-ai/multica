package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type InboxItemResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	RecipientType string          `json:"recipient_type"`
	RecipientID   string          `json:"recipient_id"`
	Type          string          `json:"type"`
	Severity      string          `json:"severity"`
	IssueID       *string         `json:"issue_id"`
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
	var details json.RawMessage
	if i.Details.Valid {
		details = json.RawMessage(i.Details.String)
	}
	return InboxItemResponse{
		ID:            i.ID,
		WorkspaceID:   i.WorkspaceID,
		RecipientType: i.RecipientType,
		RecipientID:   i.RecipientID,
		Type:          i.Type,
		Severity:      i.Severity,
		IssueID:       nullStringToPtr(i.IssueID),
		Title:         i.Title,
		Body:          nullStringToPtr(i.Body),
		Read:          i.Read != 0,
		Archived:      i.Archived != 0,
		CreatedAt:     i.CreatedAt,
		ActorType:     nullStringToPtr(i.ActorType),
		ActorID:       nullStringToPtr(i.ActorID),
		Details:       details,
	}
}

func inboxRowToResponse(r db.ListInboxItemsRow) InboxItemResponse {
	var details json.RawMessage
	if r.Details.Valid {
		details = json.RawMessage(r.Details.String)
	}
	return InboxItemResponse{
		ID:            r.ID,
		WorkspaceID:   r.WorkspaceID,
		RecipientType: r.RecipientType,
		RecipientID:   r.RecipientID,
		Type:          r.Type,
		Severity:      r.Severity,
		IssueID:       nullStringToPtr(r.IssueID),
		Title:         r.Title,
		Body:          nullStringToPtr(r.Body),
		Read:          r.Read != 0,
		Archived:      r.Archived != 0,
		CreatedAt:     r.CreatedAt,
		IssueStatus:   nullStringToPtr(r.IssueStatus),
		ActorType:     nullStringToPtr(r.ActorType),
		ActorID:       nullStringToPtr(r.ActorID),
		Details:       details,
	}
}

func (h *Handler) enrichInboxResponse(ctx context.Context, resp InboxItemResponse, issueID string) InboxItemResponse {
	if issueID == "" {
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	items, err := h.Queries.ListInboxItems(r.Context(), db.ListInboxItemsParams{
		WorkspaceID:   workspaceID,
		RecipientType: "member",
		RecipientID:   userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list inbox")
		return
	}

	resp := make([]InboxItemResponse, len(items))
	for i, item := range items {
		resp[i] = inboxRowToResponse(item)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) MarkInboxRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.loadInboxItemForUser(w, r, id); !ok {
		return
	}
	item, err := h.Queries.MarkInboxRead(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}

	userID := requestUserID(r)
	workspaceID := item.WorkspaceID
	h.publish(protocol.EventInboxRead, workspaceID, "member", userID, map[string]any{
		"item_id":      item.ID,
		"recipient_id": item.RecipientID,
	})

	issueIDStr := ""
	if item.IssueID.Valid {
		issueIDStr = item.IssueID.String
	}
	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), issueIDStr)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ArchiveInboxItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.loadInboxItemForUser(w, r, id); !ok {
		return
	}
	item, err := h.Queries.ArchiveInboxItem(r.Context(), id)
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
	workspaceID := item.WorkspaceID
	h.publish(protocol.EventInboxArchived, workspaceID, "member", userID, map[string]any{
		"item_id":      item.ID,
		"issue_id":     nullStringToPtr(item.IssueID),
		"recipient_id": item.RecipientID,
	})

	issueIDStr2 := ""
	if item.IssueID.Valid {
		issueIDStr2 = item.IssueID.String
	}
	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), issueIDStr2)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CountUnreadInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.CountUnreadInbox(r.Context(), db.CountUnreadInboxParams{
		WorkspaceID:   workspaceID,
		RecipientType: "member",
		RecipientID:   userID,
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.MarkAllInboxRead(r.Context(), db.MarkAllInboxReadParams{
		WorkspaceID: workspaceID,
		RecipientID: userID,
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.ArchiveAllInbox(r.Context(), db.ArchiveAllInboxParams{
		WorkspaceID: workspaceID,
		RecipientID: userID,
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.ArchiveAllReadInbox(r.Context(), db.ArchiveAllReadInboxParams{
		WorkspaceID: workspaceID,
		RecipientID: userID,
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.ArchiveCompletedInbox(r.Context(), db.ArchiveCompletedInboxParams{
		WorkspaceID: workspaceID,
		RecipientID: userID,
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
