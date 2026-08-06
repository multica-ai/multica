package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// evaluateCommentWorkflowGate collects the request facts consumed by
// before.message.send. The actual decisions remain in Cerebro's Workflow
// policies; this adapter only bridges the shared HTTP handler to that engine.
func (h *Handler) evaluateCommentWorkflowGate(
	ctx context.Context,
	r *http.Request,
	issue db.Issue,
	authorType, authorID, content string,
	parentID string,
) (CommentWorkflowGateResult, error) {
	input := CommentWorkflowGateInput{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     uuidToString(issue.ID),
		AuthorType:  authorType,
		AuthorID:    authorID,
		Content:     content,
		ParentID:    parentID,
		IsSubIssue:  issue.ParentIssueID.Valid,
	}

	taskIDHeader := r.Header.Get("X-Task-ID")
	if authorType == "agent" && taskIDHeader != "" {
		if taskUUID, err := util.ParseUUID(taskIDHeader); err == nil {
			if task, err := h.Queries.GetAgentTask(ctx, taskUUID); err == nil {
				input.TaskID = taskIDHeader
				input.SessionID = task.SessionID.String
				if task.IssueID.Valid && uuidToString(task.IssueID) == input.IssueID {
					if task.TriggerCommentID.Valid {
						input.ThreadRequired = true
						input.RequiredParentID = uuidToString(task.TriggerCommentID)
					}
					noAction, checkErr := service.HasSquadLeaderNoActionEvaluationForTask(ctx, h.Queries, task)
					if checkErr != nil {
						slog.Warn("checking squad leader no_action evaluation failed", append(logger.RequestAttrs(r),
							"error", checkErr,
							"task_id", taskIDHeader,
							"issue_id", input.IssueID,
						)...)
					} else {
						input.NoAction = noAction
					}
				}
				if input.IsSubIssue && task.OriginalUserID.Valid {
					input.OwnerUserIDs = []string{uuidToString(task.OriginalUserID)}
				}
				if input.IsSubIssue && h.CerebroQueries != nil {
					posted, err := h.CerebroQueries.HasTaskPostedOnIssue(ctx, cerebrodb.HasTaskPostedOnIssueParams{
						TaskID:  taskUUID,
						IssueID: issue.ParentIssueID,
					})
					if err == nil {
						input.TaskPostedOnParent = posted
					}
				}
			}
		}
	}

	if input.IsSubIssue && authorType == "agent" && len(input.OwnerUserIDs) == 0 {
		if members, err := h.Queries.ListMembers(ctx, issue.WorkspaceID); err == nil {
			for _, member := range members {
				if member.Role == "owner" {
					input.OwnerUserIDs = append(input.OwnerUserIDs, uuidToString(member.UserID))
				}
			}
		}
	}

	if authorType == "agent" && h.CerebroQueries != nil {
		if agentUUID, err := util.ParseUUID(authorID); err == nil {
			active, err := h.CerebroQueries.HasActiveWakeupForAgentIssue(ctx, cerebrodb.HasActiveWakeupForAgentIssueParams{
				WorkspaceID: issue.WorkspaceID,
				AgentID:     agentUUID,
				IssueID:     issue.ID,
			})
			if err == nil {
				input.AgentHasActiveWakeup = active
			}
		}
	}

	if h.CommentTargetGuard == nil {
		return CommentWorkflowGateResult{}, fmt.Errorf("before.message.send Workflow gate is unavailable")
	}

	return h.CommentTargetGuard.EvaluateComment(ctx, input)
}
