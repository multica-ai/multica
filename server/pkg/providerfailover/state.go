package providerfailover

// HandoffState is the lifecycle state of a provider_failover_handoff ledger
// row. The string forms are persisted (agent_task_queue-style wire stability):
// renaming one is a breaking change and must be paired with a migration and a
// CHECK-constraint update.
type HandoffState string

const (
	// StatePending: active-mode failover has atomically claimed ownership of the
	// task chain but has not yet created the Claude fallback task. This is the
	// state that supersedes a late primary completion.
	StatePending HandoffState = "HANDOFF_PENDING"
	// StateDispatched: the Claude fallback task has been created and enqueued.
	StateDispatched HandoffState = "HANDOFF_DISPATCHED"
	// StateCompleted: the Claude fallback task finished. Terminal. The fallback
	// retains chain ownership; a late primary completion is discarded by the
	// CompleteTask supersede guard WITHOUT releasing this ownership.
	StateCompleted HandoffState = "HANDOFF_COMPLETED"
	// StateFailed: the handoff could not proceed (Claude unavailable) or the
	// Claude fallback itself failed. The user sees an explicit FAILED with a
	// ledger reference. Terminal.
	StateFailed HandoffState = "HANDOFF_FAILED"
	// StateShadow: shadow-mode record. The full policy verdict is captured
	// (WouldFailOver + reason) but no task outcome, binding, or dispatch changed.
	// Terminal and non-owning — shadow rows never claim a chain.
	StateShadow HandoffState = "HANDOFF_SHADOW"
	// StateDeclined: active-mode policy declined the handoff. Terminal and
	// non-owning.
	StateDeclined HandoffState = "HANDOFF_DECLINED"
)

// owningStates are the states in which a handoff row owns its task chain — no
// other handoff may be created for the same chain while one of these is live.
// Kept in lockstep with the partial predicate of the chain-owner unique index
// (migration 226).
var owningStates = map[HandoffState]bool{
	StatePending:    true,
	StateDispatched: true,
	StateCompleted:  true,
}

// IsOwning reports whether the state owns its task chain.
func (s HandoffState) IsOwning() bool { return owningStates[s] }

// IsTerminal reports whether the state is final (no further transition).
func (s HandoffState) IsTerminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateShadow, StateDeclined:
		return true
	default:
		return false
	}
}

// validTransitions is the legal state graph. Only the active-mode owning states
// (PENDING/DISPATCHED) transition; StateShadow/StateDeclined are recorded
// terminal and never move. The two owning-but-live states move to a terminal
// COMPLETED/FAILED as the dispatch and the fallback run resolve.
var validTransitions = map[HandoffState]map[HandoffState]bool{
	StatePending: {
		StateDispatched: true, // target resolved + fallback task created
		StateFailed:     true, // dispatch aborted before the fallback task exists
	},
	StateDispatched: {
		StateCompleted: true, // Claude fallback finished
		StateFailed:    true, // Claude fallback failed
	},
	StateCompleted: {},
	StateFailed:    {},
	StateShadow:    {},
	StateDeclined:  {},
}

// CanTransition reports whether from → to is a legal state change.
func CanTransition(from, to HandoffState) bool {
	return validTransitions[from][to]
}
