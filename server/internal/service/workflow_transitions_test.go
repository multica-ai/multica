package service

import "testing"

// TestNodeRunTakeoverHandbackTransitions covers the human takeover / handback
// state transitions added for Design Two (real-time CSC session collaboration).
//
// Takeover  = working → blocked   (user pauses the node to intervene)
// Handback  = blocked → working   (user returns control; daemon resumes the
//                                   same CSC session)
// Finalize  = blocked → completed / failed / cancelled
//             (user decides the node's outcome while holding control)
//
// The takeover "blocked" reuses the existing blocked status; it is told apart
// from a rework-exhausted "stuck" blocked by completed_at: takeover goes
// through a dedicated query that leaves completed_at NULL, while a stuck
// blocked sets completed_at via the generic status CASE.
func TestNodeRunTakeoverHandbackTransitions(t *testing.T) {
	allowed := []struct{ from, to string }{
		// Takeover.
		{NodeRunStatusWorking, NodeRunStatusBlocked},
		// Handback.
		{NodeRunStatusBlocked, NodeRunStatusWorking},
		// Finalize while held.
		{NodeRunStatusBlocked, NodeRunStatusCompleted},
		{NodeRunStatusBlocked, NodeRunStatusFailed},
		{NodeRunStatusBlocked, NodeRunStatusCancelled},
		// Pre-existing rework recovery must still hold.
		{NodeRunStatusBlocked, NodeRunStatusFormatOk},
		{NodeRunStatusBlocked, NodeRunStatusSkipped},
	}
	for _, tc := range allowed {
		if !isValidTransition(tc.from, tc.to) {
			t.Errorf("expected transition %s → %s to be allowed", tc.from, tc.to)
		}
	}

	forbidden := []struct{ from, to string }{
		// Cannot jump straight from working to completed; must go through
		// the critic loop or a takeover.
		{NodeRunStatusWorking, NodeRunStatusCompleted},
		// Terminal states never transition out.
		{NodeRunStatusCompleted, NodeRunStatusWorking},
		{NodeRunStatusFailed, NodeRunStatusWorking},
	}
	for _, tc := range forbidden {
		if isValidTransition(tc.from, tc.to) {
			t.Errorf("expected transition %s → %s to be forbidden", tc.from, tc.to)
		}
	}
}
