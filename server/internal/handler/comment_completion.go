package handler

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// handoffWorkerCompletionComment applies the existing assigned-squad reply
// route to a result synthesized by TaskService after successful completion.
// Explicit mentions retain their separate fallback-delegation semantics; this
// hook only admits a plain worker reply to the assigned leader's delegation.
func (h *Handler) handoffWorkerCompletionComment(ctx context.Context, issue db.Issue, comment db.Comment) {
	if issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid ||
		comment.AuthorType != "agent" || comment.Type != "comment" ||
		!comment.SourceTaskID.Valid || !comment.ParentID.Valid ||
		hasAgentOrSquadMention(util.ParseMentions(comment.Content)) {
		return
	}
	parent, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: comment.ParentID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		if !isNotFound(err) {
			slog.Warn("worker completion comment parent lookup failed",
				"comment_id", uuidToString(comment.ID), "error", err)
		}
		return
	}
	if parent.IssueID != issue.ID || parent.AuthorType != "agent" {
		return
	}
	originator := h.TaskService.ResolveOriginatorFromTriggerComment(ctx, issue.WorkspaceID, comment.ID)
	triggers, _ := h.computeCommentAgentTriggers(ctx, issue, comment.Content, &parent, "agent", uuidToString(comment.AuthorID), commentTriggerComputeOptions{
		ExcludeTriggerCommentID: comment.ID,
		OriginatorUserID:        uuidToString(originator),
	})
	for _, trigger := range triggers {
		if trigger.Source != commentTriggerSourceIssueAssignee || !trigger.NonLeaderAgentReply ||
			trigger.Squad == nil || trigger.Squad.ArchivedAt.Valid || parent.AuthorID != trigger.Agent.ID {
			continue
		}
		// A guest squad can delegate on an issue owned by another squad. Only
		// continue a delegation whose source task proves the assigned leader
		// role, so its result cannot wake an unrelated or replacement leader.
		squad, ok := h.squadLeaderRoleOfAuthoringTask(ctx, issue, parent, trigger.Agent)
		if !ok || squad.ID != issue.AssigneeID {
			continue
		}
		// Reuse the queued merge / dispatched planned-input handoff, including
		// permission checks and source-task attribution, instead of treating any
		// existing pending run as proof that this result will be delivered.
		status, reason := h.resolveCommentTriggerEnqueue(ctx, issue, trigger, comment.ID)
		if status == DispatchBlocked {
			slog.Warn("worker completion comment handoff blocked",
				"issue_id", uuidToString(issue.ID), "comment_id", uuidToString(comment.ID),
				"source_task_id", uuidToString(comment.SourceTaskID), "reason", reason)
		}
	}
}
