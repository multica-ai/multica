package loops

// gate_evaluator.go is the engine-facing adapter for the delivery gate: it
// joins the persisted check outcomes (Store) to the pure decision (Reconcile)
// behind the small interface the workflows engine calls for an OpCheckPasses
// condition. It implements workflows.GateEvaluator structurally (matching
// method signature, no import of workflows needed), which is what lets the
// loops -> workflows import edge stay one-directional while the engine still
// reaches loop logic.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
)

// gateRound is the loop round the gate evaluates against. It is fixed at 1
// until per-revision round tracking lands (the caps/iteration counter is a
// later slice). A constant round is correct meanwhile: a re-run re-reports the
// same argv, updating the same row, so the gate always decides on the latest
// reported exit codes — "latest wins" idempotency, which is exactly what an
// engine that re-evaluates on every event needs.
const gateRound int32 = 1

// GateEvaluator decides loop delivery gates for the workflow engine. It is the
// concrete implementation of workflows.GateEvaluator.
type GateEvaluator struct {
	store      *Store
	dispatcher CheckDispatcher
}

// NewGateEvaluator builds a GateEvaluator backed by a check-outcome store on
// the given pool.
func NewGateEvaluator(pool *pgxpool.Pool) *GateEvaluator {
	return &GateEvaluator{store: NewStore(pool)}
}

// WithDispatcher plugs in the egress that sends enqueued checks to a runtime.
// Optional and nil-safe: without it the evaluator only records checks as
// pending (their dispatch is a no-op), which is the pre-dispatch behavior.
// Returns the receiver for fluent construction at the call site.
func (g *GateEvaluator) WithDispatcher(d CheckDispatcher) *GateEvaluator {
	g.dispatcher = d
	return g
}

// EvaluateCheckGate reads the reported outcomes for this gate, decides via
// Reconcile, and registers any not-yet-seen checks as pending so the runtime
// can pick them up. It advances only when every required check has run and
// exited zero; a missing or in-flight check waits, a failed check revises —
// in every non-advance case it returns false so the gate stays closed.
func (g *GateEvaluator) EvaluateCheckGate(ctx context.Context, issueID, gate string, value any) (bool, error) {
	cfg, err := parseCheckGateConfig(value)
	if err != nil {
		return false, err
	}
	iid, err := util.ParseUUID(issueID)
	if err != nil {
		return false, fmt.Errorf("check gate: parse issue id: %w", err)
	}

	outcomes, err := g.store.Outcomes(ctx, iid, gate, gateRound)
	if err != nil {
		return false, err
	}

	decision := Reconcile(cfg, outcomes)
	switch decision.Action {
	case GateAdvance:
		return true, nil
	case GateEnqueue:
		// Register the missing checks as pending, then send any not-yet-sent
		// check to the worker agent's runtime so it actually runs. The gate
		// still holds (returns false) until the runtime reports green.
		if err := g.store.Enqueue(ctx, iid, gate, gateRound, decision.Enqueue); err != nil {
			return false, err
		}
		if err := g.dispatchPending(ctx, iid, cfg.AgentID, gate); err != nil {
			return false, err
		}
		return false, nil
	default:
		// GateWait (in flight) or GateRevise (a check failed): hold the gate.
		return false, nil
	}
}

// dispatchPending sends every enqueued-but-undispatched check for this gate
// round to the worker agent's runtime exactly once, stamping each as dispatched
// so a later evaluation never re-sends it. A nil dispatcher (the pre-dispatch
// wiring) or a config without an agent leaves the checks pending without
// sending them — the gate still holds, it just has no egress.
func (g *GateEvaluator) dispatchPending(ctx context.Context, issueID pgtype.UUID, agentID, gate string) error {
	if g.dispatcher == nil || agentID == "" {
		return nil
	}
	pending, err := g.store.PendingDispatch(ctx, issueID, gate, gateRound)
	if err != nil {
		return err
	}
	issueStr := util.UUIDToString(issueID)
	for _, argv := range pending {
		if err := g.dispatcher.DispatchCheck(ctx, CheckDispatch{
			AgentID: agentID,
			IssueID: issueStr,
			Gate:    gate,
			Round:   gateRound,
			Argv:    argv,
		}); err != nil {
			return fmt.Errorf("dispatch pending check: %w", err)
		}
		if err := g.store.MarkDispatched(ctx, issueID, gate, gateRound, argv); err != nil {
			return err
		}
	}
	return nil
}

// parseCheckGateConfig accepts either a already-typed CheckGateConfig (the
// in-process Compile path) or its JSON form (a Condition.Value loaded from the
// workflow row), mirroring parseEvidenceConfig in the workflows package.
func parseCheckGateConfig(v any) (CheckGateConfig, error) {
	if v == nil {
		return CheckGateConfig{}, nil
	}
	if cfg, ok := v.(CheckGateConfig); ok {
		return cfg, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return CheckGateConfig{}, fmt.Errorf("check gate: marshal value: %w", err)
	}
	var cfg CheckGateConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return CheckGateConfig{}, fmt.Errorf("check gate: %w", err)
	}
	return cfg, nil
}
