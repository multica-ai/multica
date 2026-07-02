package loops

// This file holds the engine-side decision for a delivery gate: given the
// checks a loop requires (CheckGateConfig) and the outcomes the agent runtime
// reported after executing them, decide whether to advance, revise, or wait.
//
// The split is deliberate. Running the check is I/O that must happen in the
// runtime where the repo lives (the next slice wires that transport). Deciding
// what a set of reported exit codes means is pure logic and lives here, fully
// unit-tested. The agent's opinion never enters the decision — only the
// reported exit codes do. That is what makes the gate trustworthy.

// CheckOutcome is one programmatic check's result as reported by the runtime
// that executed it. Ran is false until a result has been reported.
type CheckOutcome struct {
	Argv     []string `json:"argv"`
	Ran      bool     `json:"ran"`
	ExitCode int      `json:"exit_code"`
}

// GateState is the engine's verdict for a delivery gate.
type GateState int

const (
	// GatePending — at least one required check has not been reported yet.
	GatePending GateState = iota
	// GatePassed — every required check ran and exited zero.
	GatePassed
	// GateFailed — at least one required check ran and exited non-zero.
	GateFailed
)

func (g GateState) String() string {
	switch g {
	case GatePassed:
		return "passed"
	case GateFailed:
		return "failed"
	default:
		return "pending"
	}
}

// EvaluateGate decides a delivery gate from the reported outcomes.
//
//   - any required check confirmed non-zero  -> GateFailed  (revise now)
//   - every required check confirmed zero     -> GatePassed  (advance)
//   - otherwise                                -> GatePending (wait for results)
//
// A known failure wins over a still-pending check: there is no point waiting
// once we already have to revise. Outcomes for argv not in the config are
// ignored, so stale or extra reports cannot flip a gate.
func EvaluateGate(cfg CheckGateConfig, outcomes []CheckOutcome) GateState {
	if len(cfg.Checks) == 0 {
		return GatePending
	}
	allReported := true
	for _, required := range cfg.Checks {
		o, found := findOutcome(outcomes, required)
		if !found || !o.Ran {
			allReported = false
			continue
		}
		if o.ExitCode != 0 {
			return GateFailed
		}
	}
	if !allReported {
		return GatePending
	}
	return GatePassed
}

func findOutcome(outcomes []CheckOutcome, argv []string) (CheckOutcome, bool) {
	for _, o := range outcomes {
		if argvEqual(o.Argv, argv) {
			return o, true
		}
	}
	return CheckOutcome{}, false
}

func argvEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
