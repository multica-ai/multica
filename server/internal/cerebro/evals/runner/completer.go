package runner

import "context"

// completer.go abstracts the single model call that both the AI-judge grader
// and the prompt target need. The live implementation (GatewayCompleter) speaks
// the Firtal AI Gateway; unit tests inject a fake so grader and target logic is
// deterministic and never spends budget. Keeping this seam means the runner
// package has no dependency on the gateway wire format.

// CompletionRequest is one model turn: an optional system instruction, the user
// prompt, and an optional model override (the completer falls back to its own
// default when Model is empty).
type CompletionRequest struct {
	System string
	Prompt string
	Model  string
}

// CompletionResult is the model's reply plus what the call cost, in whole cents.
// CostCents is 0 when the completer cannot attribute a cost.
type CompletionResult struct {
	Text      string
	CostCents int64
}

// Completer runs one model turn. Implementations must return a non-nil error
// on transport or provider failure so the runner records the task fail-closed.
type Completer interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error)
}
