package service

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type workflowDispatchKey struct{}

var ErrWorkflowManagedIssue = errors.New("workflow tasks can only be started by the workflow scheduler")

func workflowEnqueueAllowed(ctx context.Context, issue db.Issue) bool {
	return issue.OriginType.String != "workflow" || ctx.Value(workflowDispatchKey{}) == true
}

// WorkflowTransaction uses the normal queue and attribution logic while keeping
// notifications private until the caller commits its run state and task together.
func (s *TaskService) WorkflowTransaction(tx pgx.Tx) *TaskService {
	out := NewTaskService(s.Queries.WithTx(tx), tx, nil, events.New())
	out.Composio = s.Composio
	out.Entitlements = s.Entitlements
	return out
}
func (s *TaskService) EnqueueWorkflowTask(ctx context.Context, issue db.Issue, prompt string, user pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueWorkflowTask(ctx, issue, prompt, user, pgtype.UUID{}, false)
}

// EnqueueWorkflowRetryTask keeps the workflow subtask (issue) stable while
// recording a new queue attempt as a retry child. The fresh-session flag is
// intentional: a user retry should not resume the failed runtime conversation
// after the user has explicitly asked the node to run again.
func (s *TaskService) EnqueueWorkflowRetryTask(ctx context.Context, issue db.Issue, prompt string, user, previousTaskID pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueWorkflowTask(ctx, issue, prompt, user, previousTaskID, true)
}

func (s *TaskService) enqueueWorkflowTask(ctx context.Context, issue db.Issue, prompt string, user, retryOfTaskID pgtype.UUID, forceFreshSession bool) (db.AgentTaskQueue, error) {
	if issue.OriginType.String != "workflow" {
		return db.AgentTaskQueue{}, ErrWorkflowManagedIssue
	}
	return s.enqueueIssueTask(context.WithValue(ctx, workflowDispatchKey{}, true), issue, pgtype.UUID{}, forceFreshSession, prompt, user, retryOfTaskID, pgtype.Timestamptz{}, OriginDerived)
}
