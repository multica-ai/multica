package service

import "slices"

// Initiative lifecycle statuses (Initiatives & Orchestrator RFC §4.1).
const (
	InitiativeStatusDraft       = "draft"
	InitiativeStatusPlanning    = "planning"
	InitiativeStatusPlanReview  = "plan_review"
	InitiativeStatusExecuting   = "executing"
	InitiativeStatusIntegrating = "integrating"
	InitiativeStatusVerifying   = "verifying"
	InitiativeStatusDone        = "done"
	InitiativeStatusNeedsHuman  = "needs_human"
	InitiativeStatusPaused      = "paused"
	InitiativeStatusCancelled   = "cancelled"
	InitiativeStatusFailed      = "failed"
)

// Initiative task overlay states (RFC §4.2). The linked issue keeps its normal
// Multica status; these states are orchestration metadata layered on top.
const (
	InitiativeTaskStatePending    = "pending"
	InitiativeTaskStateReady      = "ready"
	InitiativeTaskStateDispatched = "dispatched"
	InitiativeTaskStateInProgress = "in_progress"
	InitiativeTaskStateReview     = "review"
	InitiativeTaskStateVerifying  = "verifying"
	InitiativeTaskStateDone       = "done"
	InitiativeTaskStateBlocked    = "blocked"
	InitiativeTaskStateFailed     = "failed"
	InitiativeTaskStateRetrying   = "retrying"
)

// ActiveInitiativeStatuses are the statuses the reconciler ticks over. Keep in
// sync with idx_initiative_active (migration 219) and ListActiveInitiativeIDs.
var ActiveInitiativeStatuses = []string{
	InitiativeStatusPlanning,
	InitiativeStatusPlanReview,
	InitiativeStatusExecuting,
	InitiativeStatusIntegrating,
	InitiativeStatusVerifying,
	InitiativeStatusNeedsHuman,
}

// initiativeTransitions is the complete edge set of the initiative state
// machine — human- and reconciler-driven edges combined. *Who* may drive an
// edge is enforced at the call sites (handlers gate human actions, the
// reconciler gates automatic ones); this map only answers whether the edge
// exists at all. Terminal statuses have no outgoing edges. `paused` resumes to
// the status stored in initiative.pause_prev_status, so it lists every status
// pause is reachable from. planning → executing is the autonomy-level-3 path
// where the plan-review gate auto-approves.
var initiativeTransitions = map[string][]string{
	InitiativeStatusDraft: {
		InitiativeStatusPlanning,
		InitiativeStatusCancelled,
	},
	InitiativeStatusPlanning: {
		InitiativeStatusPlanReview,
		InitiativeStatusExecuting,
		InitiativeStatusNeedsHuman,
		InitiativeStatusFailed,
		InitiativeStatusPaused,
		InitiativeStatusCancelled,
	},
	InitiativeStatusPlanReview: {
		InitiativeStatusPlanning,
		InitiativeStatusExecuting,
		InitiativeStatusNeedsHuman,
		InitiativeStatusPaused,
		InitiativeStatusCancelled,
	},
	InitiativeStatusExecuting: {
		InitiativeStatusIntegrating,
		InitiativeStatusNeedsHuman,
		InitiativeStatusFailed,
		InitiativeStatusPaused,
		InitiativeStatusCancelled,
	},
	InitiativeStatusIntegrating: {
		InitiativeStatusVerifying,
		InitiativeStatusExecuting,
		InitiativeStatusNeedsHuman,
		InitiativeStatusPaused,
		InitiativeStatusCancelled,
	},
	InitiativeStatusVerifying: {
		InitiativeStatusDone,
		InitiativeStatusIntegrating,
		InitiativeStatusExecuting,
		InitiativeStatusNeedsHuman,
		InitiativeStatusPaused,
		InitiativeStatusCancelled,
	},
	InitiativeStatusNeedsHuman: {
		InitiativeStatusPlanning,
		InitiativeStatusPlanReview,
		InitiativeStatusExecuting,
		InitiativeStatusIntegrating,
		InitiativeStatusVerifying,
		InitiativeStatusPaused,
		InitiativeStatusCancelled,
		InitiativeStatusFailed,
	},
	InitiativeStatusPaused: {
		InitiativeStatusPlanning,
		InitiativeStatusPlanReview,
		InitiativeStatusExecuting,
		InitiativeStatusIntegrating,
		InitiativeStatusVerifying,
		InitiativeStatusNeedsHuman,
		InitiativeStatusCancelled,
	},
	InitiativeStatusDone:      {},
	InitiativeStatusCancelled: {},
	InitiativeStatusFailed:    {},
}

// initiativeTaskTransitions mirrors initiativeTransitions for the task
// overlay. A human closing or cancelling the linked issue can terminate a task
// from any post-dispatch state, which is why done/failed appear as targets on
// every active state. `retrying` is the between-attempts holding state of the
// retry ladder; it re-enters the DAG through `ready`.
var initiativeTaskTransitions = map[string][]string{
	InitiativeTaskStatePending: {
		InitiativeTaskStateReady,
		InitiativeTaskStateFailed,
	},
	InitiativeTaskStateReady: {
		InitiativeTaskStateDispatched,
		InitiativeTaskStateFailed,
	},
	InitiativeTaskStateDispatched: {
		InitiativeTaskStateInProgress,
		InitiativeTaskStateReview,
		InitiativeTaskStateVerifying,
		InitiativeTaskStateDone,
		InitiativeTaskStateBlocked,
		InitiativeTaskStateRetrying,
		InitiativeTaskStateFailed,
	},
	InitiativeTaskStateInProgress: {
		InitiativeTaskStateReview,
		InitiativeTaskStateVerifying,
		InitiativeTaskStateDone,
		InitiativeTaskStateBlocked,
		InitiativeTaskStateRetrying,
		InitiativeTaskStateFailed,
	},
	InitiativeTaskStateReview: {
		InitiativeTaskStateVerifying,
		InitiativeTaskStateDone,
		InitiativeTaskStateBlocked,
		InitiativeTaskStateRetrying,
		InitiativeTaskStateFailed,
	},
	InitiativeTaskStateVerifying: {
		InitiativeTaskStateDone,
		InitiativeTaskStateRetrying,
		InitiativeTaskStateFailed,
	},
	InitiativeTaskStateBlocked: {
		InitiativeTaskStateReady,
		InitiativeTaskStateInProgress,
		InitiativeTaskStateDone,
		InitiativeTaskStateRetrying,
		InitiativeTaskStateFailed,
	},
	InitiativeTaskStateRetrying: {
		InitiativeTaskStateReady,
		InitiativeTaskStateFailed,
	},
	InitiativeTaskStateDone:   {},
	InitiativeTaskStateFailed: {},
}

// CanTransitionInitiative reports whether the initiative state machine has an
// edge from → to. Unknown statuses have no edges.
func CanTransitionInitiative(from, to string) bool {
	return transitionAllowed(initiativeTransitions, from, to)
}

// CanTransitionInitiativeTask reports whether the task overlay state machine
// has an edge from → to. Unknown states have no edges.
func CanTransitionInitiativeTask(from, to string) bool {
	return transitionAllowed(initiativeTaskTransitions, from, to)
}

// IsInitiativeStatusTerminal reports whether the status has no outgoing edges.
func IsInitiativeStatusTerminal(status string) bool {
	targets, ok := initiativeTransitions[status]
	return ok && len(targets) == 0
}

// IsInitiativeTaskStateTerminal reports whether the task state has no
// outgoing edges.
func IsInitiativeTaskStateTerminal(state string) bool {
	targets, ok := initiativeTaskTransitions[state]
	return ok && len(targets) == 0
}

func transitionAllowed(edges map[string][]string, from, to string) bool {
	return slices.Contains(edges[from], to)
}
