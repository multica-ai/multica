package agent

import (
	"testing"
	"time"
)

func TestStreamEventCadence_CountsTypesAndGaps(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0)
	c := newStreamEventCadence(start)

	c.observe(start.Add(1*time.Second), "system", false)
	c.observe(start.Add(2*time.Second), "assistant", true)
	// 28s of nothing, then only liveness chatter resumes.
	c.observe(start.Add(30*time.Second), "system", false)
	c.observe(start.Add(31*time.Second), "system", false)
	c.observe(start.Add(40*time.Second), "assistant", true)

	// Stream ends right on the last event, so no trailing silence is folded in.
	s := c.snapshot(start.Add(40 * time.Second))

	if got, want := s.typeCounts, "system=3 assistant=2"; got != want {
		t.Errorf("typeCounts = %q, want %q", got, want)
	}
	if got, want := s.maxGap, 28*time.Second; got != want {
		t.Errorf("maxGap = %s, want %s", got, want)
	}
	if got, want := s.maxGapEndedBy, "system"; got != want {
		t.Errorf("maxGapEndedBy = %q, want %q", got, want)
	}
	// The progress gap spans the liveness chatter: 2s -> 40s. This is the
	// number the idle watchdog threshold has to clear, and the whole reason
	// raw event gaps are not sufficient (MUL-5042).
	if got, want := s.maxProgressGap, 38*time.Second; got != want {
		t.Errorf("maxProgressGap = %s, want %s", got, want)
	}
}

// TestStreamEventCadence_TrailingSilenceIsMeasured covers the shape of the
// first investigated hang: the run works normally, then stops dead after a
// completed tool call and emits nothing until it is killed ~22 minutes later.
// The stall lives entirely after the last event, so it is only visible if the
// snapshot measures to stream end.
func TestStreamEventCadence_TrailingSilenceIsMeasured(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0)
	c := newStreamEventCadence(start)
	c.observe(start.Add(1*time.Second), "assistant", true)
	c.observe(start.Add(2*time.Second), "user", true) // tool result, 6ms-style

	// Killed 22 minutes later having emitted nothing further.
	s := c.snapshot(start.Add(22*time.Minute + 2*time.Second))

	if got, want := s.maxGap, 22*time.Minute; got != want {
		t.Errorf("maxGap = %s, want the trailing stall %s", got, want)
	}
	if got, want := s.maxGapEndedBy, streamEndedLabel; got != want {
		t.Errorf("maxGapEndedBy = %q, want %q", got, want)
	}
	if got, want := s.maxProgressGap, 22*time.Minute; got != want {
		t.Errorf("maxProgressGap = %s, want %s", got, want)
	}
}

// TestStreamEventCadence_HangSignature reproduces the shape of the two
// investigated hangs: a steady liveness cadence and zero progress events. The
// summary has to make that visibly different from a working run, since an
// aggregate event count alone cannot distinguish them.
func TestStreamEventCadence_HangSignature(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0)
	c := newStreamEventCadence(start)

	// ~35 events/min for 10 minutes, matching the observed cadence.
	at := start
	for i := 0; i < 350; i++ {
		at = at.Add(1714 * time.Millisecond)
		c.observe(at, "system", false)
	}

	s := c.snapshot(at)

	if got, want := s.typeCounts, "system=350"; got != want {
		t.Errorf("typeCounts = %q, want %q", got, want)
	}
	// No event ever advanced the run, so the progress gap covers the whole
	// stream even though raw events never stopped arriving. This is the
	// signature that an aggregate event count cannot show.
	if s.maxProgressGap < 9*time.Minute {
		t.Errorf("maxProgressGap = %s, want the full stall (>= 9m)", s.maxProgressGap)
	}
	if s.maxGap > 2*time.Second {
		t.Errorf("maxGap = %s, want a tight liveness cadence (<= 2s)", s.maxGap)
	}
}

func TestStreamEventCadence_TypeSummaryIsDeterministicOnTies(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0)
	c := newStreamEventCadence(start)
	c.observe(start.Add(time.Second), "user", true)
	c.observe(start.Add(2*time.Second), "assistant", true)
	c.observe(start.Add(3*time.Second), "result", true)

	// Equal counts fall back to type name so the field stays greppable.
	if got, want := c.snapshot(start.Add(3*time.Second)).typeCounts, "assistant=1 result=1 user=1"; got != want {
		t.Errorf("typeSummary() = %q, want %q", got, want)
	}
}

func TestStreamEventCadence_EmptyAndNilAreSafe(t *testing.T) {
	t.Parallel()

	var nilCadence *streamEventCadence
	nilCadence.observe(time.Unix(0, 0), "system", false) // must not panic
	if got := nilCadence.snapshot(time.Unix(0, 0)).typeCounts; got != "" {
		t.Errorf("nil typeSummary() = %q, want empty", got)
	}

	c := newStreamEventCadence(time.Unix(0, 0))
	if got := c.snapshot(time.Unix(0, 0)).typeCounts; got != "" {
		t.Errorf("unused typeSummary() = %q, want empty", got)
	}
}

func TestStreamEventCadence_BlankTypeIsLabelled(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0)
	c := newStreamEventCadence(start)
	c.observe(start.Add(time.Second), "", false)

	if got, want := c.snapshot(start.Add(time.Second)).typeCounts, "unknown=1"; got != want {
		t.Errorf("typeSummary() = %q, want %q", got, want)
	}
}

func TestClaudeEventIsProgress(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"assistant": true,
		"user":      true,
		"result":    true,
		// A backend blocked on a dead provider stream keeps emitting these.
		"system":          false,
		"log":             false,
		"control_request": false,
		"":                false,
	}
	for eventType, want := range cases {
		if got := claudeEventIsProgress(eventType); got != want {
			t.Errorf("claudeEventIsProgress(%q) = %v, want %v", eventType, got, want)
		}
	}
}
