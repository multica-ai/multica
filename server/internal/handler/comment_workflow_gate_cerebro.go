package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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

// recordCommentGateBlocker drops one `blocker` line into the agent's run
// timeline when a before.message.send gate rejects its comment, so a human
// watching the run modal sees the block instead of only an opaque 422. The run
// modal already renders `blocker` task_message rows, so this reuses that path —
// no new table, no new UI. Best-effort: a failure here never affects the 422
// the agent receives. No-op for member authors (no run) or when no task header
// is present (e.g. a manual CLI comment outside a task).
func (h *Handler) recordCommentGateBlocker(ctx context.Context, r *http.Request, issue db.Issue, authorType, message string) {
	if h.CerebroQueries == nil {
		return
	}
	taskIDHeader := r.Header.Get("X-Task-ID")
	line, ok := commentGateBlockerLine(authorType, taskIDHeader, message)
	if !ok {
		return
	}
	taskUUID, err := util.ParseUUID(taskIDHeader)
	if err != nil {
		return
	}
	message = line
	seq, err := h.CerebroQueries.NextTaskMessageSeq(ctx, taskUUID)
	if err != nil {
		slog.Warn("comment gate blocker: next seq failed", append(logger.RequestAttrs(r), "error", err, "task_id", taskIDHeader)...)
		return
	}
	if err := h.CerebroQueries.InsertBlockerTaskMessage(ctx, cerebrodb.InsertBlockerTaskMessageParams{
		TaskID:  taskUUID,
		Seq:     seq,
		Content: pgtype.Text{String: message, Valid: true},
	}); err != nil {
		slog.Warn("comment gate blocker: insert failed", append(logger.RequestAttrs(r), "error", err, "task_id", taskIDHeader)...)
		return
	}
	h.publishTask(protocol.EventTaskMessage, uuidToString(issue.WorkspaceID), "system", "", taskIDHeader, protocol.TaskMessagePayload{
		TaskID:    taskIDHeader,
		IssueID:   uuidToString(issue.ID),
		Seq:       int(seq),
		Type:      "blocker",
		Content:   message,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// commentGateBlockerLine decides whether a gate rejection earns a run-modal
// blocker line and returns the text to show. Only agent authors running inside
// a task (X-Task-ID present) have a run modal to surface it in; a member's
// blocked comment or a taskless CLI call gets nothing. An empty gate message
// falls back to a generic line so the timeline entry is never blank.
func commentGateBlockerLine(authorType, taskIDHeader, message string) (string, bool) {
	if authorType != "agent" || taskIDHeader == "" {
		return "", false
	}
	if strings.TrimSpace(message) == "" {
		return "Comment blocked by a before.message.send hook.", true
	}
	return message, true
}
