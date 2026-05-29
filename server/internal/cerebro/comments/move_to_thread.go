// This file owns the cerebro-only "move comments to a new thread" feature:
// lifting a hand-picked set of comments out of one thread into a freshly
// created thread on the SAME issue, leaving a breadcrumb on each moved comment
// pointing at the new thread.
//
// It is the multi-select sibling of move_to_subissue.go (JEH-1309). Where the
// sub-issue flow takes a whole root thread out of the issue, this flow lets the
// user cherry-pick comments inside a thread and spin them into a side thread
// that stays on the issue — the entry point is the per-comment "Reply in new
// thread" action.
//
// The whole operation runs in one transaction so the caller never observes a
// half-moved set: either the new thread exists with every picked comment copied
// in order and every original rewritten to a breadcrumb, or nothing changed.
package comments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// moveToThreadRequest is the POST body. `comment_ids` is the ordered set the
// user picked in the UI; the backend re-derives chronological order itself, so
// client ordering is advisory only.
type moveToThreadRequest struct {
	CommentIDs []string `json:"comment_ids"`
}

// moveToThreadResponse returns the identity of the new thread so the client can
// scroll to it without a refetch. `moved_count` is the number of comments that
// changed threads (== number of breadcrumbs left behind).
type moveToThreadResponse struct {
	RootCommentID string `json:"root_comment_id"`
	IssueID       string `json:"issue_id"`
	MovedCount    int    `json:"moved_count"`
}

// MoveToThread handles POST /api/comments/move-to-thread.
//
// Preconditions enforced inside the handler:
//   - Every id in `comment_ids` resolves to a comment in the caller's workspace.
//   - All picked comments live on the SAME issue (a new thread is created on
//     that issue; merging comments across issues is out of scope).
//   - The caller is a workspace admin/owner OR the author of every picked
//     comment. A non-admin can only move comments they wrote — same spirit as
//     the per-comment edit/delete gate.
//   - The host issue is not cancelled (mirrors the UI surface, which hides the
//     action on cancelled issues).
//
// On success the picked comments are copied into a new thread on the same issue
// (oldest pick = new root, the rest become its replies in chronological order)
// and each original is rewritten to a one-line breadcrumb linking to the new
// thread.
func (h *Handler) MoveToThread(w http.ResponseWriter, r *http.Request) {
	wsID, ok := requireWorkspace(w, r)
	if !ok {
		return
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	var req moveToThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ids, err := dedupeUUIDs(req.CommentIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "comment_ids is required")
		return
	}

	// Resolve every picked comment in the caller's workspace and confirm they
	// share one issue. Collected in input order; we re-sort by created_at below.
	picked := make([]db.Comment, 0, len(ids))
	var issueID pgtype.UUID
	for i, id := range ids {
		c, err := h.Queries.GetCommentInWorkspace(r.Context(), db.GetCommentInWorkspaceParams{
			ID:          id,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "comment not found")
			return
		}
		if i == 0 {
			issueID = c.IssueID
		} else if util.UUIDToString(c.IssueID) != util.UUIDToString(issueID) {
			writeError(w, http.StatusBadRequest, "all comments must be on the same issue")
			return
		}
		picked = append(picked, c)
	}

	hostIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "host issue not found")
		return
	}
	if hostIssue.Status == "cancelled" {
		writeError(w, http.StatusBadRequest, "cannot move comments on a cancelled issue")
		return
	}

	actorType, actorUUID, ok := requireActor(w, r)
	if !ok {
		return
	}
	if !h.callerMayMoveAll(r.Context(), picked, wsUUID, actorType, actorUUID) {
		writeError(w, http.StatusForbidden, "only the comments' author or a workspace admin can move them to a new thread")
		return
	}

	// Chronological order is the source of truth for the new thread: oldest
	// pick becomes the root, the rest hang under it in created order. Tie-break
	// on id so the order is deterministic when timestamps collide.
	sort.SliceStable(picked, func(a, b int) bool {
		ta, tb := picked[a].CreatedAt.Time, picked[b].CreatedAt.Time
		if ta.Equal(tb) {
			return util.UUIDToString(picked[a].ID) < util.UUIDToString(picked[b].ID)
		}
		return ta.Before(tb)
	})

	tx, err := h.Tx.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Create the new root from the oldest pick, preserving its author so the
	// new thread reads with the original attribution. parent_id is null — this
	// is a brand-new top-level thread on the same issue.
	root := picked[0]
	newRoot, err := qtx.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issueID,
		WorkspaceID: wsUUID,
		AuthorType:  root.AuthorType,
		AuthorID:    root.AuthorID,
		Content:     root.Content,
		Type:        root.Type,
		ParentID:    pgtype.UUID{},
	})
	if err != nil {
		slog.Error("move-to-thread: create root failed", "comment_id", util.UUIDToString(root.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create new thread")
		return
	}

	// Recreate the remaining picks as replies under the new root, in order.
	// Each insert advances now() so the relative order is preserved.
	created := []db.Comment{newRoot}
	for _, c := range picked[1:] {
		reply, err := qtx.CreateComment(r.Context(), db.CreateCommentParams{
			IssueID:     issueID,
			WorkspaceID: wsUUID,
			AuthorType:  c.AuthorType,
			AuthorID:    c.AuthorID,
			Content:     c.Content,
			Type:        c.Type,
			ParentID:    newRoot.ID,
		})
		if err != nil {
			slog.Error("move-to-thread: create reply failed", "comment_id", util.UUIDToString(c.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to copy comment into new thread")
			return
		}
		created = append(created, reply)
	}

	// Rewrite every original pick to a breadcrumb pointing at the new thread.
	// The originals stay in place (not deleted) so the trail is visible exactly
	// where each moved comment used to be. mention://comment/<id> is resolved
	// by the frontend to an in-issue scroll-to-comment link.
	// Link-only breadcrumb: the bracket text is the plain-text fallback (CLI,
	// notifications, raw markdown); the rich UI swaps it for a localized
	// "jump to new thread" affordance via the mention://comment renderer.
	breadcrumb := fmt.Sprintf("[Moved to new thread ↗](mention://comment/%s)", util.UUIDToString(newRoot.ID))
	updated := make([]db.Comment, 0, len(picked))
	for _, c := range picked {
		u, err := qtx.UpdateComment(r.Context(), db.UpdateCommentParams{
			ID:      c.ID,
			Content: breadcrumb,
		})
		if err != nil {
			slog.Error("move-to-thread: rewrite original failed", "comment_id", util.UUIDToString(c.ID), "error", err)
			writeError(w, http.StatusInternalServerError, "failed to rewrite original comment")
			return
		}
		updated = append(updated, u)
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("move-to-thread: commit failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to commit move")
		return
	}

	// Publish so every connected client refreshes the timeline without a
	// refetch: the new thread's comments as created, the rewritten originals as
	// updated. Workspace channel; the access middleware applies the per-user
	// filter downstream.
	for _, c := range created {
		h.publishComment(wsID, protocol.EventCommentCreated, actorType, actorUUID, c)
	}
	for _, c := range updated {
		h.publishComment(wsID, protocol.EventCommentUpdated, actorType, actorUUID, c)
	}

	writeJSON(w, http.StatusCreated, moveToThreadResponse{
		RootCommentID: util.UUIDToString(newRoot.ID),
		IssueID:       util.UUIDToString(issueID),
		MovedCount:    len(picked),
	})
}

// dedupeUUIDs parses and de-duplicates the id list, preserving first-seen order.
func dedupeUUIDs(raw []string) ([]pgtype.UUID, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]pgtype.UUID, 0, len(raw))
	for _, s := range raw {
		u, err := util.ParseUUID(s)
		if err != nil {
			return nil, err
		}
		key := util.UUIDToString(u)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, u)
	}
	return out, nil
}

// callerMayMoveAll gates the action: a workspace admin/owner may move any
// comments; everyone else may only move comments they authored.
func (h *Handler) callerMayMoveAll(ctx context.Context, picked []db.Comment, wsUUID pgtype.UUID, actorType string, actorID pgtype.UUID) bool {
	if h.isWorkspaceAdmin(ctx, wsUUID, actorType, actorID) {
		return true
	}
	for _, c := range picked {
		if actorType != c.AuthorType || util.UUIDToString(actorID) != util.UUIDToString(c.AuthorID) {
			return false
		}
	}
	return true
}

// isWorkspaceAdmin reports whether the member actor is an owner/admin. Non-member
// actors (agents) are never admins.
func (h *Handler) isWorkspaceAdmin(ctx context.Context, wsUUID pgtype.UUID, actorType string, actorID pgtype.UUID) bool {
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

// publishComment emits a comment event (created/updated) carrying the full row
// so the timeline cache can update without a refetch.
func (h *Handler) publishComment(wsID, eventType, actorType string, actorID pgtype.UUID, c db.Comment) {
	if h.Bus == nil {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        eventType,
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
