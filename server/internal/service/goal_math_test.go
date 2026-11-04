package service

import (
	"math"
	"testing"
	"time"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestKeyResultProgressIsClampedAndDirectional covers the progress formula of
// spec 4.1 for rising and falling metrics, the clamp at both ends, and a
// degenerate span that storage would refuse anyway.
func TestKeyResultProgressIsClampedAndDirectional(t *testing.T) {
	cases := []struct {
		name                   string
		start, target, current float64
		want                   float64
	}{
		{"rising midway", 0, 100, 50, 0.5},
		{"rising at start", 0, 100, 0, 0},
		{"rising at target", 0, 100, 100, 1},
		{"rising past target clamps to one", 0, 100, 250, 1},
		{"rising below start clamps to zero", 10, 110, -40, 0},
		{"rising from a non-zero start", 1200, 2000, 1600, 0.5},
		{"falling metric improves as it falls", 50, 10, 30, 0.5},
		{"falling metric above start is no progress", 50, 10, 60, 0},
		{"falling metric at target", 50, 10, 10, 1},
		{"falling metric past target clamps", 50, 10, -5, 1},
		{"zero span is no progress", 5, 5, 9, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := KeyResultProgress(c.start, c.target, c.current); !approx(got, c.want) {
				t.Fatalf("KeyResultProgress(%v,%v,%v) = %v, want %v", c.start, c.target, c.current, got, c.want)
			}
		})
	}
}

// TestExpectedShareIsALineThroughThePeriod checks the straight line the
// confidence is judged against: zero before the start, one after the end, and
// proportional in between.
func TestExpectedShareIsALineThroughThePeriod(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC) // 91 days
	span := end.Sub(start)
	cases := []struct {
		name string
		now  time.Time
		want float64
	}{
		{"before the period", start.Add(-time.Hour), 0},
		{"at the start", start, 0},
		{"halfway", start.Add(span / 2), 0.5},
		{"at the end", end, 1},
		{"after the end", end.Add(48 * time.Hour), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExpectedShare(start, end, c.now); !approx(got, c.want) {
				t.Fatalf("ExpectedShare = %v, want %v", got, c.want)
			}
		})
	}
	if got := ExpectedShare(end, start, start); got != 1 {
		t.Fatalf("a period that does not run forward has no line: got %v, want 1", got)
	}
}

// TestGoalProgressIsTheWeightedMeanOfReadKeyResults covers the weighting, the
// exclusion of key results with no reading, and the no-reading case.
func TestGoalProgressIsTheWeightedMeanOfReadKeyResults(t *testing.T) {
	cases := []struct {
		name   string
		krs    []KeyResultState
		want   float64
		wantOK bool
	}{
		{"no key results", nil, 0, false},
		{"nothing read", []KeyResultState{{Progress: 0.9, Weight: 1}}, 0, false},
		{"single read", []KeyResultState{{Progress: 0.4, Weight: 3, HasRead: true}}, 0.4, true},
		{"weighted", []KeyResultState{
			{Progress: 1, Weight: 1, HasRead: true},
			{Progress: 0, Weight: 3, HasRead: true},
		}, 0.25, true},
		{"unread key result is left out of the mean", []KeyResultState{
			{Progress: 0.6, Weight: 2, HasRead: true},
			{Progress: 0, Weight: 8, HasRead: false},
		}, 0.6, true},
		{"zero weight is ignored", []KeyResultState{
			{Progress: 1, Weight: 0, HasRead: true},
			{Progress: 0.2, Weight: 1, HasRead: true},
		}, 0.2, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := GoalProgress(c.krs)
			if ok != c.wantOK || (ok && !approx(got, c.want)) {
				t.Fatalf("GoalProgress = (%v, %v), want (%v, %v)", got, ok, c.want, c.wantOK)
			}
		})
	}
}

// TestDeriveConfidenceUsesTheBandsOfSpec41 walks the line: on track at the
// expected share minus ten points, at risk down to minus thirty, off track
// below that, and unknown whenever a key result has no reading.
func TestDeriveConfidenceUsesTheBandsOfSpec41(t *testing.T) {
	cases := []struct {
		name     string
		progress float64
		expected float64
		allRead  bool
		want     string
	}{
		{"no reading yet is unknown", 0.9, 0.5, false, ConfidenceUnknown},
		{"ahead of the line", 0.8, 0.5, true, ConfidenceOnTrack},
		{"on the line", 0.5, 0.5, true, ConfidenceOnTrack},
		{"exactly ten points behind is still on track", 0.4, 0.5, true, ConfidenceOnTrack},
		{"just over ten points behind is at risk", 0.39, 0.5, true, ConfidenceAtRisk},
		{"exactly thirty points behind is at risk", 0.2, 0.5, true, ConfidenceAtRisk},
		{"just over thirty points behind is off track", 0.19, 0.5, true, ConfidenceOffTrack},
		{"no progress at the end is off track", 0, 1, true, ConfidenceOffTrack},
		{"the period has not started, nothing is behind", 0, 0, true, ConfidenceOnTrack},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeriveConfidence(c.progress, c.expected, c.allRead); got != c.want {
				t.Fatalf("DeriveConfidence(%v, %v, %v) = %q, want %q", c.progress, c.expected, c.allRead, got, c.want)
			}
		})
	}
}

// TestEffectiveConfidenceShowsTheOverride checks that an owner's override wins
// and that an empty override is no override.
func TestEffectiveConfidenceShowsTheOverride(t *testing.T) {
	off := ConfidenceOffTrack
	empty := ""
	if got := EffectiveConfidence(ConfidenceAtRisk, nil); got != ConfidenceAtRisk {
		t.Fatalf("no override: got %q, want the derived value", got)
	}
	if got := EffectiveConfidence(ConfidenceAtRisk, &off); got != ConfidenceOffTrack {
		t.Fatalf("override: got %q, want %q", got, ConfidenceOffTrack)
	}
	if got := EffectiveConfidence(ConfidenceAtRisk, &empty); got != ConfidenceAtRisk {
		t.Fatalf("empty override: got %q, want the derived value", got)
	}
}
