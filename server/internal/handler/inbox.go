package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
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
	IssuePriority *string         `json:"issue_priority"`
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
		IssueID:       uuidToPtr(r.IssueID),
		Title:         r.Title,
		Body:          textToPtr(r.Body),
		Read:          r.Read,
		Archived:      r.Archived,
		CreatedAt:     timestampToString(r.CreatedAt),
		IssueStatus:   textToPtr(r.IssueStatus),
		IssuePriority: textToPtr(r.IssuePriority),
		ActorType:     textToPtr(r.ActorType),
		ActorID:       uuidToPtr(r.ActorID),
		Details:       json.RawMessage(r.Details),
	}
}

// ListArchivedInboxItemsRow carries the same columns as ListInboxItemsRow (both
// queries select `inbox_item.*` plus the joined issue projections), so the archived
// row converts to the active one and reuses its mapper. If either query's
// column list drifts, this conversion stops compiling — which is the point.
func archivedInboxRowToResponse(r db.ListArchivedInboxItemsRow) InboxItemResponse {
	return inboxRowToResponse(db.ListInboxItemsRow(r))
}

func (h *Handler) enrichInboxResponse(ctx context.Context, resp InboxItemResponse, issueID pgtype.UUID) InboxItemResponse {
	if !issueID.Valid {
		return resp
	}
	issue, err := h.Queries.GetIssue(ctx, issueID)
	if err == nil {
		s := issue.Status
		resp.IssueStatus = &s
		p := issue.Priority
		resp.IssuePriority = &p
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

	items, err := h.Queries.ListInboxItems(r.Context(), db.ListInboxItemsParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list inbox")
		return
	}

	policy, windowEnabled := h.issueWindowPolicy(r.Context(), wsUUID)
	issueIDs := make([]pgtype.UUID, 0, len(items))
	for _, item := range items {
		if item.IssueID.Valid {
			issueIDs = append(issueIDs, item.IssueID)
		}
	}
	var visible map[pgtype.UUID]struct{}
	if windowEnabled && policy.action == entitlement.ActionEnforce {
		visible, err = h.visibleIssueIDSet(r.Context(), wsUUID, policy, issueIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list inbox")
			return
		}
	} else if windowEnabled {
		h.observeIssueWindow(r.Context(), wsUUID, policy, issueIDs, "inbox")
	}
	resp := make([]InboxItemResponse, 0, len(items))
	for _, item := range items {
		if visible != nil && item.IssueID.Valid {
			if _, ok := visible[item.IssueID]; !ok {
				continue
			}
		}
		resp = append(resp, inboxRowToResponse(item))
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListArchivedInbox returns the recipient's archived notifications, backing the
// inbox's "Archived" sub-view. Kept as its own endpoint rather than a flag on
// ListInbox so installed clients keep their current contract, and so the
// unbounded archive never rides along with the main list.
//
// The query drops any issue that also has an active row, keeping this list and
// the main inbox mutually exclusive per issue group. It selects at most 200
// groups and returns only each group's newest row plus its optional comment
// anchor — see the query comment for both the bound and the grouping contract.
func (h *Handler) ListArchivedInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	items, err := h.Queries.ListArchivedInboxItems(r.Context(), db.ListArchivedInboxItemsParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list archived inbox")
		return
	}

	policy, windowEnabled := h.issueWindowPolicy(r.Context(), wsUUID)
	issueIDs := make([]pgtype.UUID, 0, len(items))
	for _, item := range items {
		if item.IssueID.Valid {
			issueIDs = append(issueIDs, item.IssueID)
		}
	}
	var visible map[pgtype.UUID]struct{}
	if windowEnabled && policy.action == entitlement.ActionEnforce {
		visible, err = h.visibleIssueIDSet(r.Context(), wsUUID, policy, issueIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list archived inbox")
			return
		}
	} else if windowEnabled {
		h.observeIssueWindow(r.Context(), wsUUID, policy, issueIDs, "inbox")
	}
	resp := make([]InboxItemResponse, 0, len(items))
	for _, item := range items {
		if visible != nil && item.IssueID.Valid {
			if _, ok := visible[item.IssueID]; !ok {
				continue
			}
		}
		resp = append(resp, archivedInboxRowToResponse(item))
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

// MarkInboxUnread flips a notification back to unread, the inverse of
// MarkInboxRead. It exists so a user can park something they opened but did not
// act on: the inbox auto-marks an item read the moment it is selected, which
// otherwise makes "opened" and "handled" the same signal.
//
// Scope is the single item, matching MarkInboxRead — see the query comment.
func (h *Handler) MarkInboxUnread(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prev, ok := h.loadInboxItemForUser(w, r, id)
	if !ok {
		return
	}
	item, err := h.Queries.MarkInboxUnread(r.Context(), prev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark unread")
		return
	}

	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxUnread, workspaceID, "member", userID, map[string]any{
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

// UnarchiveInboxItem restores an archived notification to the main inbox. It is
// the inverse of ArchiveInboxItem and mirrors its issue-level scope: archiving
// one item archives every sibling for the same issue, so restoring brings the
// whole group back.
//
// `read` is untouched on purpose. An item archived while unread comes back
// unread, which raises the unread badge again — the badge only ever counted
// non-archived items, so restoring one is a real addition, not a bug.
func (h *Handler) UnarchiveInboxItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prev, ok := h.loadInboxItemForUser(w, r, id)
	if !ok {
		return
	}
	item, err := h.Queries.UnarchiveInboxItem(r.Context(), prev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unarchive")
		return
	}

	// Restore all sibling inbox items for the same issue (issue-level restore).
	if item.IssueID.Valid {
		h.Queries.UnarchiveInboxByIssue(r.Context(), db.UnarchiveInboxByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
		})
	}

	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxUnarchived, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"issue_id":     uuidToPtr(item.IssueID),
		"recipient_id": uuidToString(item.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, resp)
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

	var count int64
	var err error
	policy, windowEnabled := h.issueWindowPolicy(r.Context(), wsUUID)
	if windowEnabled && policy.action == entitlement.ActionEnforce {
		count, err = h.countUnreadInboxWithinWindow(r.Context(), wsUUID, parseUUID(userID), policy)
	} else {
		count, err = h.Queries.CountUnreadInbox(r.Context(), db.CountUnreadInboxParams{
			WorkspaceID:   wsUUID,
			RecipientType: "member",
			RecipientID:   parseUUID(userID),
		})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread inbox")
		return
	}
	if windowEnabled && policy.action == entitlement.ActionObserve {
		windowed, observeErr := h.countUnreadInboxWithinWindow(r.Context(), wsUUID, parseUUID(userID), policy)
		if observeErr != nil {
			slog.Warn("observe unread inbox window failed", "error", observeErr)
			h.recordIssueWindow(policy.action, "inbox_count", "error")
		} else if windowed == count {
			h.recordIssueWindow(policy.action, "inbox_count", "allowed")
		} else {
			h.recordIssueWindow(policy.action, "inbox_count", "would_block")
		}
	}

	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

// InboxWorkspaceUnreadResponse is one workspace's unread inbox count in the
// cross-workspace summary.
type InboxWorkspaceUnreadResponse struct {
	WorkspaceID string `json:"workspace_id"`
	Count       int64  `json:"count"`
}

// UnreadInboxSummary returns per-workspace unread inbox counts across every
// workspace the user belongs to. The sidebar uses it to light a dot on the
// workspace switcher when a workspace OTHER than the active one has unread
// items, without fetching each workspace's full inbox list. It is
// account-level by nature: it ignores the active workspace and keys only on
// the authenticated user.
func (h *Handler) UnreadInboxSummary(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.CountUnreadInboxByWorkspace(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to summarize unread inbox")
		return
	}

	type workspacePolicy struct {
		policy      issueWindowPolicy
		legacyCount int64
	}
	policies := make(map[pgtype.UUID]workspacePolicy, len(rows))
	workspaceIDs := make([]pgtype.UUID, 0, len(rows))
	limits := make([]int64, 0, len(rows))
	for _, row := range rows {
		if policy, enabled := h.issueWindowPolicy(r.Context(), row.WorkspaceID); enabled {
			policies[row.WorkspaceID] = workspacePolicy{policy: policy, legacyCount: row.Count}
			workspaceIDs = append(workspaceIDs, row.WorkspaceID)
			limits = append(limits, policy.limit)
		}
	}
	windowedCounts := map[pgtype.UUID]int64{}
	if len(workspaceIDs) > 0 {
		windowedCounts, err = h.unreadInboxCountsWithinWindows(r.Context(), parseUUID(userID), workspaceIDs, limits)
		if err != nil {
			failClosed := false
			for _, item := range policies {
				if item.policy.action == entitlement.ActionEnforce {
					failClosed = true
				} else {
					h.recordIssueWindow(item.policy.action, "inbox_summary", "error")
				}
			}
			if failClosed {
				writeError(w, http.StatusInternalServerError, "failed to summarize unread inbox")
				return
			}
			slog.Warn("observe unread inbox summary window failed", "error", err)
		}
	}

	resp := make([]InboxWorkspaceUnreadResponse, 0, len(rows))
	for _, row := range rows {
		count := row.Count
		if item, enabled := policies[row.WorkspaceID]; enabled && err == nil {
			windowed := windowedCounts[row.WorkspaceID]
			if item.policy.action == entitlement.ActionEnforce {
				count = windowed
			} else if windowed == item.legacyCount {
				h.recordIssueWindow(item.policy.action, "inbox_summary", "allowed")
			} else {
				h.recordIssueWindow(item.policy.action, "inbox_summary", "would_block")
			}
		}
		if count == 0 {
			continue
		}
		resp = append(resp, InboxWorkspaceUnreadResponse{
			WorkspaceID: uuidToString(row.WorkspaceID),
			Count:       count,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) countUnreadInboxWithinWindow(ctx context.Context, workspaceID, recipientID pgtype.UUID, policy issueWindowPolicy) (int64, error) {
	query := fmt.Sprintf(`SELECT COUNT(*)::bigint
	FROM inbox_item i
	WHERE i.workspace_id = $1
	  AND i.recipient_type = 'member'
	  AND i.recipient_id = $2
	  AND i.read = false
	  AND i.archived = false
	  AND (i.issue_id IS NULL OR %s)`, issueWindowIDPredicate("i.issue_id", "$1", "$3"))
	var count int64
	err := h.DB.QueryRow(ctx, query, workspaceID, recipientID, policy.limit).Scan(&count)
	return count, err
}

// unreadInboxCountsWithinWindows preserves CountUnreadInboxByWorkspace's
// DISTINCT ON semantics while applying every enabled workspace policy in one
// database round trip. Observe callers use the result only for telemetry; the
// legacy response remains untouched.
func (h *Handler) unreadInboxCountsWithinWindows(ctx context.Context, recipientID pgtype.UUID, workspaceIDs []pgtype.UUID, limits []int64) (map[pgtype.UUID]int64, error) {
	query := fmt.Sprintf(`WITH policies AS (
	SELECT workspace_id, issue_limit
	FROM unnest($1::uuid[], $2::bigint[]) AS policy(workspace_id, issue_limit)
)
	SELECT policy.workspace_id, filtered.count
	FROM policies policy
	CROSS JOIN LATERAL (
		SELECT COUNT(*)::bigint AS count
		FROM (
			SELECT DISTINCT ON (COALESCE(i.issue_id, i.id)) i.read
			FROM inbox_item i
			JOIN member m ON m.workspace_id = i.workspace_id AND m.user_id = i.recipient_id
			WHERE i.workspace_id = policy.workspace_id
			  AND i.recipient_type = 'member'
			  AND i.recipient_id = $3
			  AND i.archived = false
			  AND (i.issue_id IS NULL OR %s)
			ORDER BY COALESCE(i.issue_id, i.id), i.created_at DESC
		) newest
		WHERE newest.read = false
	) filtered`, issueWindowIDPredicate("i.issue_id", "policy.workspace_id", "policy.issue_limit"))
	rows, err := h.DB.Query(ctx, query, workspaceIDs, limits, recipientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[pgtype.UUID]int64, len(workspaceIDs))
	for rows.Next() {
		var workspaceID pgtype.UUID
		var count int64
		if err := rows.Scan(&workspaceID, &count); err != nil {
			return nil, err
		}
		counts[workspaceID] = count
	}
	return counts, rows.Err()
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
