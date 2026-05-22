package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// notifyParentOfChildDone posts a top-level system comment on the parent
// issue when a child issue transitions from non-done into done. This replaces
// the agent-prompt rule that previously made child agents post the
// notification themselves (PR #2918 user feedback — the agent rule caused
// self-mention loops, planner ping-pong, and accidental `MUL-` prefix
// hardcoding because the agent did not always know the workspace prefix).
//
// Guards:
//   - prev.Status must not already be "done" (idempotent — repeat saves of
//     done do not re-fire; only the transition fires)
//   - issue.Status must be "done"
//   - issue.ParentIssueID must be set
//   - parent must not be "done" or "cancelled" — the parent is already
//     closed and a notification has no follow-up to drive
//
// The comment is inserted directly via db.Queries (not through the
// CreateComment HTTP handler) so it bypasses the assignee on_comment trigger
// and the mention-based agent enqueue paths. The content carries no
// `mention://agent/...` / `mention://member/...` / `mention://squad/...`
// links — only the safe issue mention for the child identifier. This is the
// "no default mention side-effect" requirement from MUL-2538.
//
// Errors are logged at warn level and swallowed: this is a best-effort
// notification on the side of a successful status update; failing it must
// not roll back the user's status change.
func (h *Handler) notifyParentOfChildDone(ctx context.Context, prev, issue db.Issue) {
	if !issue.ParentIssueID.Valid {
		return
	}
	if prev.Status == "done" || issue.Status != "done" {
		return
	}
	parent, err := h.Queries.GetIssue(ctx, issue.ParentIssueID)
	if err != nil {
		slog.Warn("child done: failed to load parent",
			"error", err,
			"child_id", uuidToString(issue.ID),
			"parent_id", uuidToString(issue.ParentIssueID))
		return
	}
	if parent.Status == "done" || parent.Status == "cancelled" {
		return
	}

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	identifier := prefix + "-" + strconv.Itoa(int(issue.Number))
	childID := uuidToString(issue.ID)
	title := issue.Title
	content := fmt.Sprintf(
		"Sub-issue [%s](mention://issue/%s) — \"%s\" — is done. Confirm whether to advance the next step on this parent (and promote any waiting `backlog` sub-issues).",
		identifier, childID, title,
	)

	// author_type='system', author_id=zero UUID. The zero UUID is a valid 16
	// byte value and the column is NOT NULL; frontend code should branch on
	// author_type === 'system' rather than on the UUID value.
	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     parent.ID,
		WorkspaceID: parent.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("child done: create system comment failed",
			"error", err,
			"child_id", childID,
			"parent_id", uuidToString(parent.ID))
		return
	}

	h.publish(protocol.EventCommentCreated, uuidToString(parent.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         parent.Title,
		"issue_assignee_type": textToPtr(parent.AssigneeType),
		"issue_assignee_id":   uuidToPtr(parent.AssigneeID),
		"issue_status":        parent.Status,
	})
}
