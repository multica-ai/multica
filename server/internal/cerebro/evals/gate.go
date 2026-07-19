package evals

import (
	"context"
	"encoding/json"
	"log/slog"

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
	base   BaseGateEvaluator
	store  BlockingEvalStore
	warner *AdvisoryWarner
}

func NewGateEvaluator(base BaseGateEvaluator, store BlockingEvalStore) *GateEvaluator {
	return &GateEvaluator{base: base, store: store}
}

// WithAdvisoryWarner plugs in the warn-only path for advisory (non-blocking)
// bindings. Optional and nil-safe: without it the gate only enforces blocking
// evals. The warner NEVER changes the advance decision — its error is logged.
func (g *GateEvaluator) WithAdvisoryWarner(warner *AdvisoryWarner) *GateEvaluator {
	g.warner = warner
	return g
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
	phase := evalBindingPhase(value)
	// Advisory bindings only warn — fire before the blocking read, log and
	// continue on any error so a warn failure can never block the gate.
	if g.warner != nil {
		if werr := g.warner.Warn(ctx, workflowUUID, issueUUID, phase); werr != nil {
			slog.Warn("eval advisory warn failed", "workflow_id", workflowUUID, "issue_id", issueUUID, "phase", phase, "error", werr)
		}
	}
	return g.store.BlockingEvalsPassed(ctx, workflowUUID, issueUUID, phase)
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
