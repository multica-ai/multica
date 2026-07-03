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

// StatusSetter reverts an issue's status when a gate revises, so the board
// visibly shows where the loop actually is (e.g. back in Build) instead of
// sitting on "in_review" while a revision quietly re-runs behind it. Optional
// and nil-safe on GateEvaluator, mirroring CheckDispatcher: without one wired
// (or without CheckGateConfig.RevertStatus set), revise() behaves exactly as
// before. This is best-effort visibility, NOT the mechanism that re-runs the
// build — DispatchRevision (via CheckDispatcher) still does that directly, so
// a status-update failure here never blocks the actual revision work.
type StatusSetter interface {
	SetStatus(ctx context.Context, issueID pgtype.UUID, status string) error
}

// GateEvaluator decides loop delivery gates for the workflow engine. It is the
// concrete implementation of workflows.GateEvaluator.
type GateEvaluator struct {
	store        *Store
	dispatcher   CheckDispatcher
	statusSetter StatusSetter
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

// WithStatusSetter plugs in the status-revert egress (see StatusSetter).
// Optional and nil-safe. Returns the receiver for fluent construction.
func (g *GateEvaluator) WithStatusSetter(s StatusSetter) *GateEvaluator {
	g.statusSetter = s
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
	humanOutcomes, err := g.store.HumanOutcomes(ctx, iid, gate, state.Round)
	if err != nil {
		return false, err
	}

	decision := Reconcile(cfg, outcomes, judgeOutcomes, humanOutcomes)
	switch decision.Action {
	case GateAdvance:
		return true, nil
	case GateEnqueue:
		// Register the missing checks as pending, then send any not-yet-sent
		// check to the worker agent's runtime, any not-yet-sent judge check to
		// the judge agent's runtime, and any not-yet-sent human check to its
		// assignee, so they actually run. The gate still holds (returns
		// false) until every one reports back.
		if err := g.store.Enqueue(ctx, iid, gate, state.Round, decision.Enqueue); err != nil {
			return false, err
		}
		if err := g.store.EnqueueJudge(ctx, iid, gate, state.Round, decision.EnqueueJudge); err != nil {
			return false, err
		}
		if err := g.store.EnqueueHuman(ctx, iid, gate, state.Round, decision.EnqueueHuman); err != nil {
			return false, err
		}
		if err := g.dispatchPending(ctx, iid, cfg.AgentID, gate, state.Round); err != nil {
			return false, err
		}
		if err := g.dispatchPendingJudge(ctx, iid, cfg, gate, state.Round); err != nil {
			return false, err
		}
		if err := g.dispatchPendingHuman(ctx, iid, gate, state.Round); err != nil {
			return false, err
		}
		return false, nil
	case GateRevise:
		return false, g.revise(ctx, iid, gate, cfg, outcomes, judgeOutcomes, humanOutcomes)
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
//
// When cfg.RevertStatus is set (Compile fills it in — BuildStatus for the
// delivery gate, PlanningStatus for the plan gate), revise also moves the
// issue's status back there so the board shows reality (kicked back to
// Build/Plan) instead of sitting on a status the loop has already moved past.
// This is deliberately best-effort and additive: a status-update failure is
// logged-and-swallowed by the caller's error return being non-fatal to the
// dispatch below in the sense that dispatch always still runs — the actual
// re-run of the worker never depends on this succeeding.
func (g *GateEvaluator) revise(ctx context.Context, issueID pgtype.UUID, gate string, cfg CheckGateConfig, outcomes []CheckOutcome, judgeOutcomes []JudgeOutcome, humanOutcomes []HumanOutcome) error {
	signature := OutcomeSignature(outcomes, judgeOutcomes, humanOutcomes)
	next, err := g.store.RecordRevision(ctx, issueID, gate, signature, cfg.Caps)
	if err != nil {
		return err
	}
	if next.Stopped {
		return nil
	}
	var revertErr error
	if g.statusSetter != nil && cfg.RevertStatus != "" {
		revertErr = g.statusSetter.SetStatus(ctx, issueID, cfg.RevertStatus)
	}
	if g.dispatcher == nil || cfg.AgentID == "" || cfg.RevisionSkill == "" {
		return revertErr
	}
	dispatchErr := g.dispatcher.DispatchRevision(ctx, RevisionDispatch{
		AgentID:   cfg.AgentID,
		IssueID:   util.UUIDToString(issueID),
		Gate:      gate,
		Round:     next.Round,
		SkillName: cfg.RevisionSkill,
		Failures:  outcomes,
	})
	// The actual re-run must never be silently lost behind a status-revert
	// failure — surface whichever step failed (dispatch takes priority since
	// it's the mechanism that matters; the revert is cosmetic).
	if dispatchErr != nil {
		return dispatchErr
	}
	return revertErr
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
// this gate round to its judge agent's runtime exactly once, mirroring
// dispatchPending. A check with its own AssigneeType==AssigneeAgent (set on
// its Verification in the Issue workflow editor) dispatches to that agent and
// skill; otherwise it falls back to cfg.JudgeAgentID/cfg.JudgeSkill, and
// further to the worker agent (cfg.AgentID) when even that is empty, so a
// spec with judge checks but no configured judge still dispatches instead of
// stalling forever — Compile always fills JudgeAgentID (defaulting to
// AgentID), but a hand-built config used directly against the evaluator may
// not.
func (g *GateEvaluator) dispatchPendingJudge(ctx context.Context, issueID pgtype.UUID, cfg CheckGateConfig, gate string, round int32) error {
	if g.dispatcher == nil {
		return nil
	}
	pending, err := g.store.PendingJudgeDispatch(ctx, issueID, gate, round)
	if err != nil {
		return err
	}
	issueStr := util.UUIDToString(issueID)
	for _, check := range pending {
		checkCfg, found := findJudgeCheckConfig(cfg, check.ID)
		agentID, skill := judgeAssignee(cfg, checkCfg, found)
		if agentID == "" {
			continue
		}
		if err := g.dispatcher.DispatchJudge(ctx, JudgeDispatch{
			AgentID:   agentID,
			IssueID:   issueStr,
			Gate:      gate,
			Round:     round,
			CheckID:   check.ID,
			Rubric:    check.Rubric,
			SkillName: skill,
		}); err != nil {
			return fmt.Errorf("dispatch pending judge check: %w", err)
		}
		if err := g.store.MarkJudgeDispatched(ctx, issueID, gate, round, check.ID); err != nil {
			return err
		}
	}
	return nil
}

// findJudgeCheckConfig looks up a judge check's own config (assignee/skill
// override) by id from the gate config's JudgeChecks list.
func findJudgeCheckConfig(cfg CheckGateConfig, id string) (JudgeCheck, bool) {
	for _, c := range cfg.JudgeChecks {
		if c.ID == id {
			return c, true
		}
	}
	return JudgeCheck{}, false
}

// judgeAssignee resolves which agent (and skill) scores a judge check:
// the check's own AssigneeType==AssigneeAgent override first, then the gate's
// spec-wide JudgeAgentID/JudgeSkill, then the worker agent as a last resort
// (self-graded fallback) so a judge check never silently stalls forever.
func judgeAssignee(cfg CheckGateConfig, check JudgeCheck, found bool) (agentID, skill string) {
	if found && check.AssigneeType == AssigneeAgent && check.AssigneeID != "" {
		s := check.Skill
		if s == "" {
			s = cfg.JudgeSkill
		}
		return check.AssigneeID, s
	}
	if cfg.JudgeAgentID != "" {
		return cfg.JudgeAgentID, cfg.JudgeSkill
	}
	return cfg.AgentID, cfg.JudgeSkill
}

// dispatchPendingHuman sends every enqueued-but-undispatched human check for
// this gate round to its assignee exactly once. An agent assignee is
// dispatched a task the same way a judge check is (asking it to call
// report_loop_human instead of report_loop_judge); a member (person)
// assignee has nothing to dispatch to — approval happens out of band via the
// approve-human-check endpoint when they act on it — so a member check is
// simply stamped as dispatched (now visible / actionable) without an egress
// call.
func (g *GateEvaluator) dispatchPendingHuman(ctx context.Context, issueID pgtype.UUID, gate string, round int32) error {
	pending, err := g.store.PendingHumanDispatch(ctx, issueID, gate, round)
	if err != nil {
		return err
	}
	issueStr := util.UUIDToString(issueID)
	for _, check := range pending {
		if check.AssigneeType == AssigneeAgent && check.AssigneeID != "" && g.dispatcher != nil {
			if err := g.dispatcher.DispatchHuman(ctx, HumanDispatch{
				AssigneeType: check.AssigneeType,
				AssigneeID:   check.AssigneeID,
				IssueID:      issueStr,
				Gate:         gate,
				Round:        round,
				CheckID:      check.ID,
				Prompt:       check.Prompt,
			}); err != nil {
				return fmt.Errorf("dispatch pending human check: %w", err)
			}
		}
		if err := g.store.MarkHumanDispatched(ctx, issueID, gate, round, check.ID); err != nil {
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
