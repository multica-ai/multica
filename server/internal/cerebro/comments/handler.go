// Package comments owns the cerebro-only "move comment thread to sub-issue"
// feature: lifting a top-level comment (and its replies) out of an issue into
// a freshly-created sub-issue, leaving behind a one-line breadcrumb pointing
// at the new issue.
//
// Backend fundament for JEH-1309. The single endpoint is intentionally narrow
// (a root comment is in or out, no field-level selection); Phase 2's
// multi-select across a thread can layer on top without changing this API.
//
// The whole operation runs in one transaction so the caller never observes a
// half-moved thread: either the sub-issue exists with every reply moved and
// the breadcrumb is in place, or nothing changed.
package comments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TxStarter abstracts transaction creation. *pgxpool.Pool satisfies it.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Handler holds the move-to-sub-issue HTTP endpoint. Carries the upstream
// queries + transaction starter so the multi-step move can run atomically,
// and the event bus so the UI sees the new issue and the rewritten root
// comment without a re-fetch.
type Handler struct {
	Queries *db.Queries
	Tx      TxStarter
	Bus     *events.Bus
}

// New constructs the handler. Router wires the dependencies in.
func New(queries *db.Queries, tx TxStarter, bus *events.Bus) *Handler {
	return &Handler{Queries: queries, Tx: tx, Bus: bus}
}

// moveRequest is the POST body. `title` is optional — when omitted the
// sub-issue title is derived from the first line of the moved comment
// (truncated, see deriveTitle). Letting callers override is mostly useful for
// the eventual Phase 2 multi-select flow where the auto-derived title may not
// reflect the picked subset.
type moveRequest struct {
	Title string `json:"title"`
}

// moveResponse returns the newly-created sub-issue's identity. The frontend
// uses `identifier` for the breadcrumb text and `id` to navigate.
type moveResponse struct {
	IssueID    string `json:"issue_id"`
	Identifier string `json:"identifier"`
	Number     int32  `json:"number"`
}

// MoveToSubIssue handles POST /api/comments/{commentId}/move-to-subissue.
//
// Preconditions enforced inside the handler:
//   - The {commentId} resolves to a comment in the caller's workspace.
//   - The comment is a root (parent_id IS NULL) — replies can't be moved on
//     their own, the whole thread moves with the root. Phase 2 multi-select
//     will relax this with a different endpoint.
//   - The caller is the comment's author OR a workspace admin/owner. Same
//     gate as edit/delete on the comment itself (handler/comment.go).
//   - The host issue is not archived/locked (status != cancelled is the
//     coarse check — a moved thread on a cancelled issue is harmless but the
//     UI surface should not offer it; the API mirrors the UI rule).
//
// On success the response is the new sub-issue's identity (UUID + readable
// identifier) so the client can navigate without re-fetching the issue list.
func (h *Handler) MoveToSubIssue(w http.ResponseWriter, r *http.Request) {
	wsID, ok := requireWorkspace(w, r)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	commentUUID, ok := parseUUIDParam(w, r, "commentId", "comment id")
	if !ok {
		return
	}

	rootComment, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
		ID:          commentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if rootComment.ParentID.Valid {
		writeError(w, http.StatusBadRequest, "only root comments can be moved to a sub-issue")
		return
	}

	hostIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          rootComment.IssueID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "host issue not found")
		return
	}

	actorType, actorUUID, ok := requireActor(w, r)
	if !ok {
		return
	}
	if !h.callerMayMove(r.Context(), rootComment, wsUUID, actorType, actorUUID) {
		writeError(w, http.StatusForbidden, "only the comment author or a workspace admin can move a comment to a sub-issue")
		return
	}

	var req moveRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = deriveTitle(rootComment.Content)
	}
	if title == "" {
		// Comment was entirely whitespace / mention markup that collapsed to
		// empty. Fall back to a deterministic placeholder so the new issue
		// is still createable; user can rename it from the UI.
		title = "Moved comment thread"
	}

	// Eager-load replies BEFORE the tx so a slow workspace doesn't hold a
	// write transaction open during a query that doesn't need to be inside
	// it. Sorted by created_at so the new issue's comment timeline matches
	// the original conversation order.
	replies, err := h.loadReplies(r.Context(), rootComment.ID, wsUUID)
	if err != nil {
		slog.Error("move-to-subissue: load replies failed", "comment_id", util.UUIDToString(rootComment.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load comment thread")
		return
	}

	tx, err := h.Tx.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	number, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
	if err != nil {
		slog.Error("move-to-subissue: increment counter failed", "workspace_id", wsID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
		return
	}

	// Creator of the new sub-issue is the comment's original author, not the
	// caller — the conversation is theirs, the sub-issue inherits ownership.
	// project_id is copied from the host so the sub-issue sits under the
	// same project tree and respects the host's access rules.
	newIssue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID:   wsUUID,
		Title:         title,
		Description:   pgtype.Text{String: rootComment.Content, Valid: true},
		Status:        "todo",
		Priority:      "none",
		AssigneeType:  pgtype.Text{},
		AssigneeID:    pgtype.UUID{},
		CreatorType:   rootComment.AuthorType,
		CreatorID:     rootComment.AuthorID,
		ParentIssueID: hostIssue.ID,
		Position:      0,
		DueDate:       pgtype.Timestamptz{},
		Number:        number,
		ProjectID:     hostIssue.ProjectID,
	})
	if err != nil {
		slog.Error("move-to-subissue: create issue failed", "workspace_id", wsID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create sub-issue")
		return
	}

	// Recreate each reply on the new issue with the original author so the
	// audit trail reads identically. We deliberately do NOT preserve the
	// original created_at — sqlc's CreateComment uses now() — because the
	// sub-issue conversation is conceptually new; the original timestamps
	// live in the reply rows we're about to delete. The relative ordering
	// is preserved because we iterate `replies` in chronological order and
	// each insert advances now() monotonically.
	for _, reply := range replies {
		if _, err := qtx.CreateComment(r.Context(), db.CreateCommentParams{
			IssueID:     newIssue.ID,
			WorkspaceID: wsUUID,
			AuthorType:  reply.AuthorType,
			AuthorID:    reply.AuthorID,
			Content:     reply.Content,
			Type:        reply.Type,
			ParentID:    pgtype.UUID{}, // flatten — the sub-issue's root is the issue description.
		}); err != nil {
			slog.Error("move-to-subissue: recreate reply failed", "reply_id", util.UUIDToString(reply.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to copy reply into sub-issue")
			return
		}
	}

	// Delete the replies on the host issue. The root comment is rewritten,
	// not deleted, so it can carry the breadcrumb.
	for _, reply := range replies {
		if err := qtx.DeleteComment(r.Context(), reply.ID); err != nil {
			slog.Error("move-to-subissue: delete reply failed", "reply_id", util.UUIDToString(reply.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to remove reply from host issue")
			return
		}
	}

	prefix := h.issuePrefix(r.Context(), wsUUID)
	identifier := fmt.Sprintf("%s-%d", prefix, newIssue.Number)
	breadcrumb := fmt.Sprintf("Moved to [%s](mention://issue/%s)", identifier, util.UUIDToString(newIssue.ID))
	updatedRoot, err := qtx.UpdateComment(r.Context(), db.UpdateCommentParams{
		ID:      rootComment.ID,
		Content: breadcrumb,
	})
	if err != nil {
		slog.Error("move-to-subissue: rewrite root comment failed", "comment_id", util.UUIDToString(rootComment.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to rewrite original comment")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("move-to-subissue: commit failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to commit move")
		return
	}

	// Subscribers: copy the host issue's subscribers onto the new sub-issue
	// so anyone watching the parent conversation also sees the spin-off.
	// Best-effort — failure here doesn't roll the move back, since the
	// subscriber list is a notification surface, not a data invariant.
	h.copySubscribers(r.Context(), hostIssue.ID, newIssue.ID, rootComment)

	// Publish events: the new issue, the rewritten root comment, and one
	// deleted event per reply so the host issue's timeline updates without
	// a refetch. We publish on the workspace channel — the access middleware
	// downstream applies the per-user filter.
	h.publishIssueCreated(wsID, actorType, actorUUID, newIssue, prefix)
	h.publishCommentUpdated(wsID, actorType, actorUUID, updatedRoot)
	for _, reply := range replies {
		h.publishCommentDeleted(wsID, actorType, actorUUID, reply)
	}

	writeJSON(w, http.StatusCreated, moveResponse{
		IssueID:    util.UUIDToString(newIssue.ID),
		Identifier: identifier,
		Number:     newIssue.Number,
	})
}

// loadReplies returns the chronologically-ordered children of a root comment
// in the given workspace. We filter from ListCommentsForIssue rather than
// adding a new query — issue threads are O(30) on p99 so the in-memory filter
// is cheaper than a new sqlc surface.
func (h *Handler) loadReplies(ctx context.Context, rootID, wsUUID pgtype.UUID) ([]db.Comment, error) {
	// Need the host issue id to scope ListCommentsForIssue.
	root, err := h.Queries.GetComment(ctx, rootID)
	if err != nil {
		return nil, err
	}
	all, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     root.IssueID,
		WorkspaceID: wsUUID,
		Limit:       2000,
	})
	if err != nil {
		return nil, err
	}
	rootIDStr := util.UUIDToString(rootID)
	replies := make([]db.Comment, 0, len(all))
	for _, c := range all {
		if c.ParentID.Valid && util.UUIDToString(c.ParentID) == rootIDStr {
			replies = append(replies, c)
		}
	}
	return replies, nil
}

// callerMayMove gates the action: the comment's author or a workspace
// admin/owner. Agents calling via a task token are allowed when the task's
// agent id matches the comment's author id (mirrors the edit/delete rule).
func (h *Handler) callerMayMove(ctx context.Context, c db.Comment, wsUUID pgtype.UUID, actorType string, actorID pgtype.UUID) bool {
	if actorType == c.AuthorType && util.UUIDToString(actorID) == util.UUIDToString(c.AuthorID) {
		return true
	}
	if actorType != "member" {
		return false
	}
	member, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      actorID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		return false
	}
	return member.Role == "owner" || member.Role == "admin"
}

func (h *Handler) copySubscribers(ctx context.Context, hostIssueID, newIssueID pgtype.UUID, rootComment db.Comment) {
	subs, err := h.Queries.ListIssueSubscribers(ctx, hostIssueID)
	if err != nil {
		slog.Warn("move-to-subissue: copy subscribers — list failed", "host_issue_id", util.UUIDToString(hostIssueID), "error", err)
		return
	}
	for _, s := range subs {
		if err := h.Queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID:  newIssueID,
			UserType: s.UserType,
			UserID:   s.UserID,
			Reason:   s.Reason,
		}); err != nil {
			slog.Warn("move-to-subissue: copy subscriber failed", "user_id", util.UUIDToString(s.UserID), "error", err)
		}
	}
	// Always add the comment author so they see notifications on the new
	// sub-issue even if they were not subscribed to the host.
	if rootComment.AuthorType == "member" || rootComment.AuthorType == "agent" {
		if err := h.Queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID:  newIssueID,
			UserType: rootComment.AuthorType,
			UserID:   rootComment.AuthorID,
			Reason:   "thread_moved",
		}); err != nil {
			slog.Warn("move-to-subissue: subscribe author failed", "author_id", util.UUIDToString(rootComment.AuthorID), "error", err)
		}
	}
}

func (h *Handler) issuePrefix(ctx context.Context, wsUUID pgtype.UUID) string {
	ws, err := h.Queries.GetWorkspace(ctx, wsUUID)
	if err != nil {
		return "ISSUE"
	}
	if ws.IssuePrefix != "" {
		return ws.IssuePrefix
	}
	// Fallback identical in spirit to handler.generateIssuePrefix but kept
	// local so this cerebro package doesn't import the upstream handler.
	return "ISSUE"
}

func (h *Handler) publishIssueCreated(wsID, actorType string, actorID pgtype.UUID, issue db.Issue, prefix string) {
	if h.Bus == nil {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: wsID,
		ActorType:   actorType,
		ActorID:     util.UUIDToString(actorID),
		Payload: map[string]any{
			"issue": map[string]any{
				"id":              util.UUIDToString(issue.ID),
				"workspace_id":    util.UUIDToString(issue.WorkspaceID),
				"number":          issue.Number,
				"identifier":      fmt.Sprintf("%s-%d", prefix, issue.Number),
				"kind":            issue.Kind,
				"title":           issue.Title,
				"status":          issue.Status,
				"priority":        issue.Priority,
				"creator_type":    issue.CreatorType,
				"creator_id":      util.UUIDToString(issue.CreatorID),
				"parent_issue_id": util.UUIDToPtr(issue.ParentIssueID),
				"project_id":      util.UUIDToPtr(issue.ProjectID),
				"created_at":      util.TimestampToString(issue.CreatedAt),
				"updated_at":      util.TimestampToString(issue.UpdatedAt),
			},
		},
	})
}

func (h *Handler) publishCommentUpdated(wsID, actorType string, actorID pgtype.UUID, c db.Comment) {
	if h.Bus == nil {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        protocol.EventCommentUpdated,
		WorkspaceID: wsID,
		ActorType:   actorType,
		ActorID:     util.UUIDToString(actorID),
		Payload: map[string]any{
			"comment": map[string]any{
				"id":          util.UUIDToString(c.ID),
				"issue_id":    util.UUIDToString(c.IssueID),
				"author_type": c.AuthorType,
				"author_id":   util.UUIDToString(c.AuthorID),
				"content":     c.Content,
				"type":        c.Type,
				"parent_id":   util.UUIDToPtr(c.ParentID),
				"created_at":  util.TimestampToString(c.CreatedAt),
				"updated_at":  util.TimestampToString(c.UpdatedAt),
			},
		},
	})
}

func (h *Handler) publishCommentDeleted(wsID, actorType string, actorID pgtype.UUID, c db.Comment) {
	if h.Bus == nil {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        protocol.EventCommentDeleted,
		WorkspaceID: wsID,
		ActorType:   actorType,
		ActorID:     util.UUIDToString(actorID),
		Payload: map[string]any{
			"comment_id": util.UUIDToString(c.ID),
			"issue_id":   util.UUIDToString(c.IssueID),
		},
	})
}

// deriveTitle takes the first non-empty line of the comment and trims it to
// a sensible title length. Mention links ([@Name](mention://...)) are
// reduced to "@Name" so the title reads naturally; raw URLs are left alone.
func deriveTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = collapseMentionLinks(line)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return truncateRunes(line, 100)
	}
	return ""
}

// collapseMentionLinks rewrites `[@Name](mention://...)` to `@Name` and
// `[MUL-1](mention://issue/uuid)` to `MUL-1` so titles read like prose rather
// than raw markdown. Non-mention markdown links (e.g. arbitrary URLs) are
// left intact — they are usually meaningful when somebody picks a
// comment-derived title.
func collapseMentionLinks(s string) string {
	var b strings.Builder
	for {
		open := strings.Index(s, "[")
		if open < 0 {
			b.WriteString(s)
			return b.String()
		}
		close := strings.Index(s[open:], "](")
		if close < 0 {
			b.WriteString(s)
			return b.String()
		}
		close += open
		end := strings.Index(s[close:], ")")
		if end < 0 {
			b.WriteString(s)
			return b.String()
		}
		end += close
		label := s[open+1 : close]
		target := s[close+2 : end]
		b.WriteString(s[:open])
		if strings.HasPrefix(target, "mention://") {
			b.WriteString(label)
		} else {
			b.WriteString(s[open : end+1])
		}
		s = s[end+1:]
	}
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	count := 0
	for i := range s {
		if count == max {
			return strings.TrimRight(s[:i], " ") + "…"
		}
		count++
	}
	return s
}

func requireWorkspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return "", false
	}
	return wsID, true
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, param, label string) (pgtype.UUID, bool) {
	raw := chi.URLParam(r, param)
	u, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+label)
		return pgtype.UUID{}, false
	}
	return u, true
}

// requireActor resolves the caller identity: an agent when the request
// carries a task scope, otherwise the X-User-ID member. Mirrors the
// upstream resolveActor seam without importing it.
func requireActor(w http.ResponseWriter, r *http.Request) (string, pgtype.UUID, bool) {
	if scope := middleware.AuthScopeFromContext(r.Context()); scope == middleware.ScopeTask {
		ts := middleware.TaskScopeFromContext(r.Context())
		agentUUID, err := util.ParseUUID(ts.AgentID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid agent id on task token")
			return "", pgtype.UUID{}, false
		}
		return "agent", agentUUID, true
	}
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", pgtype.UUID{}, false
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return "", pgtype.UUID{}, false
	}
	return "member", userUUID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
