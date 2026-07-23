package service

import (
	"slices"
	"testing"
)

func TestCanTransitionInitiative(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		// Happy path through the lifecycle.
		{"draft to planning", InitiativeStatusDraft, InitiativeStatusPlanning, true},
		{"planning to plan_review", InitiativeStatusPlanning, InitiativeStatusPlanReview, true},
		{"planning to executing (autonomy 3 auto-approve)", InitiativeStatusPlanning, InitiativeStatusExecuting, true},
		{"plan_review to executing (approve)", InitiativeStatusPlanReview, InitiativeStatusExecuting, true},
		{"plan_review to planning (replan)", InitiativeStatusPlanReview, InitiativeStatusPlanning, true},
		{"executing to integrating", InitiativeStatusExecuting, InitiativeStatusIntegrating, true},
		{"integrating to verifying", InitiativeStatusIntegrating, InitiativeStatusVerifying, true},
		{"integrating to executing (rework)", InitiativeStatusIntegrating, InitiativeStatusExecuting, true},
		{"verifying to done", InitiativeStatusVerifying, InitiativeStatusDone, true},
		{"verifying to executing (rework)", InitiativeStatusVerifying, InitiativeStatusExecuting, true},

		// Escalation and kill-switch edges.
		{"executing to needs_human", InitiativeStatusExecuting, InitiativeStatusNeedsHuman, true},
		{"needs_human back to executing", InitiativeStatusNeedsHuman, InitiativeStatusExecuting, true},
		{"executing to paused", InitiativeStatusExecuting, InitiativeStatusPaused, true},
		{"paused resumes to executing", InitiativeStatusPaused, InitiativeStatusExecuting, true},
		{"paused to cancelled", InitiativeStatusPaused, InitiativeStatusCancelled, true},
		{"draft to cancelled", InitiativeStatusDraft, InitiativeStatusCancelled, true},

		// Forbidden edges.
		{"draft cannot skip to executing", InitiativeStatusDraft, InitiativeStatusExecuting, false},
		{"draft cannot pause", InitiativeStatusDraft, InitiativeStatusPaused, false},
		{"executing cannot jump to done", InitiativeStatusExecuting, InitiativeStatusDone, false},
		{"plan_review cannot fail", InitiativeStatusPlanReview, InitiativeStatusFailed, false},
		{"paused cannot resume to draft", InitiativeStatusPaused, InitiativeStatusDraft, false},

		// Terminal statuses have no outgoing edges.
		{"done is terminal", InitiativeStatusDone, InitiativeStatusExecuting, false},
		{"cancelled is terminal", InitiativeStatusCancelled, InitiativeStatusPlanning, false},
		{"failed is terminal", InitiativeStatusFailed, InitiativeStatusPlanning, false},

		// Unknown statuses have no edges in either direction.
		{"unknown from", "bogus", InitiativeStatusPlanning, false},
		{"unknown to", InitiativeStatusDraft, "bogus", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransitionInitiative(tt.from, tt.to); got != tt.want {
				t.Errorf("CanTransitionInitiative(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestCanTransitionInitiativeTask(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		// Happy path.
		{"pending to ready", InitiativeTaskStatePending, InitiativeTaskStateReady, true},
		{"ready to dispatched", InitiativeTaskStateReady, InitiativeTaskStateDispatched, true},
		{"dispatched to in_progress", InitiativeTaskStateDispatched, InitiativeTaskStateInProgress, true},
		{"in_progress to review", InitiativeTaskStateInProgress, InitiativeTaskStateReview, true},
		{"review to verifying", InitiativeTaskStateReview, InitiativeTaskStateVerifying, true},
		{"verifying to done", InitiativeTaskStateVerifying, InitiativeTaskStateDone, true},

		// Human closes the linked issue from any active state.
		{"dispatched to done (human closed issue)", InitiativeTaskStateDispatched, InitiativeTaskStateDone, true},
		{"blocked to done (human closed issue)", InitiativeTaskStateBlocked, InitiativeTaskStateDone, true},

		// Blocker and retry-ladder edges.
		{"in_progress to blocked", InitiativeTaskStateInProgress, InitiativeTaskStateBlocked, true},
		{"blocked resumes in_progress", InitiativeTaskStateBlocked, InitiativeTaskStateInProgress, true},
		{"blocked re-enters ready", InitiativeTaskStateBlocked, InitiativeTaskStateReady, true},
		{"in_progress to retrying", InitiativeTaskStateInProgress, InitiativeTaskStateRetrying, true},
		{"retrying re-enters ready", InitiativeTaskStateRetrying, InitiativeTaskStateReady, true},
		{"retrying exhausts to failed", InitiativeTaskStateRetrying, InitiativeTaskStateFailed, true},

		// Forbidden edges.
		{"pending cannot dispatch", InitiativeTaskStatePending, InitiativeTaskStateDispatched, false},
		{"pending cannot be done", InitiativeTaskStatePending, InitiativeTaskStateDone, false},
		{"ready cannot block", InitiativeTaskStateReady, InitiativeTaskStateBlocked, false},
		{"verifying cannot block", InitiativeTaskStateVerifying, InitiativeTaskStateBlocked, false},

		// Terminal states.
		{"done is terminal", InitiativeTaskStateDone, InitiativeTaskStateRetrying, false},
		{"failed is terminal", InitiativeTaskStateFailed, InitiativeTaskStateReady, false},

		// Unknown states.
		{"unknown from", "bogus", InitiativeTaskStateReady, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransitionInitiativeTask(tt.from, tt.to); got != tt.want {
				t.Errorf("CanTransitionInitiativeTask(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestInitiativeTerminalStatuses(t *testing.T) {
	for _, status := range []string{InitiativeStatusDone, InitiativeStatusCancelled, InitiativeStatusFailed} {
		if !IsInitiativeStatusTerminal(status) {
			t.Errorf("expected %q to be terminal", status)
		}
	}
	for _, status := range ActiveInitiativeStatuses {
		if IsInitiativeStatusTerminal(status) {
			t.Errorf("expected active status %q to not be terminal", status)
		}
	}
	if IsInitiativeStatusTerminal("bogus") {
		t.Error("unknown status must not be reported terminal")
	}
	if !IsInitiativeTaskStateTerminal(InitiativeTaskStateDone) || !IsInitiativeTaskStateTerminal(InitiativeTaskStateFailed) {
		t.Error("done/failed task states must be terminal")
	}
}

// TestActiveInitiativeStatusesMatchTransitionMap guards the three places the
// "active" set is duplicated (this slice, ListActiveInitiativeIDs, and the
// idx_initiative_active partial index): every active status must exist in the
// transition map with at least one outgoing edge, and every non-terminal
// non-draft status must be listed as active.
func TestActiveInitiativeStatusesMatchTransitionMap(t *testing.T) {
	for _, status := range ActiveInitiativeStatuses {
		targets, ok := initiativeTransitions[status]
		if !ok || len(targets) == 0 {
			t.Errorf("active status %q missing from transition map or terminal", status)
		}
	}
	for status, targets := range initiativeTransitions {
		if len(targets) == 0 || status == InitiativeStatusDraft || status == InitiativeStatusPaused {
			continue
		}
		if !slices.Contains(ActiveInitiativeStatuses, status) {
			t.Errorf("non-terminal status %q missing from ActiveInitiativeStatuses", status)
		}
	}
}
