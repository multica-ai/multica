package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	TriageStatus  string          `json:"triage_status"`
	SnoozedUntil  *string         `json:"snoozed_until"`
	HandledAt     *string         `json:"handled_at"`
	DismissedAt   *string         `json:"dismissed_at"`
	TriagedBy     *string         `json:"triaged_by"`
}

func inboxToResponse(i db.InboxItem) InboxItemResponse {
	return InboxItemResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		RecipientType: i.RecipientType,
		RecipientID:   uuidToString(i.RecipientID),
		Type:          i.Type,
		Severity:      i.Severity,
		IssueID:       uuidToPtr(i.IssueID),
		Title:         i.Title,
		Body:          textToPtr(i.Body),
		Read:          i.Read,
		Archived:      i.Archived,
		CreatedAt:     timestampToString(i.CreatedAt),
		ActorType:     textToPtr(i.ActorType),
		ActorID:       uuidToPtr(i.ActorID),
		Details:       json.RawMessage(i.Details),
		TriageStatus:  i.TriageStatus,
		SnoozedUntil:  timestampToPtr(i.SnoozedUntil),
		HandledAt:     timestampToPtr(i.HandledAt),
		DismissedAt:   timestampToPtr(i.DismissedAt),
		TriagedBy:     uuidToPtr(i.TriagedBy),
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
		TriageStatus:  r.TriageStatus,
		SnoozedUntil:  timestampToPtr(r.SnoozedUntil),
		HandledAt:     timestampToPtr(r.HandledAt),
		DismissedAt:   timestampToPtr(r.DismissedAt),
		TriagedBy:     uuidToPtr(r.TriagedBy),
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	items, err := h.Queries.ListInboxItems(r.Context(), db.ListInboxItemsParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
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
	item, err := h.Queries.MarkInboxRead(r.Context(), parseUUID(id))
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
	if _, ok := h.loadInboxItemForUser(w, r, id); !ok {
		return
	}
	item, err := h.Queries.ArchiveInboxItem(r.Context(), parseUUID(id))
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

func (h *Handler) HandleInboxItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, ok := h.loadInboxItemForUser(w, r, id)
	if !ok {
		return
	}
	userID := requestUserID(r)

	handled, err := h.Queries.HandleInboxItem(r.Context(), db.HandleInboxItemParams{
		ID:        item.ID,
		TriagedBy: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to handle inbox item")
		return
	}

	if handled.IssueID.Valid {
		_, _ = h.Queries.HandleInboxByIssue(r.Context(), db.HandleInboxByIssueParams{
			WorkspaceID:   handled.WorkspaceID,
			RecipientType: handled.RecipientType,
			RecipientID:   handled.RecipientID,
			TriagedBy:     parseUUID(userID),
			IssueID:       handled.IssueID,
		})
	}

	workspaceID := uuidToString(handled.WorkspaceID)
	h.publish(protocol.EventInboxHandled, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(handled.ID),
		"issue_id":     uuidToPtr(handled.IssueID),
		"recipient_id": uuidToString(handled.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(handled), handled.IssueID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DismissInboxItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, ok := h.loadInboxItemForUser(w, r, id)
	if !ok {
		return
	}
	userID := requestUserID(r)

	dismissed, err := h.Queries.DismissInboxItem(r.Context(), db.DismissInboxItemParams{
		ID:        item.ID,
		TriagedBy: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to dismiss inbox item")
		return
	}

	workspaceID := uuidToString(dismissed.WorkspaceID)
	h.publish(protocol.EventInboxDismissed, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(dismissed.ID),
		"issue_id":     uuidToPtr(dismissed.IssueID),
		"recipient_id": uuidToString(dismissed.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(dismissed), dismissed.IssueID)
	writeJSON(w, http.StatusOK, resp)
}

type SnoozeInboxRequest struct {
	SnoozedUntil string `json:"snoozed_until"`
}

func (h *Handler) SnoozeInboxItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, ok := h.loadInboxItemForUser(w, r, id)
	if !ok {
		return
	}
	if item.TriageStatus == "handled" || item.TriageStatus == "dismissed" {
		writeError(w, http.StatusBadRequest, "handled or dismissed inbox items cannot be snoozed")
		return
	}

	var req SnoozeInboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	parsed, err := time.Parse(time.RFC3339, req.SnoozedUntil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid snoozed_until format (use RFC 3339)")
		return
	}
	if !parsed.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "snoozed_until must be in the future")
		return
	}

	userID := requestUserID(r)
	snoozed, err := h.Queries.SnoozeInboxItem(r.Context(), db.SnoozeInboxItemParams{
		ID:           item.ID,
		SnoozedUntil: pgtype.Timestamptz{Time: parsed, Valid: true},
		TriagedBy:    parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snooze inbox item")
		return
	}

	workspaceID := uuidToString(snoozed.WorkspaceID)
	h.publish(protocol.EventInboxSnoozed, workspaceID, "member", userID, map[string]any{
		"item_id":       uuidToString(snoozed.ID),
		"issue_id":      uuidToPtr(snoozed.IssueID),
		"recipient_id":  uuidToString(snoozed.RecipientID),
		"snoozed_until": timestampToString(snoozed.SnoozedUntil),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(snoozed), snoozed.IssueID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CountUnreadInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.CountUnreadInbox(r.Context(), db.CountUnreadInboxParams{
		WorkspaceID:   parseUUID(workspaceID),
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.MarkAllInboxRead(r.Context(), db.MarkAllInboxReadParams{
		WorkspaceID: parseUUID(workspaceID),
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.ArchiveAllInbox(r.Context(), db.ArchiveAllInboxParams{
		WorkspaceID: parseUUID(workspaceID),
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.ArchiveAllReadInbox(r.Context(), db.ArchiveAllReadInboxParams{
		WorkspaceID: parseUUID(workspaceID),
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
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.ArchiveCompletedInbox(r.Context(), db.ArchiveCompletedInboxParams{
		WorkspaceID: parseUUID(workspaceID),
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

func (h *Handler) HandleCompletedInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.HandleCompletedInbox(r.Context(), db.HandleCompletedInboxParams{
		WorkspaceID: parseUUID(workspaceID),
		RecipientID: parseUUID(userID),
		TriagedBy:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to handle completed inbox")
		return
	}

	slog.Info("inbox: handle completed", append(logger.RequestAttrs(r), "user_id", userID, "count", count)...)
	h.publish(protocol.EventInboxBatchTriaged, workspaceID, "member", userID, map[string]any{
		"recipient_id":  userID,
		"triage_status": "handled",
		"count":         count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) BatchHandleInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.BatchHandleInbox(r.Context(), db.BatchHandleInboxParams{
		WorkspaceID: parseUUID(workspaceID),
		RecipientID: parseUUID(userID),
		TriagedBy:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to handle inbox items")
		return
	}

	h.publish(protocol.EventInboxBatchTriaged, workspaceID, "member", userID, map[string]any{
		"recipient_id":  userID,
		"count":         count,
		"triage_status": "handled",
	})
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) BatchDismissInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	count, err := h.Queries.BatchDismissInbox(r.Context(), db.BatchDismissInboxParams{
		WorkspaceID: parseUUID(workspaceID),
		RecipientID: parseUUID(userID),
		TriagedBy:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to dismiss inbox items")
		return
	}

	h.publish(protocol.EventInboxBatchTriaged, workspaceID, "member", userID, map[string]any{
		"recipient_id":  userID,
		"count":         count,
		"triage_status": "dismissed",
	})
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *Handler) BatchSnoozeInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := r.Header.Get("X-Workspace-ID")

	var req SnoozeInboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	parsed, err := time.Parse(time.RFC3339, req.SnoozedUntil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid snoozed_until format (use RFC 3339)")
		return
	}
	if !parsed.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "snoozed_until must be in the future")
		return
	}

	count, err := h.Queries.BatchSnoozeInbox(r.Context(), db.BatchSnoozeInboxParams{
		WorkspaceID:  parseUUID(workspaceID),
		RecipientID:  parseUUID(userID),
		SnoozedUntil: pgtype.Timestamptz{Time: parsed, Valid: true},
		TriagedBy:    parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snooze inbox items")
		return
	}

	h.publish(protocol.EventInboxBatchTriaged, workspaceID, "member", userID, map[string]any{
		"recipient_id":  userID,
		"count":         count,
		"triage_status": "snoozed",
		"snoozed_until": parsed.Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}
