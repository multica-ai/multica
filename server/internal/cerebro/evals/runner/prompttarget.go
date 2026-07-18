package runner

import (
	"context"
	"errors"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// prompttarget.go is the "prompt" target: it runs each task's situation as a
// direct model prompt and returns the completion as the answer. It is the
// simplest target — no agent/skill/workflow orchestration — and the one the
// in-app builder uses to evaluate a bare prompt against a grader.

// PromptTarget runs task situations through the injected Completer.
type PromptTarget struct {
	completer Completer
	system    string
	model     string
}

// NewPromptTarget builds a prompt target from the eval's target spec.
func NewPromptTarget(completer Completer, spec evals.TargetSpec) (*PromptTarget, error) {
	if completer == nil {
		return nil, errors.New("prompt target needs a completer")
	}
	return &PromptTarget{completer: completer, system: spec.System, model: spec.Model}, nil
}

// Run sends the task situation as the prompt and returns the model's reply. An
// empty situation cannot happen — ParseTasks drops blank situations — but an
// all-whitespace answer is returned verbatim for the grader to judge.
func (p *PromptTarget) Run(ctx context.Context, task evals.TaskCase) (string, int64, error) {
	res, err := p.completer.Complete(ctx, CompletionRequest{
		System: p.system,
		Prompt: strings.TrimSpace(task.Situation),
		Model:  p.model,
	})
	if err != nil {
		return "", 0, err
	}
	return res.Text, res.CostCents, nil
}
