package handler

import (
	"errors"
	"testing"
)

// TestCommentMergeTerminalOutcome is Elon round-5 must-fix: a pending-task merge
// must report an HONEST public outcome. Only a real merge is coalesced; a
// fail-closed refusal is blocked/attribution_blocked and any other failure is
// blocked/internal_error — never a fabricated coalesced success. Only
// "no queued task to fold into" is non-terminal (the caller runs the active-task
// decision). Pure-tested because a real DB/attribution fault cannot be forced
// through valid handler inputs, and this mapping is what governs the outcome.
func TestCommentMergeTerminalOutcome(t *testing.T) {
	cases := []struct {
		name         string
		result       commentMergeResult
		wantTerminal bool
		wantStatus   DispatchStatus
		wantReason   DispatchReasonCode
	}{
		{"real merge coalesces", commentMergeSucceeded, true, DispatchCoalesced, ReasonCoalesced},
		{"attribution fail-closed is blocked, not success", commentMergeAttributionBlocked, true, DispatchBlocked, ReasonAttributionBlocked},
		{"unknown error is blocked, not success", commentMergeError, true, DispatchBlocked, ReasonInternalError},
		{"no compatible pending task needs write-backed hand-off", commentMergeNoPendingTask, false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, reason, terminal := commentMergeTerminalOutcome(tc.result)
			if terminal != tc.wantTerminal {
				t.Errorf("terminal = %v, want %v", terminal, tc.wantTerminal)
			}
			if status != tc.wantStatus || reason != tc.wantReason {
				t.Errorf("got %q/%q, want %q/%q", status, reason, tc.wantStatus, tc.wantReason)
			}
			if tc.wantTerminal && status != DispatchCoalesced && status != DispatchBlocked {
				t.Errorf("terminal outcome %q is neither the coalesced success nor a blocked failure", status)
			}
		})
	}
}

// TestDecideSuppressedLeaderOutcome: the self-trigger-suppressed squad leader's
// active-task check must never fake success — a query error is a non-success
// internal_error, a confirmed active run defers, and a confirmed-none is
// self_trigger_suppressed (MUL-4525, Elon round 4).
func TestDecideSuppressedLeaderOutcome(t *testing.T) {
	cases := []struct {
		name       string
		active     bool
		err        error
		wantStatus DispatchStatus
		wantReason DispatchReasonCode
	}{
		{"query error", false, errors.New("db down"), DispatchBlocked, ReasonInternalError},
		{"query error dominates stale active", true, errors.New("db down"), DispatchBlocked, ReasonInternalError},
		{"active run", true, nil, DispatchDeferred, ReasonAlreadyActive},
		{"no active run", false, nil, DispatchBlocked, ReasonSelfTriggerSuppressed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, reason := decideSuppressedLeaderOutcome(tc.active, tc.err)
			if status != tc.wantStatus || reason != tc.wantReason {
				t.Errorf("got %s/%s, want %s/%s", status, reason, tc.wantStatus, tc.wantReason)
			}
		})
	}
}
