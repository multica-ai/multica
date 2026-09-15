package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issueworkflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrIssueExecutionSuperseded = errors.New("this execution was superseded and can no longer change the issue status")

type workflowExecutionQuerier interface {
	ListIssueAutomationExecutions(context.Context, db.ListIssueAutomationExecutionsParams) ([]db.AutomationExecution, error)
	GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error)
}

// AssertIssueWorkflowWriteAllowed runs under the issue row lock. A member can
// move the issue after takeover, but automated writes from that entry cannot.
func AssertIssueWorkflowWriteAllowed(ctx context.Context, q workflowExecutionQuerier, issue db.Issue, actor issueworkflow.TransitionActor) error {
	if actor.Type == "member" || !issue.WorkflowID.Valid || !issue.WorkflowStatusID.Valid {
		return nil
	}
	executions, err := q.ListIssueAutomationExecutions(ctx, db.ListIssueAutomationExecutionsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return err
	}
	var actorExecutionID pgtype.UUID
	if actor.Type == "agent" && actor.TaskID.Valid {
		task, err := q.GetAgentTask(ctx, actor.TaskID)
		if err != nil {
			return err
		}
		if task.AgentID == actor.ID && task.IssueID == issue.ID {
			actorExecutionID = task.AutomationExecutionID
		}
	}
	for _, execution := range executions {
		// A handed-off run may finish, but cannot change a later entry,
		// including a re-entry into the same status with the same agent.
		if actorExecutionID.Valid && execution.ID == actorExecutionID && execution.TriggerTransitionID != issue.LastTransitionID {
			return ErrIssueExecutionSuperseded
		}
		if execution.TriggerTransitionID == issue.LastTransitionID && execution.StatusID == issue.WorkflowStatusID && execution.Status == "superseded" {
			return ErrIssueExecutionSuperseded
		}
	}
	return nil
}
