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

import (
	"fmt"
	"sort"
	"strings"
)

// CheckOutcome is one programmatic check's result as reported by the runtime
// that executed it. Ran is false until a result has been reported.
type CheckOutcome struct {
	Argv     []string `json:"argv"`
	Ran      bool     `json:"ran"`
	ExitCode int      `json:"exit_code"`
}

// JudgeOutcome is one judge check's reported verdict, as scored by a judge
// agent against the check's rubric. Ran is false until a verdict has been
// reported. Blocking lists the specific reasons the judge revised, so the
// worker does not have to rediscover them — mirroring how a failed
// programmatic check's exit code alone is not the diagnosis, only the signal.
type JudgeOutcome struct {
	ID       string   `json:"id"`
	Ran      bool     `json:"ran"`
	Pass     bool     `json:"pass"`
	Blocking []string `json:"blocking_issues,omitempty"`
}

// OutcomeSignature returns a stable digest of a round's reported outcomes
// (programmatic and judge), used by the caps tracker (Store.RecordRevision)
// to detect a no-progress stall: two consecutive revisions with the same
// signature mean the worker resubmitted the same failing state instead of
// fixing anything. Sorted so the signature is independent of report order;
// unreported checks and their results are included so "still not reported"
// also counts as unchanged.
func OutcomeSignature(outcomes []CheckOutcome, judgeOutcomes []JudgeOutcome) string {
	parts := make([]string, 0, len(outcomes)+len(judgeOutcomes))
	for _, o := range outcomes {
		parts = append(parts, fmt.Sprintf("check|%s|%t|%d", strings.Join(o.Argv, " "), o.Ran, o.ExitCode))
	}
	for _, o := range judgeOutcomes {
		parts = append(parts, fmt.Sprintf("judge|%s|%t|%t|%s", o.ID, o.Ran, o.Pass, strings.Join(o.Blocking, ";")))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
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

func findJudgeOutcome(outcomes []JudgeOutcome, id string) (JudgeOutcome, bool) {
	for _, o := range outcomes {
		if o.ID == id {
			return o, true
		}
	}
	return JudgeOutcome{}, false
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
