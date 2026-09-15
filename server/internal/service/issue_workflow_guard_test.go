package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issueworkflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type workflowExecutionQueryStub struct {
	entries []db.AutomationExecution
	err     error
	task    db.AgentTaskQueue
}

func (q workflowExecutionQueryStub) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return q.task, q.err
}

func (q workflowExecutionQueryStub) ListIssueAutomationExecutions(context.Context, db.ListIssueAutomationExecutionsParams) ([]db.AutomationExecution, error) {
	return q.entries, q.err
}

func TestWorkflowWriteGuardOnlyBlocksSupersededCurrentEntries(t *testing.T) {
	issue := db.Issue{ID: dbid.NewV7(), WorkspaceID: dbid.NewV7(), WorkflowID: dbid.NewV7(), WorkflowStatusID: dbid.NewV7(), LastTransitionID: dbid.NewV7()}
	entry := db.AutomationExecution{TriggerTransitionID: issue.LastTransitionID, StatusID: issue.WorkflowStatusID}
	for _, actor := range []string{"agent", "system", "integration", "member"} {
		for _, status := range []string{"dormant", "queued", "running", "completed", "superseded"} {
			t.Run(actor+"/"+status, func(t *testing.T) {
				entry.Status = status
				err := AssertIssueWorkflowWriteAllowed(context.Background(), workflowExecutionQueryStub{entries: []db.AutomationExecution{entry}}, issue, issueworkflow.TransitionActor{Type: actor})
				if actor != "member" && status == "superseded" {
					if !errors.Is(err, ErrIssueExecutionSuperseded) {
						t.Fatalf("write after takeover = %v", err)
					}
				} else if err != nil {
					t.Fatalf("write unexpectedly required approval: %v", err)
				}
			})
		}
	}
	entry.Status = "superseded"
	entry.TriggerTransitionID = dbid.NewV7()
	if err := AssertIssueWorkflowWriteAllowed(context.Background(), workflowExecutionQueryStub{entries: []db.AutomationExecution{entry}}, issue, issueworkflow.TransitionActor{Type: "agent"}); err != nil {
		t.Fatalf("historical entry blocked current work: %v", err)
	}
	readErr := errors.New("execution lookup failed")
	if err := AssertIssueWorkflowWriteAllowed(context.Background(), workflowExecutionQueryStub{err: readErr}, issue, issueworkflow.TransitionActor{Type: "agent"}); !errors.Is(err, readErr) {
		t.Fatalf("execution lookup failure was ignored: %v", err)
	}
}
