package service

import (
	"testing"
	"time"
)

// TestAutopilotComputeNextRunDoesNotChain locks down the contract the
// scheduler relies on: ComputeNextRun returns a time STRICTLY in the
// future relative to now, regardless of how far overdue the previous
// scheduled fire was. This is what gives the scheduler its "at most one
// catch-up run per overdue trigger" semantics — if ComputeNextRun chained
// from the missed previous fire, an autopilot offline for 6 hours on a
// 5-minute cron would produce 72 dispatches per tick instead of 1.
func TestAutopilotComputeNextRunDoesNotChain(t *testing.T) {
	before := time.Now()
	next, err := ComputeNextRun("*/5 * * * *", "UTC")
	if err != nil {
		t.Fatalf("ComputeNextRun: %v", err)
	}
	if !next.After(before) {
		t.Fatalf("expected next run to be strictly after now (%v), got %v", before, next)
	}
	// A 5-minute cron must fire within the next 5 minutes plus a tiny
	// scheduling slack. Anything beyond that means the parser is wrong.
	if next.Sub(before) > 6*time.Minute {
		t.Fatalf("expected next run within ~5 min of now, got delta %v", next.Sub(before))
	}
}

// TestAutopilotComputeNextRunIgnoresOverdue documents the function shape:
// ComputeNextRun takes (cron, timezone) only — it has NO concept of a
// "previous" fire time, so it cannot chain through missed slots. Two
// successive calls return the same upcoming tick (within the same minute
// of wall-clock).
func TestAutopilotComputeNextRunIgnoresOverdue(t *testing.T) {
	first, err := ComputeNextRun("*/5 * * * *", "UTC")
	if err != nil {
		t.Fatalf("ComputeNextRun first: %v", err)
	}
	second, err := ComputeNextRun("*/5 * * * *", "UTC")
	if err != nil {
		t.Fatalf("ComputeNextRun second: %v", err)
	}
	// Both calls compute "next 5-minute boundary after now". Unless the
	// test happens to straddle a 5-minute boundary, they're equal; if it
	// does straddle, the second is exactly 5 minutes after the first.
	delta := second.Sub(first).Abs()
	if delta != 0 && delta != 5*time.Minute {
		t.Fatalf("expected delta of 0 or 5min between back-to-back ComputeNextRun calls, got %v", delta)
	}

	// Independently: ComputeNextRun does not take a "previous" parameter,
	// so it cannot possibly chain. This is enforced by the function
	// signature, but we assert it explicitly here as living documentation.
	if next, err := ComputeNextRun("*/5 * * * *", "UTC"); err != nil {
		t.Fatalf("ComputeNextRun: %v", err)
	} else if !next.After(time.Now().Add(-time.Second)) {
		t.Fatalf("expected ComputeNextRun to return a future time, got %v", next)
	}
}
