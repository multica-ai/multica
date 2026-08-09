package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
)

// WorkflowCompletionGate is implemented entirely by cerebro/workflows.
// This upstream seam carries no policy logic.
type WorkflowCompletionGate interface {
	BeforeComplete(context.Context, pgtype.UUID, []byte) ([]byte, error)
}

// WorkflowCompletionGuidance is the transport-neutral instruction returned
// when a Workflow hook can still be satisfied by the running agent.
type WorkflowCompletionGuidance struct {
	Code         string   `json:"code"`
	HookID       string   `json:"hook_id,omitempty"`
	HookName     string   `json:"hook_name,omitempty"`
	Requirement  string   `json:"requirement"`
	Alternatives []string `json:"alternatives"`
	Attempt      int      `json:"attempt"`
}

type workflowCompletionGuidanceError interface {
	error
	WorkflowCompletionGuidance() WorkflowCompletionGuidance
}

func WorkflowCompletionGuidanceFromError(err error) (WorkflowCompletionGuidance, bool) {
	var guidanceErr workflowCompletionGuidanceError
	if !errors.As(err, &guidanceErr) {
		return WorkflowCompletionGuidance{}, false
	}
	return guidanceErr.WorkflowCompletionGuidance(), true
}
