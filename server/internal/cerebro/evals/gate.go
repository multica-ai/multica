package evals

import (
	"context"

	"github.com/google/uuid"
)

type BaseGateEvaluator interface {
	EvaluateCheckGate(ctx context.Context, issueID, gate string, value any) (bool, error)
}

type BlockingEvalStore interface {
	BlockingEvalsPassed(ctx context.Context, workflowID, issueID uuid.UUID) (bool, error)
}

// GateEvaluator decorates the existing loop verification gate. Programmatic,
// judge, and human checks must pass first; then every blocking delivery eval
// bound to the canonical workflow recipe must have a latest passed run for the
// same issue. Missing results fail closed.
type GateEvaluator struct {
	base  BaseGateEvaluator
	store BlockingEvalStore
}

func NewGateEvaluator(base BaseGateEvaluator, store BlockingEvalStore) *GateEvaluator {
	return &GateEvaluator{base: base, store: store}
}

func (g *GateEvaluator) EvaluateCheckGate(ctx context.Context, issueID, gate string, value any) (bool, error) {
	passed, err := g.base.EvaluateCheckGate(ctx, issueID, gate, value)
	if err != nil || !passed {
		return passed, err
	}
	issueUUID, err := uuid.Parse(issueID)
	if err != nil {
		return false, err
	}
	workflowUUID, err := uuid.Parse(gate)
	if err != nil {
		return false, err
	}
	return g.store.BlockingEvalsPassed(ctx, workflowUUID, issueUUID)
}
