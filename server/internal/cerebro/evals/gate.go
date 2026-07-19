package evals

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

type BaseGateEvaluator interface {
	EvaluateCheckGate(ctx context.Context, issueID, gate string, value any) (bool, error)
}

type BlockingEvalStore interface {
	BlockingEvalsPassed(ctx context.Context, workflowID, issueID uuid.UUID, phase string) (bool, error)
}

type evalPhaseCarrier interface {
	EvalBindingPhase() string
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
	return g.store.BlockingEvalsPassed(ctx, workflowUUID, issueUUID, evalBindingPhase(value))
}

func evalBindingPhase(value any) string {
	if carrier, ok := value.(evalPhaseCarrier); ok && validEvalPhase(carrier.EvalBindingPhase()) {
		return carrier.EvalBindingPhase()
	}
	// Workflow conditions cross a JSON boundary before evaluation, so retain
	// the phase even when the concrete CheckGateConfig type is no longer
	// available here. Old stored configs have no marker and remain delivery
	// gates for backwards compatibility.
	raw, err := json.Marshal(value)
	if err == nil {
		var config struct {
			Phase string `json:"eval_phase"`
		}
		if json.Unmarshal(raw, &config) == nil && validEvalPhase(config.Phase) {
			return config.Phase
		}
	}
	return "delivery"
}

func validEvalPhase(phase string) bool {
	return phase == "plan" || phase == "delivery" || phase == "monitor"
}
