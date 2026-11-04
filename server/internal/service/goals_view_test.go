package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func pgDate(y int, m time.Month, d int) pgtype.Date {
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

func pgNum(t *testing.T, v float64) pgtype.Numeric {
	t.Helper()
	n, err := numericFromFloat(v)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// fixtureGoal is an active goal for Q4 2026 with a given stored override.
func fixtureGoal(status string, override *string) db.Goal {
	g := db.Goal{
		Title: "Goal", Status: status, ScopeType: ScopeCompany,
		PeriodStart: pgDate(2026, time.October, 1), PeriodEnd: pgDate(2026, time.December, 31),
		Confidence: ConfidenceUnknown,
	}
	if override != nil {
		g.ConfidenceOverride = pgtype.Text{String: *override, Valid: true}
		g.ConfidenceNote = "owner's reason"
	}
	return g
}

// TestBuildGoalViewsWeighsReadingsAndNamesDirection checks the view arithmetic
// without a database: a rising and a falling key result, the weighted progress,
// the unread key result, and the confidence against the period's line.
func TestBuildGoalViewsWeighsReadingsAndNamesDirection(t *testing.T) {
	g := fixtureGoal(GoalStatusActive, nil)
	gid := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	g.ID = gid
	at := pgtype.Timestamptz{Time: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), Valid: true}
	rows := []db.ListKeyResultLatestReadingsRow{
		{ // rising: 500 of 1000, weight 2 -> 0.5
			KeyResultID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, GoalID: gid, Title: "Revenue", MetricKey: "mrr",
			StartValue: pgNum(t, 0), TargetValue: pgNum(t, 1000), Weight: 2, CurrentValue: pgNum(t, 500), ObservedAt: at,
		},
		{ // falling: 30 of 50 towards 10 -> 0.5, weight 2
			KeyResultID: pgtype.UUID{Bytes: [16]byte{3}, Valid: true}, GoalID: gid, Title: "Churn", MetricKey: "churn",
			StartValue: pgNum(t, 50), TargetValue: pgNum(t, 10), Weight: 2, CurrentValue: pgNum(t, 30), ObservedAt: at,
		},
		{ // no reading in the period
			KeyResultID: pgtype.UUID{Bytes: [16]byte{4}, Valid: true}, GoalID: gid, Title: "Runs", MetricKey: "runs.count",
			StartValue: pgNum(t, 0), TargetValue: pgNum(t, 10), Weight: 1,
		},
	}
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	views := buildGoalViews([]db.Goal{g}, rows, now)
	v := views[0]
	if v.Progress == nil || !closeTo(*v.Progress, 0.5) {
		t.Fatalf("progress = %v, want 0.5 (the unread key result is left out)", v.Progress)
	}
	if v.ReadingsMissing != 1 || v.Derived != ConfidenceUnknown {
		t.Fatalf("missing %d derived %s: one key result has no reading, so the confidence is unknown", v.ReadingsMissing, v.Derived)
	}

	// With every key result read, the same progress is judged against the line.
	rows[2].CurrentValue = pgNum(t, 5)
	rows[2].ObservedAt = at
	v = buildGoalViews([]db.Goal{g}, rows, now)[0]
	if v.ReadingsMissing != 0 {
		t.Fatalf("missing %d after reading all", v.ReadingsMissing)
	}
	// progress = (0.5*2 + 0.5*2 + 0.5*1) / 5 = 0.5; expected = 33/92 ~ 0.36, so on track.
	if !closeTo(*v.Progress, 0.5) || v.Derived != ConfidenceOnTrack {
		t.Fatalf("progress %v derived %s, want 0.5 and on_track", *v.Progress, v.Derived)
	}
	if v.Effective != ConfidenceOnTrack || v.Overridden {
		t.Fatalf("effective %s overridden %v with no override", v.Effective, v.Overridden)
	}
}

// TestBuildGoalViewsShowsAnOverrideAsAnOverride: an owner's override is the
// effective value, the derived one is still reported, and a draft is unknown.
func TestBuildGoalViewsShowsAnOverrideAsAnOverride(t *testing.T) {
	off := ConfidenceOffTrack
	g := fixtureGoal(GoalStatusActive, &off)
	g.ID = pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	gid := g.ID
	rows := []db.ListKeyResultLatestReadingsRow{{
		KeyResultID: pgtype.UUID{Bytes: [16]byte{8}, Valid: true}, GoalID: gid, Title: "Revenue", MetricKey: "mrr",
		StartValue: pgNum(t, 0), TargetValue: pgNum(t, 100), Weight: 1, CurrentValue: pgNum(t, 100),
		ObservedAt: pgtype.Timestamptz{Time: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), Valid: true},
	}}
	v := buildGoalViews([]db.Goal{g}, rows, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))[0]
	if v.Derived != ConfidenceOnTrack || v.Effective != ConfidenceOffTrack || !v.Overridden {
		t.Fatalf("derived %s effective %s overridden %v: the override must win and be marked", v.Derived, v.Effective, v.Overridden)
	}

	draft := fixtureGoal(GoalStatusDraft, nil)
	draft.ID = pgtype.UUID{Bytes: [16]byte{7}, Valid: true}
	dv := buildGoalViews([]db.Goal{draft}, []db.ListKeyResultLatestReadingsRow{{
		KeyResultID: pgtype.UUID{Bytes: [16]byte{6}, Valid: true}, GoalID: draft.ID, Title: "x", MetricKey: "mrr",
		StartValue: pgNum(t, 0), TargetValue: pgNum(t, 1), Weight: 1, CurrentValue: pgNum(t, 1),
	}}, time.Now())[0]
	if dv.Derived != ConfidenceUnknown {
		t.Fatalf("a draft goal's confidence is %s, want unknown: it has no line", dv.Derived)
	}
}

// TestPlanningPeriodIsTheCalendarQuarter: the quarter that holds a date starts on
// its first day, in UTC.
func TestPlanningPeriodIsTheCalendarQuarter(t *testing.T) {
	cases := map[string]string{
		"2026-01-01T00:00:00Z":      "2026-01-01",
		"2026-03-31T23:59:00Z":      "2026-01-01",
		"2026-04-01T00:00:00Z":      "2026-04-01",
		"2026-11-03T06:00:00Z":      "2026-10-01",
		"2026-12-31T23:00:00Z":      "2026-10-01",
		"2027-01-05T09:00:00+02:00": "2027-01-01",
	}
	for in, want := range cases {
		at, err := time.Parse(time.RFC3339, in)
		if err != nil {
			t.Fatal(err)
		}
		if got := PlanningPeriodStart(at).Format("2006-01-02"); got != want {
			t.Errorf("PlanningPeriodStart(%s) = %s, want %s", in, got, want)
		}
	}
	if got := planningPeriodLabel(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); got != "Q4 2026" {
		t.Errorf("label = %q, want Q4 2026", got)
	}
}
