package notetypes

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func noteTypeCreatedAt(createdAt time.Time, count int32, anchorWeekday int16) cerebrodb.CerebroNoteType {
	return cerebrodb.CerebroNoteType{
		CadenceUnit:   CadenceWeek,
		CadenceCount:  count,
		CreatedAt:     pgtype.Timestamptz{Time: createdAt, Valid: true},
		AnchorWeekday: pgtype.Int2{Int16: anchorWeekday, Valid: true},
	}
}

func TestPeriodAnchor_WeeklyAnchorWeekdayUsesSelectedWeekday(t *testing.T) {
	createdAt := time.Date(2026, time.June, 16, 10, 0, 0, 0, time.UTC) // Tuesday
	nt := noteTypeCreatedAt(createdAt, 2, 1)                           // Monday

	got := periodAnchor(nt)
	if want := "2026-06-15"; got.Format("2006-01-02") != want {
		t.Fatalf("periodAnchor() = %s, want %s", got.Format("2006-01-02"), want)
	}
}

func TestShouldSweepApply_WeeklyAnchorWaitsUntilNextAnchoredPeriod(t *testing.T) {
	createdAt := time.Date(2026, time.June, 16, 10, 0, 0, 0, time.UTC) // Tuesday after the meeting
	nt := noteTypeCreatedAt(createdAt, 2, 1)                           // Every 2 weeks on Monday

	if shouldSweepApply(nt, time.Date(2026, time.June, 17, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("should not materialise in the already-started anchor period")
	}
	if !shouldSweepApply(nt, time.Date(2026, time.June, 29, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("should materialise on the next selected Monday period")
	}
}

func TestShouldSweepApply_WeeklyAnchorSkipsCreationDayPeriod(t *testing.T) {
	createdAt := time.Date(2026, time.June, 15, 15, 0, 0, 0, time.UTC) // Monday after the meeting
	nt := noteTypeCreatedAt(createdAt, 2, 1)                           // Every 2 weeks on Monday

	if shouldSweepApply(nt, time.Date(2026, time.June, 15, 16, 0, 0, 0, time.UTC)) {
		t.Fatal("should not materialise the same anchored period as the creation day")
	}
	if !shouldSweepApply(nt, time.Date(2026, time.June, 29, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("should materialise on the next selected Monday period")
	}
}
