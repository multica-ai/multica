package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type InboxItemResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	RecipientType string  `json:"recipient_type"`
	RecipientID   string  `json:"recipient_id"`
	Type          string  `json:"type"`
	Severity      string  `json:"severity"`
	IssueID       *string `json:"issue_id"`
	Title         string  `json:"title"`
	Body          *string `json:"body"`
	Read          bool    `json:"read"`
	Archived      bool    `json:"archived"`
	CreatedAt     string  `json:"created_at"`
	IssueStatus   *string `json:"issue_status"`
	IssuePriority *string `json:"issue_priority"`
	// Issue fields the inbox filters match on. List rows always carry them
	// (an empty label list included), which is how a client tells a server
	// that supports those filters from an older one.
	IssueProjectID    *string         `json:"issue_project_id"`
	IssueAssigneeType *string         `json:"issue_assignee_type"`
	IssueAssigneeID   *string         `json:"issue_assignee_id"`
	IssueDueDate      *string         `json:"issue_due_date"`
	IssueLabelIDs     []string        `json:"issue_label_ids"`
	ActorType         *string         `json:"actor_type"`
	ActorID           *string         `json:"actor_id"`
	Details           json.RawMessage `json:"details"`
	// Section is the Inbox section the row belongs to: "needs_you" when it is
	// addressed to the recipient, "updates" when they only follow the issue
	// (inbox.EffectiveSection).
	Section string `json:"section"`
	// SnoozedUntil is when a snoozed row comes back. A time in the past means
	// it already has.
	SnoozedUntil *string `json:"snoozed_until"`
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
		Section:       inbox.EffectiveSection(i.Section, i.Type, i.Read),
		SnoozedUntil:  timestampToPtr(i.SnoozedUntil),
	}
}

// inboxListBodyPreviewLimit caps, in characters, the body a comment
// notification carries in the inbox LIST responses. The ellipsis counts
// toward it.
//
// A comment notification stores the full comment it was raised for, and the
// list shipped that verbatim — a several-thousand-character agent reply went
// over the wire, through JSON parsing and into the client cache, only for
// every client to render it as a single truncated line. 200 leaves ample room
// for that one line at any width or script, while bounding what each row can
// cost.
const inboxListBodyPreviewLimit = 200

// inboxListBody returns the body a notification carries in the inbox list:
// comment notifications on an issue get a bounded preview, everything else is
// returned as stored. The database keeps the full text.
//
// Scoped to exactly the rows whose full body no client ever reads from the
// list. Opening an issue-backed notification renders the issue itself, and the
// comment it points at is found through `details.comment_id`, which is left
// intact — on every client already installed, so none needs to change. Other
// notification types are NOT shortened: issue-less notifications render their
// body in the detail pane straight from the list cache, and Quick Create
// failures refill the composer from `details` — cutting those would lose
// content, not just bytes.
//
// Truncation is by code point, so a multi-byte character is never split into
// invalid UTF-8. A grapheme cluster (an emoji sequence, a letter with a
// combining mark) can still be cut at the boundary; that lands ~200 characters
// in, far past the single line any client shows.
//
// One pass that stops at the limit, rather than counting or converting the
// whole string: the work per row stays bounded by the preview, not by however
// long the comment happens to be.
func inboxListBody(notifType string, issueID pgtype.UUID, body pgtype.Text) *string {
	full := textToPtr(body)
	if full == nil || notifType != "new_comment" || !issueID.Valid {
		return full
	}
	// `range` yields the byte offset where each character starts. `cut` ends
	// up where the ellipsis goes: after limit-1 characters.
	cut, seen := 0, 0
	for offset := range *full {
		if seen == inboxListBodyPreviewLimit-1 {
			cut = offset
		}
		if seen++; seen > inboxListBodyPreviewLimit {
			preview := (*full)[:cut] + "…"
			return &preview
		}
	}
	return full
}

// inboxRowToResponse maps a LIST row — the main inbox and, through
// archivedInboxRowToResponse, the archived view. Single-item responses go
// through inboxToResponse and keep the full body.
func inboxRowToResponse(r db.ListInboxItemsRow) InboxItemResponse {
	return InboxItemResponse{
		ID:                uuidToString(r.ID),
		WorkspaceID:       uuidToString(r.WorkspaceID),
		RecipientType:     r.RecipientType,
		RecipientID:       uuidToString(r.RecipientID),
		Type:              r.Type,
		Severity:          r.Severity,
		IssueID:           uuidToPtr(r.IssueID),
		Title:             r.Title,
		Body:              inboxListBody(r.Type, r.IssueID, r.Body),
		Read:              r.Read,
		Archived:          r.Archived,
		CreatedAt:         timestampToString(r.CreatedAt),
		IssueStatus:       textToPtr(r.IssueStatus),
		IssuePriority:     textToPtr(r.IssuePriority),
		IssueProjectID:    uuidToPtr(r.IssueProjectID),
		IssueAssigneeType: textToPtr(r.IssueAssigneeType),
		IssueAssigneeID:   uuidToPtr(r.IssueAssigneeID),
		IssueDueDate:      dateToPtr(r.IssueDueDate),
		IssueLabelIDs:     inboxLabelIDs(r.IssueLabelIds),
		ActorType:         textToPtr(r.ActorType),
		ActorID:           uuidToPtr(r.ActorID),
		Details:           json.RawMessage(r.Details),
		Section:           inbox.EffectiveSection(r.Section, r.Type, r.Read),
		SnoozedUntil:      timestampToPtr(r.SnoozedUntil),
	}
}

func inboxLabelIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
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
		resp.IssueProjectID = uuidToPtr(issue.ProjectID)
		resp.IssueAssigneeType = textToPtr(issue.AssigneeType)
		resp.IssueAssigneeID = uuidToPtr(issue.AssigneeID)
		resp.IssueDueDate = dateToPtr(issue.DueDate)
		labels, err := h.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
		if err == nil {
			resp.IssueLabelIDs = make([]string, len(labels))
			for i, label := range labels {
				resp.IssueLabelIDs[i] = uuidToString(label.ID)
			}
		}
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

	// Notifications about private-project issues are not written for
	// outsiders (CreateInboxItem); this read filter also drops older rows once
	// the reader loses access.
	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	items, err := h.Queries.ListInboxItems(r.Context(), db.ListInboxItemsParams{
		WorkspaceID:      wsUUID,
		RecipientType:    "member",
		RecipientID:      parseUUID(userID),
		HiddenProjectIds: hidden.Param(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list inbox")
		return
	}

	// A snoozed row is out of the Inbox until its time passes. A client that
	// shows the Snoozed section asks for those rows; one that predates it gets
	// the list without them, so a snoozed issue is hidden there too instead of
	// sitting in its inbox as unread.
	includeSnoozed := r.URL.Query().Get("include_snoozed") == "true"
	now := time.Now()
	resp := make([]InboxItemResponse, 0, len(items))
	for _, item := range items {
		if !includeSnoozed && item.SnoozedUntil.Valid && item.SnoozedUntil.Time.After(now) {
			continue
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

	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	items, err := h.Queries.ListArchivedInboxItems(r.Context(), db.ListArchivedInboxItemsParams{
		WorkspaceID:      wsUUID,
		RecipientType:    "member",
		RecipientID:      parseUUID(userID),
		HiddenProjectIds: hidden.Param(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list archived inbox")
		return
	}

	resp := make([]InboxItemResponse, len(items))
	for i, item := range items {
		resp[i] = archivedInboxRowToResponse(item)
	}

	writeJSON(w, http.StatusOK, resp)
}

// MarkInboxRead reads the item and, when it belongs to an issue, every other
// unread active item the caller has on that issue: selecting a row in the inbox
// opens the whole issue (MUL-8618, see the query comment).
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
// Scope is the single item, the newest one the row shows, unlike MarkInboxRead
// — see the query comment.
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

func (h *Handler) DeleteInboxItem(w http.ResponseWriter, r *http.Request) {
	item, ok := h.loadInboxItemForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.requireIssueIDVisible(w, r, uuidToString(item.WorkspaceID), item.IssueID, http.StatusNotFound, "inbox item not found") {
		return
	}
	count, err := h.Queries.DeleteInboxGroup(r.Context(), db.DeleteInboxGroupParams{
		ID:          item.ID,
		WorkspaceID: item.WorkspaceID,
		RecipientID: item.RecipientID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete notification")
		return
	}
	h.publish(protocol.EventInboxDeleted, uuidToString(item.WorkspaceID), "member", requestUserID(r), map[string]any{
		"item_id":      uuidToString(item.ID),
		"recipient_id": uuidToString(item.RecipientID),
		"count":        count,
	})
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
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
// `read` is untouched on purpose for an issue that needs the member. An item
// archived while unread comes back unread, which raises the unread badge again
// — the badge only ever counted non-archived items, so restoring one is a real
// addition, not a bug. An update comes back unread whatever it was: see
// MarkRestoredInboxUpdateUnread.
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

	if flipped, err := h.Queries.MarkRestoredInboxUpdateUnread(r.Context(), db.MarkRestoredInboxUpdateUnreadParams{
		ID:            item.ID,
		NeedsYouTypes: inbox.NeedsYouTypes,
	}); err != nil {
		slog.Warn("inbox: mark restored update unread failed", append(logger.RequestAttrs(r), "item_id", id, "error", err)...)
	} else if flipped > 0 {
		if fresh, err := h.Queries.GetInboxItem(r.Context(), item.ID); err == nil {
			item = fresh
		}
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

	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	count, err := h.Queries.CountUnreadInbox(r.Context(), db.CountUnreadInboxParams{
		WorkspaceID:      wsUUID,
		RecipientType:    "member",
		RecipientID:      parseUUID(userID),
		HiddenProjectIds: hidden.Param(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread inbox")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

// InboxWorkspaceUnreadResponse is one workspace's inbox counts in the
// cross-workspace summary.
type InboxWorkspaceUnreadResponse struct {
	WorkspaceID string `json:"workspace_id"`
	// Count is the number of unread issues. Clients that predate the sectioned
	// Inbox show it as their badge, so its meaning does not change.
	Count int64 `json:"count"`
	// NeedsYou is the number of issues waiting on the user, read or not: the
	// rows of the Inbox's "Needs you" section, and the badge of a client that
	// shows it (MUL-9215).
	NeedsYou int64 `json:"needs_you"`
	// NextWakeAt is when the workspace's next snoozed issue comes back, RFC
	// 3339, or absent with none. The counts leave snoozed issues out and
	// nothing is sent when a snooze runs out, so a client re-reads the
	// summary then.
	NextWakeAt *string `json:"next_wake_at,omitempty"`
}

// UnreadInboxSummary returns per-workspace inbox counts across every workspace
// the user belongs to. The rail badge and the workspace switcher dot read it
// without fetching each workspace's full inbox list. It is account-level by
// nature: it ignores the active workspace and keys only on the authenticated
// user. A workspace appears when either count is above zero or it has a
// snoozed issue still to come back.
func (h *Handler) UnreadInboxSummary(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	unread, err := h.Queries.CountUnreadInboxByWorkspace(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to summarize unread inbox")
		return
	}
	needsYou, err := h.Queries.CountNeedsYouInboxByWorkspace(r.Context(), db.CountNeedsYouInboxByWorkspaceParams{
		RecipientID:   parseUUID(userID),
		NeedsYouTypes: inbox.NeedsYouTypes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to summarize inbox")
		return
	}
	wakes, err := h.Queries.NextInboxWakeByWorkspace(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to summarize inbox")
		return
	}

	byWorkspace := make(map[string]*InboxWorkspaceUnreadResponse, len(unread)+len(needsYou))
	entry := func(workspaceID pgtype.UUID) *InboxWorkspaceUnreadResponse {
		id := uuidToString(workspaceID)
		if byWorkspace[id] == nil {
			byWorkspace[id] = &InboxWorkspaceUnreadResponse{WorkspaceID: id}
		}
		return byWorkspace[id]
	}
	for _, row := range unread {
		entry(row.WorkspaceID).Count = row.Count
	}
	for _, row := range needsYou {
		entry(row.WorkspaceID).NeedsYou = row.Count
	}
	for _, row := range wakes {
		entry(row.WorkspaceID).NextWakeAt = timestampToPtr(row.NextWakeAt)
	}

	resp := make([]InboxWorkspaceUnreadResponse, 0, len(byWorkspace))
	for _, summary := range byWorkspace {
		resp = append(resp, *summary)
	}
	sort.Slice(resp, func(i, j int) bool { return resp[i].WorkspaceID < resp[j].WorkspaceID })

	writeJSON(w, http.StatusOK, resp)
}

// inboxSnoozeMaxDuration bounds how far ahead an issue can be snoozed. The
// picker offers hours to days; a year is far past any of it and keeps a typo
// or a clock gone wrong from parking an issue for good.
const inboxSnoozeMaxDuration = 366 * 24 * time.Hour

type SnoozeInboxRequest struct {
	// Until is when the issue comes back, RFC 3339.
	Until string `json:"until"`
}

// SnoozeInboxItem takes an issue out of the Inbox until a time the member
// picks. It is issue-level, like archiving: every active row the member has on
// the item's issue is snoozed with it. The issue comes back when the time
// passes, or earlier when something new is addressed to the member: a row
// written after the snooze carries no wake time.
func (h *Handler) SnoozeInboxItem(w http.ResponseWriter, r *http.Request) {
	item, ok := h.loadInboxItemForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req SnoozeInboxRequest
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
	if until.After(now.Add(inboxSnoozeMaxDuration)) {
		writeError(w, http.StatusBadRequest, "until is too far in the future")
		return
	}
	if item.Archived {
		writeError(w, http.StatusConflict, "an archived notification cannot be snoozed")
		return
	}

	count, err := h.Queries.SnoozeInboxGroup(r.Context(), db.SnoozeInboxGroupParams{
		ID:           item.ID,
		SnoozedUntil: pgtype.Timestamptz{Time: until, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snooze")
		return
	}

	snoozedUntil := until.UTC().Format(time.RFC3339)
	h.publish(protocol.EventInboxSnoozed, uuidToString(item.WorkspaceID), "member", requestUserID(r), map[string]any{
		"item_id":       uuidToString(item.ID),
		"issue_id":      uuidToPtr(item.IssueID),
		"recipient_id":  uuidToString(item.RecipientID),
		"snoozed_until": snoozedUntil,
	})
	writeJSON(w, http.StatusOK, map[string]any{"count": count, "snoozed_until": snoozedUntil})
}

// UnsnoozeInboxItem brings a snoozed issue back now. Issue-level, mirroring
// SnoozeInboxItem.
func (h *Handler) UnsnoozeInboxItem(w http.ResponseWriter, r *http.Request) {
	item, ok := h.loadInboxItemForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	count, err := h.Queries.UnsnoozeInboxGroup(r.Context(), item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unsnooze")
		return
	}
	// Bringing back an issue that was not snoozed changes no row, so it must
	// not make every open client refetch its inbox.
	if count > 0 {
		h.publish(protocol.EventInboxUnsnoozed, uuidToString(item.WorkspaceID), "member", requestUserID(r), map[string]any{
			"item_id":      uuidToString(item.ID),
			"issue_id":     uuidToPtr(item.IssueID),
			"recipient_id": uuidToString(item.RecipientID),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
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

// MarkIssueInboxRead marks every unread active notification the caller has on
// one issue read (see MarkInboxReadByIssue). The client calls it while the
// issue is on screen outside the inbox, so reading an issue anywhere also
// reads it in the inbox (MUL-8588). The update
// is scoped to the caller's own rows, so an issue id from another workspace
// matches nothing.
func (h *Handler) MarkIssueInboxRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue id")
	if !ok {
		return
	}

	count, err := h.Queries.MarkInboxReadByIssue(r.Context(), db.MarkInboxReadByIssueParams{
		WorkspaceID: wsUUID,
		RecipientID: parseUUID(userID),
		IssueID:     issueUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark issue inbox read")
		return
	}

	// Opening an issue with nothing unread is the common case. It changes no
	// row, so it must not make every open client refetch its inbox.
	if count > 0 {
		h.publish(protocol.EventInboxBatchRead, workspaceID, "member", userID, map[string]any{
			"recipient_id": userID,
			"count":        count,
			"issue_id":     uuidToString(issueUUID),
		})
	}

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

// inboxArchiveUpdatesMaxGroups bounds one archive-updates request. A client
// with more groups to put away sends them in several requests.
const inboxArchiveUpdatesMaxGroups = 500

type ArchiveInboxUpdatesRequest struct {
	// The issues to archive, by issue id. A notification without an issue is
	// named by its own id, which is how the Inbox keys its rows.
	Groups []string `json:"groups"`
	// Archive only the groups that have been read.
	ReadOnly bool `json:"read_only"`
}

// ArchiveInboxUpdates archives the listed issues as long as they are still
// only updates for the caller (see ArchiveInboxUpdateGroups). The Inbox calls
// it with read_only for the updates the member has read, which leave the list
// once read, and without it when the member clears the "Updates" section.
//
// A listed issue that needs the caller, is snoozed, or (with read_only) has
// become unread again is left alone, so the count can be lower than the
// number of groups sent.
func (h *Handler) ArchiveInboxUpdates(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	var req ArchiveInboxUpdatesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Groups) > inboxArchiveUpdatesMaxGroups {
		writeError(w, http.StatusBadRequest, "too many groups")
		return
	}
	groupIDs := make([]pgtype.UUID, 0, len(req.Groups))
	for _, group := range req.Groups {
		groupID, ok := parseUUIDOrBadRequest(w, group, "group id")
		if !ok {
			return
		}
		groupIDs = append(groupIDs, groupID)
	}
	if len(groupIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"count": 0})
		return
	}

	count, err := h.Queries.ArchiveInboxUpdateGroups(r.Context(), db.ArchiveInboxUpdateGroupsParams{
		WorkspaceID:   wsUUID,
		RecipientID:   parseUUID(userID),
		GroupIds:      groupIDs,
		NeedsYouTypes: inbox.NeedsYouTypes,
		ReadOnly:      req.ReadOnly,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive inbox updates")
		return
	}

	// Nothing archived changes no row, so it must not make every open client
	// refetch its inbox.
	if count > 0 {
		h.publish(protocol.EventInboxBatchArchived, workspaceID, "member", userID, map[string]any{
			"recipient_id": userID,
			"count":        count,
		})
	}

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

	terminalStatusKeys, err := h.terminalIssueStatusKeys(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve status categories")
		return
	}
	count, err := h.Queries.ArchiveCompletedInbox(r.Context(), db.ArchiveCompletedInboxParams{
		WorkspaceID:        wsUUID,
		RecipientID:        parseUUID(userID),
		TerminalStatusKeys: terminalStatusKeys,
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
