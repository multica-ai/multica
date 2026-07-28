// This file owns the cerebro-only "move comments to a new thread" feature:
// lifting a hand-picked set of comments out of one thread into a new thread on
// the SAME issue.
//
// It is the multi-select sibling of move_to_subissue.go (JEH-1309). Where the
// sub-issue flow takes a whole root thread out of the issue, this flow lets the
// user cherry-pick comments inside a thread and spin them into a side thread
// that stays on the issue — the entry point is the per-comment "Reply in new
// thread" action.
//
// FIR-3880: the move re-parents the original rows instead of copying them —
// the oldest pick is promoted to a thread root and the rest are hung under it.
// Nothing is left behind at the old location and no breadcrumb comment is
// written, so a moved comment keeps its id, author, timestamps, attachments,
// reactions and approval bindings. Comments that were NOT picked but hung off
// one that was are re-homed to the nearest comment that stays, so the old
// thread survives the split (see planMove).
//
// The whole operation runs in one transaction so the caller never observes a
// half-moved set: either every picked comment sits in the new thread and every
// comment left behind has a valid parent, or nothing changed.
package comments

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// issueCommentCap bounds the per-issue comment load used to work out which
// comments stay behind. Mirrors the defensive cap the comment list endpoints
// use — issue p99 is ~30 comments, the largest ever observed is ~1.1k.
const issueCommentCap = 2000

// moveToThreadRequest is the POST body. `comment_ids` is the ordered set the
// user picked in the UI; the backend re-derives chronological order itself, so
// client ordering is advisory only.
type moveToThreadRequest struct {
	CommentIDs []string `json:"comment_ids"`
}

// moveToThreadResponse returns the identity of the new thread so the client can
// scroll to it without a refetch. `root_comment_id` is the oldest pick, which
// the move promotes to the new thread's root; `moved_count` is the number of
// comments that changed threads.
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
// On success the picked comments form a new thread on the same issue (oldest
// pick = new root, the rest become its replies in chronological order). The
// rows are re-parented in place, so nothing is copied and nothing is left
// behind at the old location.
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

	// The whole issue is needed to work out what happens to the comments that
	// stay behind: a reply whose parent is moving would otherwise be dragged
	// along or orphaned. Chronological order, which planMove relies on.
	all, err := h.Queries.ListCommentsForIssue(r.Context(), db.ListCommentsForIssueParams{
		IssueID:     issueID,
		WorkspaceID: wsUUID,
		Limit:       issueCommentCap,
	})
	if err != nil {
		slog.Error("move-to-thread: list issue comments failed", "issue_id", util.UUIDToString(issueID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load the issue's comments")
		return
	}

	pickedIDs := make([]string, 0, len(picked))
	for _, c := range picked {
		pickedIDs = append(pickedIDs, util.UUIDToString(c.ID))
	}
	newRootID := pickedIDs[0]
	plan := planMove(toCommentNodes(all), pickedIDs)

	// Every id in the plan came out of `all`, so keep the parsed UUIDs around
	// instead of re-parsing strings on the write path.
	uuidByID := make(map[string]pgtype.UUID, len(all))
	for _, c := range all {
		uuidByID[util.UUIDToString(c.ID)] = c.ID
	}

	tx, err := h.Tx.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Apply the plan. planMove emits assignments in chronological order, so a
	// comment is always re-parented after the comment it will hang under.
	updated := make([]db.Comment, 0, len(plan))
	for _, a := range plan {
		var parent pgtype.UUID
		if a.ParentID != "" {
			parent = uuidByID[a.ParentID]
		}
		u, err := qtx.SetCommentParent(r.Context(), db.SetCommentParentParams{
			ID:          uuidByID[a.ID],
			WorkspaceID: wsUUID,
			ParentID:    parent,
		})
		if err != nil {
			slog.Error("move-to-thread: re-parent failed", "comment_id", a.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to move comment")
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
	// refetch. Nothing is created or deleted — every touched comment simply
	// hangs somewhere else now. Workspace channel; the access middleware
	// applies the per-user filter downstream.
	for _, c := range updated {
		h.publishComment(wsID, protocol.EventCommentUpdated, actorType, actorUUID, c)
	}

	writeJSON(w, http.StatusCreated, moveToThreadResponse{
		RootCommentID: newRootID,
		IssueID:       util.UUIDToString(issueID),
		MovedCount:    len(picked),
	})
}

// commentNode is the minimal shape planMove needs: an id and its direct parent
// ("" for a thread root).
type commentNode struct {
	ID       string
	ParentID string
}

// parentAssignment is a single parent_id write. ParentID "" promotes the
// comment to a thread root.
type parentAssignment struct {
	ID       string
	ParentID string
}

func toCommentNodes(all []db.Comment) []commentNode {
	out := make([]commentNode, 0, len(all))
	for _, c := range all {
		parent := ""
		if c.ParentID.Valid {
			parent = util.UUIDToString(c.ParentID)
		}
		out = append(out, commentNode{ID: util.UUIDToString(c.ID), ParentID: parent})
	}
	return out
}

// planMove works out every parent_id write the move implies.
//
// `all` is every comment on the issue in chronological order; `pickedIDs` is
// the moving set, also chronological. Two groups of comments change parent:
//
//   - The picked set becomes one thread: the oldest pick is promoted to a root
//     and the rest hang directly under it.
//   - A comment that stays behind but hung off a pick would be orphaned, so it
//     is re-homed to its nearest ancestor that stays. When its whole ancestor
//     chain is moving, the old thread has lost its root: the oldest such
//     comment becomes the root of what is left and its peers hang under it.
//
// Assignments come back in `all` order (chronological) with no-ops dropped, so
// a comment is always written after the comment it will hang under, and no
// intermediate state can point a comment at a parent that is about to move.
func planMove(all []commentNode, pickedIDs []string) []parentAssignment {
	if len(pickedIDs) == 0 {
		return nil
	}

	parentOf := make(map[string]string, len(all))
	order := make(map[string]int, len(all))
	for i, c := range all {
		parentOf[c.ID] = c.ParentID
		order[c.ID] = i
	}
	moving := make(map[string]struct{}, len(pickedIDs))
	for _, id := range pickedIDs {
		moving[id] = struct{}{}
	}

	// Nearest ancestor that stays behind; "" when the whole chain is moving.
	survivingAncestor := func(id string) string {
		for p := parentOf[id]; p != ""; p = parentOf[p] {
			if _, gone := moving[p]; !gone {
				return p
			}
		}
		return ""
	}
	oldRoot := func(id string) string {
		root := id
		for parentOf[root] != "" {
			root = parentOf[root]
		}
		return root
	}

	assign := make(map[string]string, len(all))
	assign[pickedIDs[0]] = ""
	for _, id := range pickedIDs[1:] {
		assign[id] = pickedIDs[0]
	}

	// Comments left behind whose direct parent is moving. Those with no
	// surviving ancestor are grouped per old thread and re-rooted together.
	rerootByOldRoot := make(map[string][]string)
	for _, c := range all {
		if _, gone := moving[c.ID]; gone {
			continue
		}
		if c.ParentID == "" {
			continue
		}
		if _, parentGone := moving[c.ParentID]; !parentGone {
			continue
		}
		if anc := survivingAncestor(c.ID); anc != "" {
			assign[c.ID] = anc
			continue
		}
		r := oldRoot(c.ID)
		rerootByOldRoot[r] = append(rerootByOldRoot[r], c.ID)
	}
	for _, ids := range rerootByOldRoot {
		sort.SliceStable(ids, func(a, b int) bool { return order[ids[a]] < order[ids[b]] })
		assign[ids[0]] = ""
		for _, id := range ids[1:] {
			assign[id] = ids[0]
		}
	}

	out := make([]parentAssignment, 0, len(assign))
	for _, c := range all {
		next, planned := assign[c.ID]
		if !planned || next == c.ParentID {
			continue
		}
		out = append(out, parentAssignment{ID: c.ID, ParentID: next})
	}
	return out
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
