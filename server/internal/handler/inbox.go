package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
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
	// The issue's identifier (MUL-123), so a client links to the canonical
	// issue URL. Opening `/issues/<uuid>` instead rewrites the address once
	// the issue loads, and on web that mounts the whole page a second time.
	// Null when the item has no issue or the prefix could not be resolved.
	IssueIdentifier *string `json:"issue_identifier"`
	Title           string  `json:"title"`
	Body            *string `json:"body"`
	Read            bool    `json:"read"`
	Archived        bool    `json:"archived"`
	CreatedAt       string  `json:"created_at"`
	IssueStatus     *string `json:"issue_status"`
	IssuePriority   *string `json:"issue_priority"`
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
	// When the issue comes back from a snooze (MUL-9333). In the list it is
	// always past: the issue came back on its own at that time.
	SnoozedUntil *string `json:"snoozed_until,omitempty"`
	// When that snooze began; the issue's rows written after it arrived
	// while it was away.
	SnoozedAt *string `json:"snoozed_at,omitempty"`
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
		SnoozedUntil:  timestampToPtr(i.SnoozedUntil),
		SnoozedAt:     timestampToPtr(i.SnoozedAt),
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

// inboxIssueIdentifier formats an inbox row's issue identifier the way issue
// responses do. `prefix` is the workspace's effective issue prefix; empty when
// it could not be loaded, which leaves the client on the UUID link.
func inboxIssueIdentifier(prefix string, number pgtype.Int4) *string {
	if prefix == "" || !number.Valid {
		return nil
	}
	identifier := prefix + "-" + strconv.Itoa(int(number.Int32))
	return &identifier
}

// inboxRowToResponse maps a LIST row — the main inbox and, through
// archivedInboxRowToResponse, the archived view. Single-item responses go
// through inboxToResponse and keep the full body.
func inboxRowToResponse(r db.ListInboxItemsRow, issuePrefix string) InboxItemResponse {
	return InboxItemResponse{
		ID:                uuidToString(r.ID),
		WorkspaceID:       uuidToString(r.WorkspaceID),
		RecipientType:     r.RecipientType,
		RecipientID:       uuidToString(r.RecipientID),
		Type:              r.Type,
		Severity:          r.Severity,
		IssueID:           uuidToPtr(r.IssueID),
		IssueIdentifier:   inboxIssueIdentifier(issuePrefix, r.IssueNumber),
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
		SnoozedUntil:      timestampToPtr(r.SnoozedUntil),
		SnoozedAt:         timestampToPtr(r.SnoozedAt),
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
func archivedInboxRowToResponse(r db.ListArchivedInboxItemsRow, issuePrefix string) InboxItemResponse {
	return inboxRowToResponse(db.ListInboxItemsRow(r), issuePrefix)
}

func (h *Handler) enrichInboxResponse(ctx context.Context, resp InboxItemResponse, issueID pgtype.UUID) InboxItemResponse {
	if !issueID.Valid {
		return resp
	}
	issue, err := h.Queries.GetIssue(ctx, issueID)
	if err == nil {
		resp.IssueIdentifier = inboxIssueIdentifier(h.getIssuePrefix(ctx, issue.WorkspaceID), pgtype.Int4{Int32: issue.Number, Valid: true})
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

	if !h.requireInboxOwnerAccess(w, r, userID, wsUUID) {
		return
	}

	// Notifications about private-project issues are not written for
	// outsiders (CreateInboxItem); this read filter also drops older rows once
	// the reader loses access.
	hidden, ok := h.requestInboxHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	items, err := runInboxRead(r.Context(), h, "list", func(ctx context.Context, q *db.Queries) ([]db.ListInboxItemsRow, error) {
		return q.ListInboxItems(ctx, db.ListInboxItemsParams{
			WorkspaceID:      wsUUID,
			RecipientType:    "member",
			RecipientID:      parseUUID(userID),
			HiddenProjectIds: hidden.Param(),
		})
	})
	if err != nil {
		writeInboxReadError(w, err, "failed to list inbox")
		return
	}

	issuePrefix := h.getIssuePrefix(r.Context(), wsUUID)
	resp := make([]InboxItemResponse, len(items))
	for i, item := range items {
		resp[i] = inboxRowToResponse(item, issuePrefix)
	}

	writeJSON(w, http.StatusOK, resp)
}

// Task tokens identify the runtime owner, not necessarily the human who asked
// the agent to act. Reading or changing the owner's notifications requires
// that same human upstream; every inbox endpoint goes through this gate. Auth
// strips client identity headers and stamps the task/agent from the token.
func (h *Handler) requireInboxOwnerAccess(w http.ResponseWriter, r *http.Request, userID string, workspaceID pgtype.UUID) bool {
	source := r.Header.Get("X-Actor-Source")
	if source == "" {
		return true
	}
	deny := func() bool {
		writeError(w, http.StatusForbidden, "inbox access requires the runtime owner's verified human trigger")
		return false
	}
	queryFailure := func(err error) bool {
		if errors.Is(err, pgx.ErrNoRows) {
			return deny()
		}
		slog.Warn("resolve inbox access failed", append(logger.RequestAttrs(r), "error", err)...)
		writeInboxReadError(w, err, "failed to resolve inbox access")
		return false
	}
	// A cloud-node PAT has no authenticated task binding or human trigger.
	if source != "task_token" {
		return deny()
	}
	taskID, err := util.ParseUUID(r.Header.Get("X-Task-ID"))
	if err != nil {
		return deny()
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil {
		return queryFailure(err)
	}
	if isTerminalTaskStatus(task.Status) ||
		!task.OriginatorUserID.Valid || uuidToString(task.OriginatorUserID) != userID ||
		uuidToString(task.AgentID) != r.Header.Get("X-Agent-ID") {
		return deny()
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID: task.RuntimeID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return queryFailure(err)
	}
	if !runtime.OwnerID.Valid || uuidToString(runtime.OwnerID) != userID {
		return deny()
	}
	return true
}

// logAgentInboxWrite records which agent changed which human's inbox, so a
// change made on the owner's behalf can be traced to the run that made it.
// Human writes need no extra record.
func logAgentInboxWrite(r *http.Request, action string, attrs ...any) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		return
	}
	attrs = append([]any{"action", action, "agent_id", r.Header.Get("X-Agent-ID"), "task_id", r.Header.Get("X-Task-ID")}, attrs...)
	slog.Info("inbox: agent write", append(logger.RequestAttrs(r), attrs...)...)
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

	if !h.requireInboxOwnerAccess(w, r, userID, wsUUID) {
		return
	}

	hidden, ok := h.requestInboxHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	items, err := runInboxRead(r.Context(), h, "archived", func(ctx context.Context, q *db.Queries) ([]db.ListArchivedInboxItemsRow, error) {
		return q.ListArchivedInboxItems(ctx, db.ListArchivedInboxItemsParams{
			WorkspaceID:      wsUUID,
			RecipientType:    "member",
			RecipientID:      parseUUID(userID),
			HiddenProjectIds: hidden.Param(),
		})
	})
	if err != nil {
		writeInboxReadError(w, err, "failed to list archived inbox")
		return
	}

	issuePrefix := h.getIssuePrefix(r.Context(), wsUUID)
	resp := make([]InboxItemResponse, len(items))
	for i, item := range items {
		resp[i] = archivedInboxRowToResponse(item, issuePrefix)
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
	row, err := h.Queries.MarkInboxRead(r.Context(), prev.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}
	item := row.InboxItem
	count := row.SiblingsRead + changedCount(!prev.Read)

	logAgentInboxWrite(r, "read", "item_id", uuidToString(item.ID), "count", count)
	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxRead, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"recipient_id": uuidToString(item.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, inboxWriteResponse{resp, count})
}

// inboxWriteResponse is the changed item plus how many notifications the change
// actually modified, its issue group included; repeating a change counts 0.
type inboxWriteResponse struct {
	InboxItemResponse
	Count int64 `json:"count"`
}

func changedCount(changed bool) int64 {
	if changed {
		return 1
	}
	return 0
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

	count := changedCount(prev.Read)
	logAgentInboxWrite(r, "unread", "item_id", uuidToString(item.ID), "count", count)
	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxUnread, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"recipient_id": uuidToString(item.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, inboxWriteResponse{resp, count})
}

// DeleteInboxItem is human-only: deletion cannot be undone, unlike every other
// inbox change, so agents acting for the owner may archive but not delete.
func (h *Handler) DeleteInboxItem(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "deleting notifications is only available to human actors")
		return
	}
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

	count := changedCount(!prev.Archived)
	// Archive all sibling inbox items for the same issue (issue-level archive)
	if item.IssueID.Valid {
		siblings, _ := h.Queries.ArchiveInboxByIssue(r.Context(), db.ArchiveInboxByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
		})
		count += siblings
	}

	logAgentInboxWrite(r, "archive", "item_id", uuidToString(item.ID), "issue_id", uuidToPtr(item.IssueID), "count", count)
	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxArchived, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"issue_id":     uuidToPtr(item.IssueID),
		"recipient_id": uuidToString(item.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, inboxWriteResponse{resp, count})
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

	count := changedCount(prev.Archived)
	// Restore all sibling inbox items for the same issue (issue-level restore).
	if item.IssueID.Valid {
		siblings, _ := h.Queries.UnarchiveInboxByIssue(r.Context(), db.UnarchiveInboxByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
		})
		count += siblings
	}

	logAgentInboxWrite(r, "unarchive", "item_id", uuidToString(item.ID), "issue_id", uuidToPtr(item.IssueID), "count", count)
	userID := requestUserID(r)
	workspaceID := uuidToString(item.WorkspaceID)
	h.publish(protocol.EventInboxUnarchived, workspaceID, "member", userID, map[string]any{
		"item_id":      uuidToString(item.ID),
		"issue_id":     uuidToPtr(item.IssueID),
		"recipient_id": uuidToString(item.RecipientID),
	})

	resp := h.enrichInboxResponse(r.Context(), inboxToResponse(item), item.IssueID)
	writeJSON(w, http.StatusOK, inboxWriteResponse{resp, count})
}

func (h *Handler) CountUnreadInbox(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireInboxOwnerAccess(w, r, userID, wsUUID) {
		return
	}

	hidden, ok := h.requestInboxHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	count, err := runInboxRead(r.Context(), h, "unread_count", func(ctx context.Context, q *db.Queries) (int64, error) {
		return q.CountUnreadInbox(ctx, db.CountUnreadInboxParams{
			WorkspaceID:      wsUUID,
			RecipientType:    "member",
			RecipientID:      parseUUID(userID),
			HiddenProjectIds: hidden.Param(),
		})
	})
	if err != nil {
		writeInboxReadError(w, err, "failed to count unread inbox")
		return
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
	r, cancel := h.inboxReadRequest(r)
	defer cancel()
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// The summary ignores the active workspace, but an agent's access is
	// verified against the workspace its task runs in.
	if r.Header.Get("X-Actor-Source") != "" {
		wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
		if !ok || !h.requireInboxOwnerAccess(w, r, userID, wsUUID) {
			return
		}
	}

	value, err := h.inboxReadGuard().Summary(r.Context(), userID, func(ctx context.Context) (any, error) {
		return runInboxRead(ctx, h, "summary", func(ctx context.Context, q *db.Queries) ([]db.CountUnreadInboxByWorkspaceRow, error) {
			return q.CountUnreadInboxByWorkspace(ctx, parseUUID(userID))
		})
	})
	if err != nil {
		writeInboxReadError(w, err, "failed to summarize unread inbox")
		return
	}

	rows := value.([]db.CountUnreadInboxByWorkspaceRow)
	resp := make([]InboxWorkspaceUnreadResponse, len(rows))
	for i, row := range rows {
		resp[i] = InboxWorkspaceUnreadResponse{
			WorkspaceID: uuidToString(row.WorkspaceID),
			Count:       row.Count,
		}
	}

	writeJSON(w, http.StatusOK, resp)
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
	if !h.requireInboxOwnerAccess(w, r, userID, wsUUID) {
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
	logAgentInboxWrite(r, "mark_all_read", "count", count)
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
	if !h.requireInboxOwnerAccess(w, r, userID, wsUUID) {
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
	logAgentInboxWrite(r, "read_issue", "issue_id", uuidToString(issueUUID), "count", count)

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
	if !h.requireInboxOwnerAccess(w, r, userID, wsUUID) {
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
	logAgentInboxWrite(r, "archive_all", "count", count)
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
	if !h.requireInboxOwnerAccess(w, r, userID, wsUUID) {
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
	logAgentInboxWrite(r, "archive_all_read", "count", count)
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
	if !h.requireInboxOwnerAccess(w, r, userID, wsUUID) {
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
	logAgentInboxWrite(r, "archive_completed", "count", count)
	h.publish(protocol.EventInboxBatchArchived, workspaceID, "member", userID, map[string]any{
		"recipient_id": userID,
		"count":        count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}
