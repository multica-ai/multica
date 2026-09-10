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

type advanceQueryStub struct {
	entries []db.AutomationExecution
	node    db.IssueWorkflowStatus
	err     error
}

func (q advanceQueryStub) ListIssueAutomationExecutions(context.Context, db.ListIssueAutomationExecutionsParams) ([]db.AutomationExecution, error) {
	return q.entries, q.err
}
func (q advanceQueryStub) GetIssueWorkflowStatusByID(context.Context, db.GetIssueWorkflowStatusByIDParams) (db.IssueWorkflowStatus, error) {
	return q.node, q.err
}

func TestWorkflowAdvanceGateUsesEntrySnapshot(t *testing.T) {
	issue := db.Issue{ID: dbid.NewV7(), WorkspaceID: dbid.NewV7(), WorkflowID: dbid.NewV7(), WorkflowStatusID: dbid.NewV7(), LastTransitionID: dbid.NewV7()}
	human := []byte(`{"executor":{"type":"agent","id":"reviewer"},"instructions":"Review","advance":"human_confirms"}`)
	automatic := []byte(`{"executor":{"type":"agent","id":"reviewer"},"instructions":"Review","advance":"executor_may_transition"}`)
	entry := db.AutomationExecution{TriggerTransitionID: issue.LastTransitionID, StatusID: issue.WorkflowStatusID, PolicySnapshot: human, Status: "completed"}
	for _, actor := range []string{"agent", "system", "integration"} {
		t.Run(actor, func(t *testing.T) {
			err := AssertIssueWorkflowAdvance(context.Background(), advanceQueryStub{entries: []db.AutomationExecution{entry}, node: db.IssueWorkflowStatus{EntryPolicy: automatic}}, issue, issueworkflow.TransitionActor{Type: actor})
			if !errors.Is(err, ErrIssueHumanConfirmationRequired) {
				t.Fatalf("gate error=%v", err)
			}
		})
	}
	if err := AssertIssueWorkflowAdvance(context.Background(), advanceQueryStub{entries: []db.AutomationExecution{entry}}, issue, issueworkflow.TransitionActor{Type: "member"}); err != nil {
		t.Fatal(err)
	}
	entry.TriggerTransitionID = pgtype.UUID{Valid: false}
	if err := AssertIssueWorkflowAdvance(context.Background(), advanceQueryStub{entries: []db.AutomationExecution{entry}, node: db.IssueWorkflowStatus{EntryPolicy: automatic}}, issue, issueworkflow.TransitionActor{Type: "agent"}); err != nil {
		t.Fatalf("historical entry blocked current work: %v", err)
	}
	entry.TriggerTransitionID = issue.LastTransitionID
	entry.PolicySnapshot = automatic
	entry.Status = "superseded"
	if err := AssertIssueWorkflowAdvance(context.Background(), advanceQueryStub{entries: []db.AutomationExecution{entry}}, issue, issueworkflow.TransitionActor{Type: "agent"}); !errors.Is(err, ErrIssueHumanConfirmationRequired) {
		t.Fatalf("agent advanced after takeover: %v", err)
	}
}
