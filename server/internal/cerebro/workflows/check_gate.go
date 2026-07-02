package workflows

// check_gate.go wires the loop delivery gate (OpCheckPasses) into the engine.
//
// The gate decision itself lives in the loops package (loops.Reconcile over
// the persisted check outcomes), but workflows cannot import loops — loops
// already imports workflows to compile a spec into Rules, and the reverse edge
// would be a cycle. So the dependency is inverted: workflows declares the
// GateEvaluator interface it needs, the loops package supplies a concrete
// implementation (loops.GateEvaluator, backed by its check-outcome store), and
// main.go plugs that implementation in via Service.WithGateEvaluator. The
// engine here knows only the interface.

import "context"

// GateEvaluator decides whether a loop delivery gate may advance, based on the
// programmatic check outcomes reported so far. It is the seam the loops package
// implements; conditionsHold calls it for an OpCheckPasses condition.
//
//   - issueID is the triggered issue (workflows passes te.IssueID).
//   - gate is a stable per-gate key (workflows passes the workflow's id) so the
//     store can scope outcomes to this gate.
//   - value is the raw Condition.Value (a loops.CheckGateConfig as stored JSON);
//     the implementation parses it.
//
// It returns advance=true only when every required check has run and exited
// zero. When checks are missing it is the implementation's job to register
// them as pending (so the runtime can pick them up); it still returns
// advance=false until they report green. A non-nil error means the decision
// could not be made — conditionsHold treats that as "do not advance".
type GateEvaluator interface {
	EvaluateCheckGate(ctx context.Context, issueID, gate string, value any) (advance bool, err error)
}

// WithGateEvaluator plugs in the loop gate implementation. Optional and
// nil-safe: an engine without it cannot pass an OpCheckPasses gate (it fails
// closed in conditionsHold), exactly the safe default for a delivery gate.
// Returns the receiver for fluent construction at the call site.
func (s *Service) WithGateEvaluator(g GateEvaluator) *Service {
	s.gateEval = g
	return s
}
