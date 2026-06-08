// CEREBRO-PATCH(child-status-notify): FIR-2601 — net-new cerebro file. Extends
// the upstream platform-owned child-done parent notification (issue_child_done.go,
// MUL-2538) to ALSO fire when a sub-issue transitions into `in_review` or
// `blocked`. Same mechanism as done: post a top-level system comment on the
// parent and wake the parent assignee (agent task / squad-leader task) so the
// parent driver can take action — review/forward an in_review child, or
// unblock/re-route a blocked one. All shared helpers (comment build, mention,
// dispatch + loop/idempotency guards) are reused from issue_child_done.go,
// which lives in the same `handler` package.
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

// childStatusNotifyTemplates maps each non-done status that should wake the
// parent to the body of the system comment posted on the parent. The leading
// `%s` is the parent-assignee mention prefix (so the parent agent is both
// mentioned in the body and woken by the explicit dispatch); the following
// args are the child identifier, child id, and sanitized child title.
//
// `done` is intentionally absent — it is owned by the upstream helper
// notifyParentOfChildDone and must not be double-fired here.
var childStatusNotifyTemplates = map[string]string{
	"in_review": "%sSub-issue [%s](mention://issue/%s) — \"%s\" — is ready for review (in_review). " +
		"Take action now: review it yourself or route it to the right reviewer, then move it forward. " +
		"Do not promote any waiting `backlog` sibling that depends on this one — in_review is not done.",
	"blocked": "%sSub-issue [%s](mention://issue/%s) — \"%s\" — is blocked. " +
		"Read its latest comments to find the named blocker, then take action: unblock it " +
		"(resolve the dependency, reassign, or re-scope) or adjust the parent plan. " +
		"Do not promote any sibling that depends on this blocked work.",
}

// notifyParentOfChildStatus mirrors notifyParentOfChildDone for the `in_review`
// and `blocked` transitions. It posts a top-level system comment on the parent
// and fires the explicit parent-assignee trigger (agent / squad-leader task)
// so the parent driver wakes and can unlock or carry the work forward.
//
// Guards (same shape as the done path):
//   - issue.ParentIssueID must be set
//   - issue.Status must be a status we wake on (in_review / blocked) AND must
//     be a real transition (prev.Status != issue.Status) so repeat saves of the
//     same status do not re-fire
//   - parent must not already be done/cancelled (closed parent has no follow-up)
//   - parent assignee must not be a member (humans read their own timeline;
//     there is no agent task to trigger) — MUL-2538 product call, kept identical
//
// Errors are logged at warn and swallowed: this is a best-effort notification
// on the side of a successful status update.
func (h *Handler) notifyParentOfChildStatus(ctx context.Context, prev, issue db.Issue, requesterUserID string) {
	// CEREBRO-PATCH(child-done-notify-flag): TECH-3006 — flag gate for in_review/blocked parent notifications.
	if !h.cerebroChildStatusNotifyParentEnabled(ctx, issue.WorkspaceID, requesterUserID) {
		return
	}
	if !issue.ParentIssueID.Valid {
		return
	}
	template, ok := childStatusNotifyTemplates[issue.Status]
	if !ok || prev.Status == issue.Status {
		return
	}
	parent, err := h.Queries.GetIssue(ctx, issue.ParentIssueID)
	if err != nil {
		slog.Warn("child status: failed to load parent",
			"error", err,
			"status", issue.Status,
			"child_id", uuidToString(issue.ID),
			"parent_id", uuidToString(issue.ParentIssueID))
		return
	}
	if parent.Status == "done" || parent.Status == "cancelled" {
		return
	}
	// Human-assigned parents read their own timeline; an automated system
	// comment is noise and there is no agent task to trigger — MUL-2538.
	if parent.AssigneeType.Valid && parent.AssigneeType.String == "member" {
		return
	}

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	identifier := prefix + "-" + strconv.Itoa(int(issue.Number))
	childID := uuidToString(issue.ID)
	title := sanitizeChildTitleForSystemComment(issue.Title)
	mentionPrefix := h.buildParentAssigneeMention(ctx, parent)

	content := fmt.Sprintf(template, mentionPrefix, identifier, childID, title)

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
		slog.Warn("child status: create system comment failed",
			"error", err,
			"status", issue.Status,
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

	// Wake the parent assignee (agent task / squad-leader task). Reuses the
	// done path's dispatcher, which carries the loop + idempotency guards.
	h.dispatchParentAssigneeTrigger(ctx, parent, issue, comment)
}
