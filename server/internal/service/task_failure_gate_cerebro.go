package service

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type TaskFailureAction string

const (
	TaskFailureRetry   TaskFailureAction = "retry"
	TaskFailurePause   TaskFailureAction = "pause"
	TaskFailureAlert   TaskFailureAction = "alert"
	TaskFailureSurface TaskFailureAction = "surface"
)

type TaskFailureDecision struct {
	Action       TaskFailureAction
	FreshSession bool
	RetryLimit   int32
	UserMessage  string
	Evaluated    bool
}

type WorkflowFailureGate interface {
	EvaluateTaskFailure(context.Context, db.AgentTaskQueue, string, string) (TaskFailureDecision, error)
}

func (s *TaskService) taskFailureDecision(ctx context.Context, task db.AgentTaskQueue, reason, message string) TaskFailureDecision {
	if s != nil && s.WorkflowFailureGate != nil {
		if decision, err := s.WorkflowFailureGate.EvaluateTaskFailure(ctx, task, reason, message); err == nil && decision.Evaluated {
			return decision
		}
	}
	// Safe floor when the emergency hook switch is off or evaluation fails:
	// surface the failure without inventing a retry decision outside Workflow.
	return TaskFailureDecision{Action: TaskFailureSurface}
}
