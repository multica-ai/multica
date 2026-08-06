package service

import (
	"context"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeWorkflowFailureGate struct {
	decision TaskFailureDecision
}

func (g fakeWorkflowFailureGate) EvaluateTaskFailure(context.Context, db.AgentTaskQueue, string, string) (TaskFailureDecision, error) {
	return g.decision, nil
}

func TestTaskFailureDecisionPrefersEvaluatedWorkflowResult(t *testing.T) {
	service := &TaskService{WorkflowFailureGate: fakeWorkflowFailureGate{
		decision: TaskFailureDecision{Action: TaskFailureSurface, Evaluated: true},
	}}
	decision := service.taskFailureDecision(context.Background(), db.AgentTaskQueue{}, "runtime_offline", "offline")
	if decision.Action != TaskFailureSurface {
		t.Fatalf("action = %q, want Workflow surface decision", decision.Action)
	}
}

func TestTaskFailureDecisionSurfacesWhenWorkflowIsUnavailable(t *testing.T) {
	service := &TaskService{WorkflowFailureGate: fakeWorkflowFailureGate{
		decision: TaskFailureDecision{Evaluated: false},
	}}
	decision := service.taskFailureDecision(context.Background(), db.AgentTaskQueue{}, "runtime_offline", "offline")
	if decision.Action != TaskFailureSurface {
		t.Fatalf("action = %q, want safe surface decision", decision.Action)
	}
}
