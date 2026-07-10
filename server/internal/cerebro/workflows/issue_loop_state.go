package workflows

// issue_loop_state.go wires the control-strip live state (round k/N, last
// gate result, pending human checks) and the person-approval action onto an
// issue_loop workflow's compiled delivery gate. Same inversion as
// check_gate.go/loop_planning.go/issue_loop.go: workflows declares the seam,
// loops implements it (loops.LoopCheckStoreAdapter), router.go wires it in.

import "context"

// LoopGateState is the caps-tracking snapshot for one issue's delivery gate —
// the control strip's "round k/N, last result" line.
type LoopGateState struct {
	Round      int32
	Stopped    bool
	StopReason string
}

// PendingHumanCheck is one human check awaiting a decision, enough to render
// "Waiting on: <assignee> — <prompt>" plus an Approve/Reject action.
type PendingHumanCheck struct {
	CheckID      string
	Prompt       string
	AssigneeType string
	AssigneeID   string
}

// LoopCheckStore reads a compiled issue_loop delivery gate's live state and
// records a person's approval decision. gate is the recipe's own generated
// "loop:delivery-gate" child workflow id (see IssueLoopColumnStore) — the
// same key loops.GateEvaluator uses.
type LoopCheckStore interface {
	GateState(ctx context.Context, issueID, gate string) (LoopGateState, error)
	PendingHumanChecks(ctx context.Context, issueID, gate string) ([]PendingHumanCheck, error)
	ApproveHumanCheck(ctx context.Context, issueID, gate, checkID string, approved bool, note, approverID, approverType string) error
}

// WithLoopCheckStore plugs in the loop-state/approval implementation.
// Optional and nil-safe: without it, LoopState and ApproveHumanCheck return
// 503. Returns the receiver for chainable init.
func (h *Handler) WithLoopCheckStore(s LoopCheckStore) *Handler {
	h.loopCheckStore = s
	return h
}
