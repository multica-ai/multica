package service

import (
	"context"
	"errors"

	"github.com/multica-ai/multica/server/internal/issueworkflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrIssueHumanConfirmationRequired = errors.New("this stage requires a member to confirm the status change")

type workflowAdvanceQuerier interface {
	ListIssueAutomationExecutions(context.Context, db.ListIssueAutomationExecutionsParams) ([]db.AutomationExecution, error)
	GetIssueWorkflowStatusByID(context.Context, db.GetIssueWorkflowStatusByIDParams) (db.IssueWorkflowStatus, error)
}

// AssertIssueWorkflowAdvance runs under the issue row lock, before any status
// write. The entry snapshot owns the gate even if a definition was edited while
// work was running. System and integration writes cannot bypass a human gate.
func AssertIssueWorkflowAdvance(ctx context.Context, q workflowAdvanceQuerier, issue db.Issue, actor issueworkflow.TransitionActor) error {
	if actor.Type == "member" || !issue.WorkflowID.Valid || !issue.WorkflowStatusID.Valid {
		return nil
	}
	executions, err := q.ListIssueAutomationExecutions(ctx, db.ListIssueAutomationExecutionsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return err
	}
	var raw []byte
	for _, execution := range executions {
		if execution.TriggerTransitionID == issue.LastTransitionID && execution.StatusID == issue.WorkflowStatusID {
			if execution.Status == "superseded" {
				return ErrIssueHumanConfirmationRequired
			}
			raw = execution.PolicySnapshot
			break
		}
	}
	if raw == nil {
		node, err := q.GetIssueWorkflowStatusByID(ctx, db.GetIssueWorkflowStatusByIDParams{WorkspaceID: issue.WorkspaceID, WorkflowID: issue.WorkflowID, ID: issue.WorkflowStatusID})
		if err != nil {
			return err
		}
		raw = node.EntryPolicy
	}
	policy, err := issueworkflow.DecodeEntryPolicy(raw)
	if err != nil {
		return err
	}
	if policy.Advance == issueworkflow.AdvanceHumanConfirms && (policy.Executor.Type != issueworkflow.ExecutorNone || policy.Assignee.Type == issueworkflow.AssigneeHuman || policy.NextStatusKey != "") {
		return ErrIssueHumanConfirmationRequired
	}
	return nil
}
