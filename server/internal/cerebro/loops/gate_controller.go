package loops

// Reconcile turns the reported check outcomes into the single action the
// engine should take for a delivery gate. It is the brain of the check
// transport: pure logic, no I/O, so the async state machine (enqueue checks ->
// runtime runs them -> results come back -> decide) can be unit-tested in full
// before any DB or daemon wiring exists.
//
// The contract for the surrounding transport (next slice): the engine calls
// Reconcile on every gate evaluation, performs the returned action, and calls
// Reconcile again whenever a new outcome is reported. Reconcile is idempotent —
// calling it twice with the same outcomes yields the same decision — so a
// duplicate report or a re-fire never double-acts.

// GateAction is what the engine should do for a delivery gate right now.
type GateAction int

const (
	// GateWait — every required check is in flight; do nothing and wait for
	// the runtime to report.
	GateWait GateAction = iota
	// GateEnqueue — some required checks have no result yet and are not in
	// flight; the engine must enqueue them (see GateDecision.Enqueue).
	GateEnqueue
	// GateAdvance — every required check ran and exited zero; set the done
	// status.
	GateAdvance
	// GateRevise — a required check ran and failed; re-dispatch the build.
	GateRevise
)

func (a GateAction) String() string {
	switch a {
	case GateEnqueue:
		return "enqueue"
	case GateAdvance:
		return "advance"
	case GateRevise:
		return "revise"
	default:
		return "wait"
	}
}

// GateDecision is the result of Reconcile.
type GateDecision struct {
	Action GateAction
	// Enqueue is the set of check argv arrays the engine must run, set only
	// when Action is GateEnqueue.
	Enqueue [][]string
	// EnqueueJudge is the set of judge checks the engine must dispatch to a
	// judge agent, set only when Action is GateEnqueue. A gate can enqueue
	// both programmatic and judge checks in the same decision.
	EnqueueJudge []JudgeCheck
}

// Reconcile decides the next action for a delivery gate from the reported
// programmatic check outcomes and judge verdicts. A judge check is decided
// the same way as a programmatic one — Pass/exit-zero advances, a failed
// verdict/non-zero exit revises, a missing result is enqueued — the only
// difference is what "running" it means (a command vs. a model scoring a
// rubric).
//
//	any required check/judge failed   -> GateRevise   (fail fast)
//	a required check/judge has no result -> GateEnqueue  (run/dispatch it)
//	all required checks/judges in flight  -> GateWait
//	all required checks/judges passed     -> GateAdvance
//
// An empty config (no programmatic checks and no judge checks) never advances
// on its own — that would be a gate that checks nothing — so it reports
// GateWait.
func Reconcile(cfg CheckGateConfig, outcomes []CheckOutcome, judgeOutcomes []JudgeOutcome) GateDecision {
	if len(cfg.Checks) == 0 && len(cfg.JudgeChecks) == 0 {
		return GateDecision{Action: GateWait}
	}

	var toEnqueue [][]string
	var toEnqueueJudge []JudgeCheck
	inFlight := false

	for _, required := range cfg.Checks {
		o, found := findOutcome(outcomes, required)
		switch {
		case found && o.Ran && o.ExitCode != 0:
			return GateDecision{Action: GateRevise}
		case !found:
			toEnqueue = append(toEnqueue, required)
		case !o.Ran:
			inFlight = true
		}
	}

	for _, required := range cfg.JudgeChecks {
		o, found := findJudgeOutcome(judgeOutcomes, required.ID)
		switch {
		case found && o.Ran && !o.Pass:
			return GateDecision{Action: GateRevise}
		case !found:
			toEnqueueJudge = append(toEnqueueJudge, required)
		case !o.Ran:
			inFlight = true
		}
	}

	if len(toEnqueue) > 0 || len(toEnqueueJudge) > 0 {
		return GateDecision{Action: GateEnqueue, Enqueue: toEnqueue, EnqueueJudge: toEnqueueJudge}
	}
	if inFlight {
		return GateDecision{Action: GateWait}
	}
	return GateDecision{Action: GateAdvance}
}
