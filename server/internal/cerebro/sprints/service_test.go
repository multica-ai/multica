package sprints

import (
	"testing"
	"time"
)

func TestComputeNextStart_Weekly_SnapsToStartWeekday(t *testing.T) {
	// Previous sprint ended on a Sunday (2026-01-04). Next sprint should
	// start on the configured start_weekday (Monday=1) which is the next
	// day. The function takes "previousEnd+1" then snaps forward.
	prevEnd := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)
	got := ComputeNextStart(prevEnd, UnitWeek, 1)
	want := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("week unit snap: got %s want %s", got, want)
	}
}

func TestComputeNextStart_Weekly_AcrossWeek(t *testing.T) {
	// Prev sprint ended on Tuesday 2026-01-06; configured weekday Mon=1.
	// Next start is the *next* Monday: 2026-01-12.
	prevEnd := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	got := ComputeNextStart(prevEnd, UnitWeek, 1)
	want := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("week unit across week: got %s want %s", got, want)
	}
}

func TestComputeNextStart_Monthly_SnapsToFirst(t *testing.T) {
	prevEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	got := ComputeNextStart(prevEnd, UnitMonth, 1)
	want := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("month unit: got %s want %s", got, want)
	}
}

func TestComputeNextStart_Daily_IsExactlyNextDay(t *testing.T) {
	prevEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	got := ComputeNextStart(prevEnd, UnitDay, 1)
	want := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("day unit: got %s want %s", got, want)
	}
}

func TestComputeEnd_WeekTwoSprintsInclusive(t *testing.T) {
	// 2-week sprint starting Mon 2026-01-05 ends Sun 2026-01-18 (14 days,
	// inclusive end).
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	got := ComputeEnd(start, UnitWeek, 2)
	want := time.Date(2026, 1, 18, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("week*2 end: got %s want %s", got, want)
	}
}

func TestComputeEnd_MonthOneInclusive(t *testing.T) {
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	got := ComputeEnd(start, UnitMonth, 1)
	want := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("month end: got %s want %s", got, want)
	}
}

func TestApplyNameTemplate(t *testing.T) {
	cases := []struct {
		template string
		seq      int32
		want     string
	}{
		{"Sprint {n}", 5, "Sprint 5"},
		{"S{n}", 12, "S12"},
		{"Cycle", 3, "Cycle 3"},
		{"", 4, "Sprint 4"},
	}
	for _, c := range cases {
		if got := ApplyNameTemplate(c.template, c.seq); got != c.want {
			t.Errorf("template=%q seq=%d: got %q want %q", c.template, c.seq, got, c.want)
		}
	}
}

func TestValidateUnit(t *testing.T) {
	for _, ok := range []string{"day", "week", "month"} {
		if err := ValidateUnit(ok); err != nil {
			t.Errorf("expected %q ok, got %v", ok, err)
		}
	}
	if err := ValidateUnit("year"); err == nil {
		t.Error("expected error for unknown unit")
	}
}

func TestTodayIn_TimezoneBoundary(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	// 2026-03-10 02:00 UTC is 2026-03-09 18:00 in LA — TodayIn should
	// return 2026-03-09 00:00 LA, not the UTC date.
	now := time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC)
	got := TodayIn(loc, now)
	want := time.Date(2026, 3, 9, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("tz boundary: got %s want %s", got, want)
	}
}
