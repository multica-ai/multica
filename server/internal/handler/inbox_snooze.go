package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/inbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// SnoozedInboxResponse is one snoozed issue in the caller's inbox.
type SnoozedInboxResponse struct {
	IssueID      string `json:"issue_id"`
	ItemID       string `json:"item_id"`
	SnoozedUntil string `json:"snoozed_until"`
	SnoozedAt    string `json:"snoozed_at"`
	// Rows that arrived on the issue while it was snoozed.
	Updates int64 `json:"updates"`
}

// ListSnoozedInbox returns the caller's snoozed issues, soonest back first,
// for the Snoozed view (MUL-9333). An issue whose time has passed is back in
// the main list and no longer here.
func (h *Handler) ListSnoozedInbox(w http.ResponseWriter, r *http.Request) {
	r, cancel := h.inboxReadRequest(r)
	defer cancel()
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	hidden, ok := h.requestInboxHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	rows, err := runInboxRead(r.Context(), h, "snoozed", func(ctx context.Context, q *db.Queries) ([]db.ListSnoozedInboxRow, error) {
		return q.ListSnoozedInbox(ctx, db.ListSnoozedInboxParams{
			WorkspaceID:      wsUUID,
			RecipientID:      parseUUID(userID),
			HiddenProjectIds: hidden.Param(),
		})
	})
	if err != nil {
		writeInboxReadError(w, err, "failed to list snoozed inbox")
		return
	}
	resp := make([]SnoozedInboxResponse, len(rows))
	for i, row := range rows {
		resp[i] = SnoozedInboxResponse{
			IssueID:      uuidToString(row.IssueID),
			ItemID:       uuidToString(row.ItemID),
			SnoozedUntil: timestampToString(row.SnoozedUntil),
			SnoozedAt:    timestampToString(row.SnoozedAt),
			Updates:      row.Updates,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type snoozeInboxRequest struct {
	Until string `json:"until"`
}

// SnoozeInboxItem takes the item's issue out of the caller's inbox until a
// time (MUL-9333). Issue-level, like archive: every active row on the issue
// is snoozed, and rows written later join the snooze unless they need the
// caller (inbox.WakesSnooze). Snoozing a snoozed issue changes its time.
func (h *Handler) SnoozeInboxItem(w http.ResponseWriter, r *http.Request) {
	prev, ok := h.loadInboxItemForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !prev.IssueID.Valid {
		writeError(w, http.StatusBadRequest, "only notifications on an issue can be snoozed")
		return
	}
	if prev.Archived {
		writeError(w, http.StatusBadRequest, "an archived notification cannot be snoozed")
		return
	}
	var req snoozeInboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	until, err := time.Parse(time.RFC3339, req.Until)
	if err != nil {
		writeError(w, http.StatusBadRequest, "until must be an RFC 3339 time")
		return
	}
	now := time.Now()
	if !until.After(now) {
		writeError(w, http.StatusBadRequest, "until must be in the future")
		return
	}
	if until.After(now.Add(inbox.MaxSnooze)) {
		writeError(w, http.StatusBadRequest, "until must be within a year")
		return
	}

	rows, err := h.Queries.SnoozeInboxGroup(r.Context(), db.SnoozeInboxGroupParams{
		ID:           prev.ID,
		SnoozedUntil: pgtype.Timestamptz{Time: until, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snooze")
		return
	}

	h.publish(protocol.EventInboxSnoozed, uuidToString(prev.WorkspaceID), "member", requestUserID(r), map[string]any{
		"item_id":       uuidToString(prev.ID),
		"issue_id":      uuidToPtr(prev.IssueID),
		"recipient_id":  uuidToString(prev.RecipientID),
		"snoozed_until": until.UTC().Format(time.RFC3339),
	})
	h.publishInboxRefresh(r, prev)
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id":      uuidToString(prev.IssueID),
		"snoozed_until": until.UTC().Format(time.RFC3339),
		"count":         len(rows),
	})
}

type unsnoozeInboxRequest struct {
	// Read restores the issue as read. Undoing a snooze puts back the read
	// state it had; ending one brings it back unread.
	Read bool `json:"read"`
}

// UnsnoozeInboxItem ends the snooze on the item's issue: it is back in the
// inbox now.
func (h *Handler) UnsnoozeInboxItem(w http.ResponseWriter, r *http.Request) {
	prev, ok := h.loadInboxItemForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !prev.IssueID.Valid {
		writeError(w, http.StatusBadRequest, "only notifications on an issue can be snoozed")
		return
	}
	var req unsnoozeInboxRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	rows, err := h.Queries.UnsnoozeInboxGroup(r.Context(), db.UnsnoozeInboxGroupParams{
		WorkspaceID:   prev.WorkspaceID,
		RecipientType: prev.RecipientType,
		RecipientID:   prev.RecipientID,
		IssueID:       prev.IssueID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unsnooze")
		return
	}
	if req.Read {
		if _, err := h.Queries.MarkInboxRead(r.Context(), prev.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to mark read")
			return
		}
	}

	h.publish(protocol.EventInboxUnsnoozed, uuidToString(prev.WorkspaceID), "member", requestUserID(r), map[string]any{
		"item_id":      uuidToString(prev.ID),
		"issue_id":     uuidToPtr(prev.IssueID),
		"recipient_id": uuidToString(prev.RecipientID),
	})
	h.publishInboxRefresh(r, prev)
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id": uuidToString(prev.IssueID),
		"count":    len(rows),
	})
}

// publishInboxRefresh repeats a snooze or unsnooze as a batch read of no rows.
// Mobile builds from before snooze refetch the inbox only on the event names
// they subscribe to, and this is the one every client takes as "refetch the
// inbox and badge" with no other effect. Rows written into a snooze don't send
// it: they stay out of the list and must not reach anyone as new.
func (h *Handler) publishInboxRefresh(r *http.Request, prev db.InboxItem) {
	h.publish(protocol.EventInboxBatchRead, uuidToString(prev.WorkspaceID), "member", requestUserID(r), map[string]any{
		"recipient_id": uuidToString(prev.RecipientID),
		"count":        0,
	})
}
