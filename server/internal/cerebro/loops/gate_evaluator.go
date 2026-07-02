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

// gateRound is the round a fresh gate starts on, before any revision has
// advanced it. GateRoundState.Round (persisted in cerebro_loop_gate_state, see
// Store.LoadGateState) is the round the evaluator actually evaluates against
// — it is 1 until the first GateRevise, then advances by one per revision.
// Tests that never trigger a revision can still assume round 1 throughout.
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

// EvaluateCheckGate reads the reported outcomes for this gate's current
// round, decides via Reconcile, and registers any not-yet-seen checks as
// pending so the runtime can pick them up. It advances only when every
// required check has run and exited zero; a missing or in-flight check waits,
// a failed check revises — in every non-advance case it returns false so the
// gate stays closed.
//
// A gate the caps tracker has already Stopped (see Store.RecordRevision) is
// frozen: it returns false without reading outcomes, enqueuing checks, or
// dispatching anything. That is the stop-rule itself — once a loop's caps
// trip, the engine must stop feeding it more rounds and wait for a human
// (loop:escalate-stalled already notifies the issue owner on review entry).
func (g *GateEvaluator) EvaluateCheckGate(ctx context.Context, issueID, gate string, value any) (bool, error) {
	cfg, err := parseCheckGateConfig(value)
	if err != nil {
		return false, err
	}
	iid, err := util.ParseUUID(issueID)
	if err != nil {
		return false, fmt.Errorf("check gate: parse issue id: %w", err)
	}

	state, err := g.store.LoadGateState(ctx, iid, gate)
	if err != nil {
		return false, err
	}
	if state.Stopped {
		return false, nil
	}

	outcomes, err := g.store.Outcomes(ctx, iid, gate, state.Round)
	if err != nil {
		return false, err
	}
	judgeOutcomes, err := g.store.JudgeOutcomes(ctx, iid, gate, state.Round)
	if err != nil {
		return false, err
	}

	decision := Reconcile(cfg, outcomes, judgeOutcomes)
	switch decision.Action {
	case GateAdvance:
		return true, nil
	case GateEnqueue:
		// Register the missing checks as pending, then send any not-yet-sent
		// check to the worker agent's runtime (and any not-yet-sent judge
		// check to the judge agent's runtime) so they actually run. The gate
		// still holds (returns false) until every runtime reports green.
		if err := g.store.Enqueue(ctx, iid, gate, state.Round, decision.Enqueue); err != nil {
			return false, err
		}
		if err := g.store.EnqueueJudge(ctx, iid, gate, state.Round, decision.EnqueueJudge); err != nil {
			return false, err
		}
		if err := g.dispatchPending(ctx, iid, cfg.AgentID, gate, state.Round); err != nil {
			return false, err
		}
		if err := g.dispatchPendingJudge(ctx, iid, cfg, gate, state.Round); err != nil {
			return false, err
		}
		return false, nil
	case GateRevise:
		return false, g.revise(ctx, iid, gate, cfg, outcomes, judgeOutcomes)
	default:
		// GateWait: every required check is in flight; hold.
		return false, nil
	}
}

// revise records a failed round against the spec's caps (Store.RecordRevision)
// and, unless that trips a cap, re-dispatches the worker's build skill so it
// can fix the failing checks in the fresh round the caps tracker just opened.
// A cap trip freezes the gate instead: EvaluateCheckGate's Stopped check on
// the next call is what actually stops the loop from consuming more rounds.
func (g *GateEvaluator) revise(ctx context.Context, issueID pgtype.UUID, gate string, cfg CheckGateConfig, outcomes []CheckOutcome, judgeOutcomes []JudgeOutcome) error {
	signature := OutcomeSignature(outcomes, judgeOutcomes)
	next, err := g.store.RecordRevision(ctx, issueID, gate, signature, cfg.Caps)
	if err != nil {
		return err
	}
	if next.Stopped {
		return nil
	}
	if g.dispatcher == nil || cfg.AgentID == "" || cfg.RevisionSkill == "" {
		return nil
	}
	return g.dispatcher.DispatchRevision(ctx, RevisionDispatch{
		AgentID:   cfg.AgentID,
		IssueID:   util.UUIDToString(issueID),
		Gate:      gate,
		Round:     next.Round,
		SkillName: cfg.RevisionSkill,
		Failures:  outcomes,
	})
}

// dispatchPending sends every enqueued-but-undispatched check for this gate
// round to the worker agent's runtime exactly once, stamping each as dispatched
// so a later evaluation never re-sends it. A nil dispatcher (the pre-dispatch
// wiring) or a config without an agent leaves the checks pending without
// sending them — the gate still holds, it just has no egress.
func (g *GateEvaluator) dispatchPending(ctx context.Context, issueID pgtype.UUID, agentID, gate string, round int32) error {
	if g.dispatcher == nil || agentID == "" {
		return nil
	}
	pending, err := g.store.PendingDispatch(ctx, issueID, gate, round)
	if err != nil {
		return err
	}
	issueStr := util.UUIDToString(issueID)
	for _, argv := range pending {
		if err := g.dispatcher.DispatchCheck(ctx, CheckDispatch{
			AgentID: agentID,
			IssueID: issueStr,
			Gate:    gate,
			Round:   round,
			Argv:    argv,
		}); err != nil {
			return fmt.Errorf("dispatch pending check: %w", err)
		}
		if err := g.store.MarkDispatched(ctx, issueID, gate, round, argv); err != nil {
			return err
		}
	}
	return nil
}

// dispatchPendingJudge sends every enqueued-but-undispatched judge check for
// this gate round to the judge agent's runtime exactly once, mirroring
// dispatchPending. It falls back to the worker agent (cfg.AgentID) when no
// distinct JudgeAgentID is configured, so a spec with judge checks but no
// judge agent still dispatches instead of stalling forever — Compile always
// fills JudgeAgentID (defaulting to AgentID), but a hand-built config used
// directly against the evaluator may not.
func (g *GateEvaluator) dispatchPendingJudge(ctx context.Context, issueID pgtype.UUID, cfg CheckGateConfig, gate string, round int32) error {
	judgeAgentID := cfg.JudgeAgentID
	if judgeAgentID == "" {
		judgeAgentID = cfg.AgentID
	}
	if g.dispatcher == nil || judgeAgentID == "" {
		return nil
	}
	pending, err := g.store.PendingJudgeDispatch(ctx, issueID, gate, round)
	if err != nil {
		return err
	}
	issueStr := util.UUIDToString(issueID)
	for _, check := range pending {
		if err := g.dispatcher.DispatchJudge(ctx, JudgeDispatch{
			AgentID:   judgeAgentID,
			IssueID:   issueStr,
			Gate:      gate,
			Round:     round,
			CheckID:   check.ID,
			Rubric:    check.Rubric,
			SkillName: cfg.JudgeSkill,
		}); err != nil {
			return fmt.Errorf("dispatch pending judge check: %w", err)
		}
		if err := g.store.MarkJudgeDispatched(ctx, issueID, gate, round, check.ID); err != nil {
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
