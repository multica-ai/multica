package handler

import (
	"errors"
	"testing"
)

// TestDecidePostMergeMiss is Elon round-4 must-fix: the active-task check governs
// what happens after a comment merge misses. A query FAILURE must fail closed —
// never enqueue a fresh task (a duplicate concurrent-run risk) and never report a
// success. Only a confirmed active task defers; a confirmed-none enqueues fresh.
// Unit-tested here because a real DB fault cannot be forced through valid inputs,
// and this decision — not the query call — is what actually governs the branch.
func TestDecidePostMergeMiss(t *testing.T) {
	t.Run("query error: fail closed, non-success internal_error", func(t *testing.T) {
		status, reason, enqueueFresh := decidePostMergeMiss(false, errors.New("db down"))
		if enqueueFresh {
			t.Error("enqueueFresh = true on query error; must fail closed to avoid a duplicate run")
		}
		if status != DispatchBlocked || reason != ReasonInternalError {
			t.Errorf("got %s/%s, want blocked/internal_error", status, reason)
		}
	})
	t.Run("query error dominates a stale active=true", func(t *testing.T) {
		status, _, enqueueFresh := decidePostMergeMiss(true, errors.New("db down"))
		if enqueueFresh || status != DispatchBlocked {
			t.Errorf("got status %s enqueueFresh %v, want blocked + no fresh enqueue", status, enqueueFresh)
		}
	})
	t.Run("active task: defer, no fresh enqueue", func(t *testing.T) {
		status, reason, enqueueFresh := decidePostMergeMiss(true, nil)
		if enqueueFresh || status != DispatchDeferred || reason != ReasonDeferred {
			t.Errorf("got %s/%s enqueueFresh %v, want deferred + no fresh enqueue", status, reason, enqueueFresh)
		}
	})
	t.Run("no active task: enqueue a fresh follow-up", func(t *testing.T) {
		_, _, enqueueFresh := decidePostMergeMiss(false, nil)
		if !enqueueFresh {
			t.Error("enqueueFresh = false with no active task; a fresh follow-up must run")
		}
	})
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
