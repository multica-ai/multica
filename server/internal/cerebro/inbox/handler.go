// Package inbox holds cerebro-only HTTP handlers for the inbox feature
// (active issue tasks / mute / unmute / mark-unread). They live here so upstream merges of the
// upstream inbox handler don't conflict on cerebro-only routes.
package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// flagCommentReminders is the per-user cerebro feature flag that gates the
// comment-reminder capability (FIR-2641). It mirrors the registry key in
// packages/cerebro-feature-flags/registry.ts. Default-on: a reminder that
// references a comment_id is rejected only when the user has an explicit
// disabling override — flipping the flag off is the no-redeploy kill switch.
const flagCommentReminders = "cerebro_comment_reminders"

// reminderTextMaxRunes bounds the auto-suggested reminder text derived from a
// comment so a long comment body does not blow up the inbox row.
const reminderTextMaxRunes = 140

// Inbox metadata WS event types. They carry the `inbox:` prefix so the client's
// generic realtime refreshMap (which fires onInboxInvalidate for any inbox:*
// event other than inbox:new) re-fetches the inbox list + unread counts on
// every connected tab/device. Without these, a mute/unmute/mark-unread/
// unarchive done in one session left other sessions' unread badge stale until
// a manual refresh (FIR-2394).
const (
	eventInboxMuted      = "inbox:muted"
	eventInboxUnmuted    = "inbox:unmuted"
	eventInboxUnread     = "inbox:unread"
	eventInboxUnarchived = "inbox:unarchived"
	// eventInboxNew fires when a new item is manually inserted via the handler.
	// listeners.go routes the WS message to the recipient's open sessions.
	eventInboxNew = "inbox:new"
)

// Handler exposes the cerebro-only inbox HTTP endpoints. Carries both the
// upstream sqlc Queries (for the workspace-scoped item lookup that enforces
// access control) and the cerebro Queries (for the new mute/unread mutations).
type Handler struct {
	Upstream *db.Queries
	Cerebro  *cerebrodb.Queries
	// Bus publishes inbox metadata events so other sessions refresh in
	// realtime. Nil-safe: when unset (e.g. older tests) publishing is skipped.
	Bus *events.Bus
	// Tasks enqueues the agent run when the owner accepts a
	// private_agent_run_request (FIR-2385). nil disables only that endpoint.
	Tasks *service.TaskService
}

// New constructs the handler. The router wires both query packages, the event
// bus, and the task service in (bus may be nil in tests that don't exercise
// realtime fan-out).
func New(upstream *db.Queries, cerebro *cerebrodb.Queries, bus *events.Bus, tasks *service.TaskService) *Handler {
	return &Handler{Upstream: upstream, Cerebro: cerebro, Bus: bus, Tasks: tasks}
}

// publishInboxEvent fans an inbox metadata change out to the recipient's other
// sessions. Workspace-scoped broadcast mirrors the upstream inbox handler;
// each client invalidates only its own inbox cache on receipt.
func (h *Handler) publishInboxEvent(eventType string, item cerebrodb.InboxItem) {
	if h.Bus == nil {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(item.WorkspaceID),
		ActorType:   "member",
		ActorID:     util.UUIDToString(item.RecipientID),
		Payload: map[string]any{
			"item_id":      util.UUIDToString(item.ID),
			"recipient_id": util.UUIDToString(item.RecipientID),
		},
	})
}

// inboxItemResponse is the JSON shape returned by mute / unmute / unread.
// Matches the cerebro-only fields the frontend cares about; the row is
// re-fetched in full via the existing list query, this is just the ack of
// the mutation.
type inboxItemResponse struct {
	ID         string  `json:"id"`
	Read       bool    `json:"read"`
	Archived   bool    `json:"archived"`
	MutedUntil *string `json:"muted_until"`
}

type createReminderRequest struct {
	Text      string  `json:"text"`
	PlannedAt string  `json:"planned_at"`
	IssueID   *string `json:"issue_id"`
	// CommentID makes the reminder point at one specific comment (FIR-2641).
	// When set, clicking the fired reminder opens the issue and scrolls to that
	// comment — the inbox deep-link reads details.comment_id. text may be left
	// empty: it is then auto-suggested from the comment body.
	CommentID *string `json:"comment_id"`
}

// MuteInboxItem mutes an inbox item until the timestamp supplied in the
// request body. The timestamp is computed client-side ("next 08:00 in user
// local time" by default) so the server stays timezone-agnostic.
//
// Body: {"muted_until": "2026-05-08T08:00:00+02:00"}
func (h *Handler) MuteInboxItem(w http.ResponseWriter, r *http.Request) {
	itemID, ok := h.loadItemID(w, r)
	if !ok {
		return
	}
	var body struct {
		MutedUntil string `json:"muted_until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	parsed, err := time.Parse(time.RFC3339, body.MutedUntil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "muted_until must be RFC3339")
		return
	}
	if !parsed.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "muted_until must be in the future")
		return
	}
	item, err := h.Cerebro.SetInboxMutedUntil(r.Context(), cerebrodb.SetInboxMutedUntilParams{
		ID:         itemID,
		MutedUntil: pgtype.Timestamptz{Time: parsed, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mute inbox item")
		return
	}
	if item.IssueID.Valid {
		_, _ = h.Cerebro.SetInboxMutedUntilByIssue(r.Context(), cerebrodb.SetInboxMutedUntilByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
			MutedUntil:    pgtype.Timestamptz{Time: parsed, Valid: true},
		})
	}
	h.publishInboxEvent(eventInboxMuted, item)
	writeJSON(w, http.StatusOK, toResponse(item))
}

// UnmuteInboxItem clears the muted_until timestamp.
func (h *Handler) UnmuteInboxItem(w http.ResponseWriter, r *http.Request) {
	itemID, ok := h.loadItemID(w, r)
	if !ok {
		return
	}
	item, err := h.Cerebro.ClearInboxMute(r.Context(), itemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmute inbox item")
		return
	}
	if item.IssueID.Valid {
		_, _ = h.Cerebro.ClearInboxMuteByIssue(r.Context(), cerebrodb.ClearInboxMuteByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
		})
	}
	h.publishInboxEvent(eventInboxUnmuted, item)
	writeJSON(w, http.StatusOK, toResponse(item))
}

// MarkInboxUnread sets read=false on an inbox item. Counterpart to the
// upstream POST /api/inbox/{id}/read; we add a separate cerebro endpoint
// rather than patch the upstream one so the path stays cerebro-only and
// merges stay trivial.
func (h *Handler) MarkInboxUnread(w http.ResponseWriter, r *http.Request) {
	itemID, ok := h.loadItemID(w, r)
	if !ok {
		return
	}
	item, err := h.Cerebro.SetInboxUnread(r.Context(), itemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark inbox item unread")
		return
	}
	h.publishInboxEvent(eventInboxUnread, item)
	writeJSON(w, http.StatusOK, toResponse(item))
}

// UnarchiveInboxItem restores an archived inbox item to the active inbox.
// Mirror of the upstream POST /api/inbox/{id}/archive. Lives in the cerebro
// package so the route stays cerebro-only and upstream merges don't touch it.
// Issue-level: if the item has an issue_id we unarchive every sibling row for
// the same recipient + issue, mirroring ArchiveInboxByIssue.
func (h *Handler) UnarchiveInboxItem(w http.ResponseWriter, r *http.Request) {
	itemID, ok := h.loadItemID(w, r)
	if !ok {
		return
	}
	item, err := h.Cerebro.UnarchiveInboxItem(r.Context(), itemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unarchive inbox item")
		return
	}
	if item.IssueID.Valid {
		_, _ = h.Cerebro.UnarchiveInboxByIssue(r.Context(), cerebrodb.UnarchiveInboxByIssueParams{
			WorkspaceID:   item.WorkspaceID,
			RecipientType: item.RecipientType,
			RecipientID:   item.RecipientID,
			IssueID:       item.IssueID,
		})
	}
	h.publishInboxEvent(eventInboxUnarchived, item)
	writeJSON(w, http.StatusOK, toResponse(item))
}

// CreateReminder creates a personal reminder by inserting a muted inbox row.
// The row is visible in the Muted/Snooze inbox view until planned_at, then it
// automatically becomes active because the regular inbox filters stop treating
// muted_until as muted once the timestamp has passed.
func (h *Handler) CreateReminder(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	var body createReminderRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	plannedAt, err := time.Parse(time.RFC3339, body.PlannedAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "planned_at must be RFC3339")
		return
	}
	if !plannedAt.After(time.Now()) {
		writeError(w, http.StatusBadRequest, "planned_at must be in the future")
		return
	}
	recipientID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	// Resolve an optional comment reference first (FIR-2641): it pins the
	// reminder to one comment, fixes the issue the reminder belongs to, and can
	// supply the auto-suggested text. Gated by the per-user
	// cerebro_comment_reminders flag so it can be switched off without redeploy.
	var (
		comment      db.Comment
		commentIDStr string
		haveComment  bool
	)
	if body.CommentID != nil && strings.TrimSpace(*body.CommentID) != "" {
		if !h.commentRemindersEnabled(r.Context(), wsUUID, recipientID) {
			writeError(w, http.StatusForbidden, "comment reminders are disabled")
			return
		}
		parsedCommentID, err := util.ParseUUID(strings.TrimSpace(*body.CommentID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid comment_id")
			return
		}
		comment, err = h.Upstream.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          parsedCommentID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "comment not found")
			return
		}
		commentIDStr = util.UUIDToString(parsedCommentID)
		haveComment = true
	}

	// Resolve the issue. A comment reference fixes the issue to the comment's
	// own issue; an explicit issue_id, if also supplied, must match it.
	var issueID pgtype.UUID
	title := "Reminder"
	switch {
	case haveComment:
		if body.IssueID != nil && strings.TrimSpace(*body.IssueID) != "" {
			parsedIssueID, err := util.ParseUUID(strings.TrimSpace(*body.IssueID))
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid issue_id")
				return
			}
			if util.UUIDToString(parsedIssueID) != util.UUIDToString(comment.IssueID) {
				writeError(w, http.StatusBadRequest, "comment_id does not belong to issue_id")
				return
			}
		}
		issue, err := h.Upstream.GetIssue(r.Context(), comment.IssueID)
		if err != nil || util.UUIDToString(issue.WorkspaceID) != wsID {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		issueID = comment.IssueID
		title = "Reminder: " + issue.Title
	case body.IssueID != nil && strings.TrimSpace(*body.IssueID) != "":
		parsedIssueID, err := util.ParseUUID(strings.TrimSpace(*body.IssueID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue_id")
			return
		}
		issue, err := h.Upstream.GetIssue(r.Context(), parsedIssueID)
		if err != nil || util.UUIDToString(issue.WorkspaceID) != wsID {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		issueID = parsedIssueID
		title = "Reminder: " + issue.Title
	}

	// Reminder text: caller-supplied wins; when blank and a comment is
	// referenced, auto-suggest a snippet from the comment body. The UI pre-fills
	// the same suggestion and lets the user edit it before saving.
	text := strings.TrimSpace(body.Text)
	if text == "" && haveComment {
		text = suggestReminderText(comment.Content)
	}
	if text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	planned := pgtype.Timestamptz{Time: plannedAt, Valid: true}
	detailsMap := map[string]string{
		// CEREBRO-PATCH(inbox-reminders-due): due sweeper only claims reminders explicitly marked pending.
		"due_pending": "true",
		"planned_at":  plannedAt.Format(time.RFC3339),
		"text":        text,
	}
	if haveComment {
		// The inbox deep-link (packages/views/inbox) reads details.comment_id to
		// scroll the opened issue to this exact comment.
		detailsMap["comment_id"] = commentIDStr
	}
	details, _ := json.Marshal(detailsMap)
	existing, err := h.Cerebro.FindPendingReminder(r.Context(), cerebrodb.FindPendingReminderParams{
		WorkspaceID: wsUUID,
		RecipientID: recipientID,
		Column3:     issueID,
		Title:       title,
		Body:        pgtype.Text{String: text, Valid: true},
		Column6:     commentIDStr,
	})
	if err == nil {
		item, err := h.Cerebro.UpdateReminderInboxItem(r.Context(), cerebrodb.UpdateReminderInboxItemParams{
			ID:         existing.ID,
			MutedUntil: planned,
			Details:    details,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update reminder")
			return
		}
		writeJSON(w, http.StatusOK, toResponse(item))
		return
	}

	item, err := h.Cerebro.CreateReminderInboxItem(r.Context(), cerebrodb.CreateReminderInboxItemParams{
		WorkspaceID: wsUUID,
		RecipientID: recipientID,
		IssueID:     issueID,
		Title:       title,
		Body:        pgtype.Text{String: text, Valid: true},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     recipientID,
		Details:     details,
		MutedUntil:  planned,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create reminder")
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(item))
}

// commentRemindersEnabled resolves the per-user cerebro_comment_reminders flag.
// Default-on: only an explicit disabling override returns false. A missing row
// (ErrNoRows) or a transient store error keeps the feature enabled — the flag
// is an opt-out kill switch, not an opt-in gate.
func (h *Handler) commentRemindersEnabled(ctx context.Context, wsUUID, userUUID pgtype.UUID) bool {
	enabled, err := h.Cerebro.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
		FlagKey:     flagCommentReminders,
	})
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return true
	}
	return enabled
}

// suggestReminderText turns a comment body into a one-line reminder suggestion:
// whitespace collapsed, trimmed, and truncated to reminderTextMaxRunes with an
// ellipsis. Falls back to a generic phrase for an empty/whitespace comment.
func suggestReminderText(content string) string {
	snippet := strings.Join(strings.Fields(content), " ")
	if snippet == "" {
		return "Reminder about this comment"
	}
	runes := []rune(snippet)
	if len(runes) > reminderTextMaxRunes {
		return strings.TrimSpace(string(runes[:reminderTextMaxRunes])) + "…"
	}
	return snippet
}

// notificationItemResponse mirrors handler.InboxItemResponse so the
// notifications page consumes the same shape as the inbox feed. Kept local
// to the cerebro package so this file does not import the upstream handler.
type notificationItemResponse struct {
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
	MutedUntil    *string         `json:"muted_until"`
	CreatedAt     string          `json:"created_at"`
	IssueStatus   *string         `json:"issue_status"`
	ActorType     *string         `json:"actor_type"`
	ActorID       *string         `json:"actor_id"`
	Details       json.RawMessage `json:"details"`
}

func notificationsRowToResponse(r db.ListNotificationsItemsRow) notificationItemResponse {
	return notificationItemResponse{
		ID:            util.UUIDToString(r.ID),
		WorkspaceID:   util.UUIDToString(r.WorkspaceID),
		RecipientType: r.RecipientType,
		RecipientID:   util.UUIDToString(r.RecipientID),
		Type:          r.Type,
		Severity:      r.Severity,
		Route:         r.Route,
		IssueID:       uuidPtr(r.IssueID),
		ProjectID:     uuidPtr(r.ProjectID),
		Title:         r.Title,
		Body:          textPtr(r.Body),
		Read:          r.Read,
		Archived:      r.Archived,
		MutedUntil:    timestampPtr(r.MutedUntil),
		CreatedAt:     r.CreatedAt.Time.Format(time.RFC3339Nano),
		IssueStatus:   textPtr(r.IssueStatus),
		ActorType:     textPtr(r.ActorType),
		ActorID:       uuidPtr(r.ActorID),
		Details:       json.RawMessage(r.Details),
	}
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := util.UUIDToString(u)
	return &s
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

func timestampPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.Format(time.RFC3339Nano)
	return &s
}

// ListNotifications returns the user's non-archived notifications-routed inbox
// items in the current workspace. Mirrors the upstream ListInbox handler but
// uses the route='notifications' filter. The SQL and the route flag are
// cerebro-only (see CEREBRO-PATCH(cerebro-inbox-fields)), so the handler lives
// here rather than in the upstream inbox package.
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUID(w, r)
	if !ok {
		return
	}
	recipientID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	items, err := h.Upstream.ListNotificationsItems(r.Context(), db.ListNotificationsItemsParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   recipientID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}
	resp := make([]notificationItemResponse, len(items))
	for i, item := range items {
		resp[i] = notificationsRowToResponse(item)
	}
	writeJSON(w, http.StatusOK, resp)
}

// CountUnreadNotifications returns the unread, non-muted notification count
// in the current workspace.
func (h *Handler) CountUnreadNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUID(w, r)
	if !ok {
		return
	}
	recipientID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	count, err := h.Upstream.CountUnreadNotifications(r.Context(), db.CountUnreadNotificationsParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   recipientID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread notifications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

// MarkAllNotificationsRead flips every unread notification in the workspace
// to read for the calling user. Returns the count of affected rows.
func (h *Handler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUID(w, r)
	if !ok {
		return
	}
	recipientID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	count, err := h.Upstream.MarkAllNotificationsRead(r.Context(), db.MarkAllNotificationsReadParams{
		WorkspaceID: wsUUID,
		RecipientID: recipientID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark all notifications read")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

// ArchiveAllNotifications archives every active notification in the workspace
// for the calling user. Returns the count of affected rows.
func (h *Handler) ArchiveAllNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUID(w, r)
	if !ok {
		return
	}
	recipientID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	count, err := h.Upstream.ArchiveAllNotifications(r.Context(), db.ArchiveAllNotificationsParams{
		WorkspaceID: wsUUID,
		RecipientID: recipientID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive all notifications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

// AddIssueToInbox manually places an issue into the member's inbox.
// Body: {"issue_id": "<uuid>"}
//
// Idempotent: if the member already has an active (non-archived) manually_added
// item for this issue, the existing item is marked unread so it resurfaces
// without creating a duplicate row.
func (h *Handler) AddIssueToInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := workspaceUUID(w, r)
	if !ok {
		return
	}
	recipientID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var body struct {
		IssueID string `json:"issue_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.IssueID) == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	issueUUID, err := util.ParseUUID(strings.TrimSpace(body.IssueID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue_id")
		return
	}

	// Validate the issue belongs to the workspace and retrieve its title.
	issue, err := h.Upstream.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	// Dedup: if an active manually_added item already exists, mark it unread so
	// it resurfaces in the feed without duplicating the row.
	existing, err := h.Cerebro.FindManualInboxItem(r.Context(), cerebrodb.FindManualInboxItemParams{
		WorkspaceID: wsUUID,
		RecipientID: recipientID,
		IssueID:     issueUUID,
	})
	if err == nil {
		updated, err := h.Cerebro.SetInboxUnread(r.Context(), existing.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update inbox item")
			return
		}
		h.publishInboxEvent(eventInboxUnread, updated)
		writeJSON(w, http.StatusOK, toResponse(updated))
		return
	}

	// No active item — create one.
	item, err := h.Cerebro.CreateManualInboxItem(r.Context(), cerebrodb.CreateManualInboxItemParams{
		WorkspaceID: wsUUID,
		RecipientID: recipientID,
		IssueID:     issueUUID,
		Title:       issue.Title,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create inbox item")
		return
	}
	h.publishNewItem(item)
	writeJSON(w, http.StatusCreated, toResponse(item))
}

// publishNewItem broadcasts inbox:new for a freshly inserted item so real-time
// listeners in listeners.go push it to the recipient's open sessions.
// The payload shape matches what listeners.go expects: payload["item"]["recipient_id"]
// routes the WS message to the correct connected client.
func (h *Handler) publishNewItem(item cerebrodb.InboxItem) {
	if h.Bus == nil {
		return
	}
	recipientID := util.UUIDToString(item.RecipientID)
	h.Bus.Publish(events.Event{
		Type:        eventInboxNew,
		WorkspaceID: util.UUIDToString(item.WorkspaceID),
		ActorType:   "member",
		ActorID:     recipientID,
		Payload: map[string]any{
			"item": map[string]any{
				"id":           util.UUIDToString(item.ID),
				"recipient_id": recipientID,
				"type":         item.Type,
				"title":        item.Title,
				"route":        item.Route,
			},
		},
	})
}

func workspaceUUID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, false
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	return wsUUID, true
}

// ListActiveIssueTasks returns issue IDs in the current workspace that have
// in-flight tasks. Drives the "agent is working" indicator on inbox rows.
func (h *Handler) ListActiveIssueTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	tasks, err := h.Upstream.ListActiveIssueTaskStatusesInWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list active issue tasks")
		return
	}

	ids := make([]string, 0, len(tasks))
	out := make([]map[string]string, 0, len(tasks))
	for _, task := range tasks {
		issueID := util.UUIDToString(task.IssueID)
		ids = append(ids, issueID)
		row := map[string]string{
			"issue_id": issueID,
			"status":   task.Status,
		}
		// FIR-2326: parent_issue_id lets the inbox surface a running sub-issue
		// on its parent row (orange "sub-issue is running" pip + Running bucket).
		if task.ParentIssueID.Valid {
			row["parent_issue_id"] = util.UUIDToString(task.ParentIssueID)
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_ids": ids,
		"tasks":     out,
	})
}

func toResponse(i cerebrodb.InboxItem) inboxItemResponse {
	resp := inboxItemResponse{
		ID:       util.UUIDToString(i.ID),
		Read:     i.Read,
		Archived: i.Archived,
	}
	if i.MutedUntil.Valid {
		s := i.MutedUntil.Time.Format(time.RFC3339)
		resp.MutedUntil = &s
	}
	return resp
}

// loadItemID resolves and authorizes the inbox item id from the URL. On any
// failure it writes the appropriate HTTP response and returns ok=false.
//
// Mirrors handler.loadInboxItemForUser but reimplemented here so this package
// stays free of the main handler's dependency surface.
func (h *Handler) loadItemID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, false
	}
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return pgtype.UUID{}, false
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return pgtype.UUID{}, false
	}
	itemUUID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid inbox item id")
		return pgtype.UUID{}, false
	}
	item, err := h.Upstream.GetInboxItemInWorkspace(r.Context(), db.GetInboxItemInWorkspaceParams{
		ID:          itemUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "inbox item not found")
		return pgtype.UUID{}, false
	}
	if item.RecipientType != "member" || util.UUIDToString(item.RecipientID) != userID {
		writeError(w, http.StatusForbidden, "not your inbox item")
		return pgtype.UUID{}, false
	}
	return item.ID, true
}

// requireUserID mirrors handler.requireUserID — kept private so this package
// compiles without importing the upstream handler package.
func requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", false
	}
	return userID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
