package service

import (
	"testing"
	"time"
)

// TestNextScheduleReferenceAnchorsToFiredOccurrence reproduces the double-fire
// bug: a scheduler node whose local clock lags behind the database clock claims
// a trigger the DB already considers due, then recomputes next_run_at from its
// own (still-earlier) clock and lands back on the SAME occurrence — leaving the
// trigger immediately due again. The reference instant must therefore be at
// least the occurrence that was just fired.
func TestNextScheduleReferenceAnchorsToFiredOccurrence(t *testing.T) {
	utc := time.UTC
	fired := time.Date(2026, 6, 24, 9, 0, 0, 0, utc) // occurrence we just fired
	laggingNow := fired.Add(-time.Minute)            // node clock behind DB clock

	ref := NextScheduleReference(laggingNow, fired)
	if !ref.Equal(fired) {
		t.Fatalf("reference must anchor to fired occurrence when the clock lags: got %v, want %v", ref, fired)
	}

	next, err := ComputeNextRunAfter("0 9 * * *", "UTC", ref)
	if err != nil {
		t.Fatalf("ComputeNextRunAfter: %v", err)
	}
	if !next.After(fired) {
		t.Fatalf("next run %v must be strictly after the fired occurrence %v", next, fired)
	}
	want := time.Date(2026, 6, 25, 9, 0, 0, 0, utc)
	if !next.Equal(want) {
		t.Fatalf("next run = %v, want %v (the following day, not a re-fire of the same occurrence)", next, want)
	}
}

func TestNextScheduleReferenceUsesNowWhenClockIsHealthy(t *testing.T) {
	fired := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	now := fired.Add(2 * time.Minute) // healthy node: clock past the fired occurrence

	if ref := NextScheduleReference(now, fired); !ref.Equal(now) {
		t.Fatalf("reference should be now when now is past the fired occurrence: got %v, want %v", ref, now)
	}
}

func TestNextScheduleReferenceFallsBackToNowWithoutFiredOccurrence(t *testing.T) {
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

	if ref := NextScheduleReference(now, time.Time{}); !ref.Equal(now) {
		t.Fatalf("reference should be now when there is no fired occurrence: got %v, want %v", ref, now)
	}
}

func TestComputeNextRunAfterIsStrictlyAfterInput(t *testing.T) {
	at := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC) // exactly a fire time

	next, err := ComputeNextRunAfter("0 9 * * *", "UTC", at)
	if err != nil {
		t.Fatalf("ComputeNextRunAfter: %v", err)
	}
	if !next.After(at) {
		t.Fatalf("ComputeNextRunAfter must return a time strictly after its input: got %v for input %v", next, at)
	}
}
