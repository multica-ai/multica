package claimwindow

import (
	"testing"
	"time"
)

func TestParseHHMM(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
		ok   bool
	}{
		{"00:00", 0, true},
		{"02:30", 150, true},
		{"23:59", 1439, true},
		{"2:30", 0, false},
		{"24:00", 0, false},
		{"02:60", 0, false},
	} {
		got, err := ParseHHMM(tc.raw)
		if tc.ok && (err != nil || got != tc.want) {
			t.Fatalf("ParseHHMM(%q) = %d, %v; want %d", tc.raw, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Fatalf("ParseHHMM(%q) unexpectedly succeeded", tc.raw)
		}
	}
}

func TestEvaluateBoundariesAndMidnight(t *testing.T) {
	tests := []struct {
		name  string
		now   string
		start int
		open  bool
	}{
		{"before", "2026-06-22T01:59:59Z", 120, false},
		{"at start", "2026-06-22T02:00:00Z", 120, true},
		{"before end", "2026-06-22T06:59:59Z", 120, true},
		{"at end", "2026-06-22T07:00:00Z", 120, false},
		{"cross midnight", "2026-06-23T02:00:00Z", 23 * 60, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tc.now)
			state, err := Evaluate(now, tc.start, "UTC")
			if err != nil || state.Open != tc.open {
				t.Fatalf("Evaluate() open=%v err=%v; want %v", state.Open, err, tc.open)
			}
			if state.End.Sub(state.Start) != Duration {
				t.Fatalf("duration=%v; want %v", state.End.Sub(state.Start), Duration)
			}
		})
	}
}

func TestEvaluateTracksNextStart(t *testing.T) {
	before, _ := time.Parse(time.RFC3339, "2026-06-22T01:00:00Z")
	state, err := Evaluate(before, 2*60, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if got := state.NextStart.Format(time.RFC3339); got != "2026-06-22T02:00:00Z" {
		t.Fatalf("next start=%s", got)
	}

	open, _ := time.Parse(time.RFC3339, "2026-06-22T03:00:00Z")
	state, err = Evaluate(open, 2*60, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if got := state.NextStart.Format(time.RFC3339); got != "2026-06-23T02:00:00Z" {
		t.Fatalf("next start=%s", got)
	}
}

func TestEvaluateWarsawDST(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Warsaw")

	// 02:30 does not exist on spring-forward day; start at first valid instant, 03:00.
	springNow := time.Date(2026, 3, 29, 3, 1, 0, 0, loc)
	spring, err := Evaluate(springNow, 2*60+30, "Europe/Warsaw")
	if err != nil || spring.Start.In(loc).Format("15:04") != "03:00" {
		t.Fatalf("spring start=%s err=%v", spring.Start.In(loc), err)
	}
	if spring.End.Sub(spring.Start) != 5*time.Hour {
		t.Fatalf("spring duration=%v", spring.End.Sub(spring.Start))
	}

	// 02:30 occurs twice on fall-back day; use the first instant (CEST, UTC+2).
	fallNow := time.Date(2026, 10, 25, 2, 31, 0, 0, loc)
	fall, err := Evaluate(fallNow, 2*60+30, "Europe/Warsaw")
	_, offset := fall.Start.In(loc).Zone()
	if err != nil || offset != 2*60*60 {
		t.Fatalf("fall start=%s offset=%d err=%v", fall.Start.In(loc), offset, err)
	}
}

func TestEvaluateRejectsInvalidTimezone(t *testing.T) {
	_, err := Evaluate(time.Now(), 120, "Mars/Olympus")
	if err == nil {
		t.Fatal("expected invalid timezone error")
	}
}

func TestFormatHHMM(t *testing.T) {
	if got := FormatHHMM(150); got != "02:30" {
		t.Fatalf("got %q", got)
	}
}
