package notetypes

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func monthlyType(createdAt time.Time, weekday, weekOfMonth int16, count int32) cerebrodb.CerebroNoteType {
	return cerebrodb.CerebroNoteType{
		CadenceUnit:       CadenceMonth,
		CadenceCount:      count,
		CreatedAt:         pgtype.Timestamptz{Time: createdAt, Valid: true},
		AnchorWeekday:     pgtype.Int2{Int16: weekday, Valid: true},
		AnchorWeekOfMonth: pgtype.Int2{Int16: weekOfMonth, Valid: true},
	}
}

func TestNthWeekdayOfMonth(t *testing.T) {
	cases := []struct {
		name    string
		year    int
		month   time.Month
		weekday int
		ordinal int
		want    string
	}{
		{"3rd Monday July 2026", 2026, time.July, 1, 3, "2026-07-20"},
		{"1st Monday July 2026", 2026, time.July, 1, 1, "2026-07-06"},
		{"last Monday July 2026", 2026, time.July, 1, -1, "2026-07-27"},
		{"5th Monday July 2026 clamps to last", 2026, time.July, 1, 5, "2026-07-27"},
		{"2nd Friday Feb 2026", 2026, time.February, 5, 2, "2026-02-13"},
		{"last Friday Feb 2026", 2026, time.February, 5, -1, "2026-02-27"},
	}
	for _, tc := range cases {
		got := nthWeekdayOfMonth(tc.year, tc.month, tc.weekday, tc.ordinal).Format("2006-01-02")
		if got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestUpcoming_MonthlyThirdMonday(t *testing.T) {
	// Created mid-June; "3rd Monday every month".
	nt := monthlyType(time.Date(2026, time.June, 10, 9, 0, 0, 0, time.UTC), 1, 3, 1)
	now := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)
	got := Upcoming(nt, now, 3)
	want := []string{"2026-07-20", "2026-08-17", "2026-09-21"}
	if len(got) != len(want) {
		t.Fatalf("got %d dates, want %d", len(got), len(want))
	}
	for i := range want {
		if g := got[i].Format("2006-01-02"); g != want[i] {
			t.Errorf("date %d: got %s, want %s", i, g, want[i])
		}
	}
}

func TestUpcoming_SkipsPassedMeetingThisMonth(t *testing.T) {
	nt := monthlyType(time.Date(2026, time.June, 10, 9, 0, 0, 0, time.UTC), 1, 3, 1)
	// "now" is after the 3rd Monday of July (the 20th) → next is August.
	now := time.Date(2026, time.July, 21, 8, 0, 0, 0, time.UTC)
	next, ok := NextOccurrence(nt, now)
	if !ok {
		t.Fatal("expected an occurrence")
	}
	if got := next.Format("2006-01-02"); got != "2026-08-17" {
		t.Errorf("got %s, want 2026-08-17", got)
	}
}

func TestShouldSweepApply_MonthlyWeekAnchorFiresOnMeetingDay(t *testing.T) {
	// Created before the July meeting; every month on the 3rd Monday.
	nt := monthlyType(time.Date(2026, time.June, 10, 9, 0, 0, 0, time.UTC), 1, 3, 1)
	if shouldSweepApply(nt, time.Date(2026, time.July, 19, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("should not fire the day before the 3rd Monday")
	}
	if !shouldSweepApply(nt, time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("should fire on the 3rd Monday")
	}
}

func TestUpcoming_ManualReturnsNothing(t *testing.T) {
	nt := cerebrodb.CerebroNoteType{CadenceUnit: CadenceManual, CadenceCount: 1}
	if got := Upcoming(nt, time.Now(), 3); len(got) != 0 {
		t.Fatalf("manual cadence should yield no dates, got %d", len(got))
	}
}
