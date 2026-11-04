package service

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func tsAt(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func TestRetroWindowIsFourteenDays(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	if !RetroDueForSubject(pgtype.Timestamptz{}, now) {
		t.Fatal("a subject that never fired is due")
	}
	if RetroDueForSubject(tsAt(now.Add(-13*24*time.Hour)), now) {
		t.Fatal("a subject that fired 13 days ago is still inside its window")
	}
	if !RetroDueForSubject(tsAt(now.Add(-RetroWindow)), now) {
		t.Fatal("a subject that fired exactly one window ago is due again")
	}
}

func TestRetroRejectedInARowNeedsThreeNewestRejections(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	squad := func(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }
	row := func(sq pgtype.UUID, state string, hours int) db.ListSquadRecentDecisionsRow {
		return db.ListSquadRecentDecisionsRow{SquadID: sq, State: state, DecidedAt: tsAt(now.Add(-time.Duration(hours) * time.Hour))}
	}
	rows := []db.ListSquadRecentDecisionsRow{
		// Three rejections in a row, newest first, decided today.
		row(squad(1), ProposalStateRejected, 1), row(squad(1), ProposalStateRejected, 2), row(squad(1), ProposalStateRejected, 3),
		// Two rejections and an approval: not a run.
		row(squad(2), ProposalStateRejected, 1), row(squad(2), ProposalStateApproved, 2), row(squad(2), ProposalStateRejected, 3),
		// Three rejections, but the newest decision is older than the window.
		row(squad(3), ProposalStateRejected, 24*20), row(squad(3), ProposalStateRejected, 24*21), row(squad(3), ProposalStateRejected, 24*22),
		// Only two decisions exist.
		row(squad(4), ProposalStateRejected, 1), row(squad(4), ProposalStateRejected, 2),
	}
	got := RetroRejectedInARow(rows, now.Add(-RetroWindow))
	if len(got) != 1 || got[0] != squad(1) {
		t.Fatalf("want only squad 1, got %v", got)
	}
}

func TestRetroStageBreachVisitsCountsVisitsPastTheirSLA(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-RetroWindow)
	sla := map[string]int{"define": 7, "build": 10, "launch": 0}
	day := func(d int) time.Time { return now.Add(time.Duration(d) * 24 * time.Hour) }

	// define entered 40 days ago and left 30 days ago (SLA 7 breached), then
	// launch, which has no SLA. The breached visit ended before the window, so
	// it does not count.
	old := []RetroStageEntry{{Stage: "define", At: day(-40)}, {Stage: "launch", At: day(-30)}}
	if n := RetroStageBreachVisits(old, sla, now, windowStart); n != 0 {
		t.Fatalf("a visit that ended before the window must not count, got %d", n)
	}

	// build entered 20 days ago, still current: past its SLA of 10 inside the window.
	// define entered 25 days ago and left 20 days ago: 5 days, within its SLA.
	current := []RetroStageEntry{{Stage: "define", At: day(-25)}, {Stage: "build", At: day(-20)}}
	if n := RetroStageBreachVisits(current, sla, now, windowStart); n != 1 {
		t.Fatalf("want one breached visit, got %d", n)
	}

	// Two visits past their SLA inside the window: define ran 17 days against
	// its SLA of 7 and ended 13 days ago; build has run 13 days against its SLA of 10.
	two := []RetroStageEntry{{Stage: "define", At: day(-30)}, {Stage: "build", At: day(-13)}}
	if n := RetroStageBreachVisits(two, sla, now, windowStart); n != 2 {
		t.Fatalf("want two breached visits, got %d", n)
	}

	// A stage with no SLA never breaches.
	launch := []RetroStageEntry{{Stage: "launch", At: day(-60)}}
	if n := RetroStageBreachVisits(launch, sla, now, windowStart); n != 0 {
		t.Fatalf("a stage with no SLA never breaches, got %d", n)
	}
}

func TestRetroIssueBodyNamesTheTemplateAndTheDeliverable(t *testing.T) {
	body := RetroIssueBody(RetroCandidate{
		Trigger: RetroTriggerAgentFailures, Name: "Build Bot",
		Evidence: []string{"3 failed runs of category \"agent_error\" in the last 7 days."},
	})
	for _, want := range []string{"## What happened", "## Evidence", "## Root cause", "## What to change",
		"multica knowledge search", "--kind postmortem", "at most three", "3 failed runs"} {
		if !strings.Contains(body, want) {
			t.Errorf("retrospective body is missing %q", want)
		}
	}
	if !strings.Contains(body, "Build Bot") {
		t.Error("the body names the subject")
	}
}

func TestRetroTriggerLabelCoversEveryTrigger(t *testing.T) {
	for _, trig := range []string{RetroTriggerInitiativeClosed, RetroTriggerGateRejected, RetroTriggerAgentFailures,
		RetroTriggerSquadRejections, RetroTriggerBudgetHold, RetroTriggerStageSLA} {
		if RetroTriggerLabel(trig) == trig {
			t.Errorf("trigger %q has no words for its issue", trig)
		}
	}
}

func TestResolveDigestModeSwitchesAboveThirtySquads(t *testing.T) {
	cases := []struct {
		setting string
		squads  int
		want    string
	}{
		{DigestModeAuto, 30, DigestModeSquads},
		{DigestModeAuto, 31, DigestModeFloors},
		{"", 31, DigestModeFloors},
		{DigestModeAuto, 0, DigestModeSquads},
		{DigestModeSquads, 200, DigestModeSquads},
		{DigestModeFloors, 2, DigestModeFloors},
	}
	for _, c := range cases {
		if got := ResolveDigestMode(c.setting, c.squads); got != c.want {
			t.Errorf("ResolveDigestMode(%q, %d) = %q, want %q", c.setting, c.squads, got, c.want)
		}
	}
}

func TestFloorFilterKeepsOnlyTheFloorsSquads(t *testing.T) {
	onFloor := map[string]bool{"sq-a": true}
	pipeline := []DigestInitiative{
		{ID: "i1", OwnerSquadID: strPtr("sq-a")},
		{ID: "i2", OwnerSquadID: strPtr("sq-b")},
		{ID: "i3"},
	}
	got := filterPipelineToSquads(pipeline, onFloor)
	if len(got) != 1 || got[0].ID != "i1" {
		t.Fatalf("pipeline: want only i1, got %+v", got)
	}
	overdue := filterOverdueToSquads([]DigestOverdueRequest{{SquadID: "sq-a"}, {SquadID: "sq-b"}}, onFloor)
	if len(overdue) != 1 || overdue[0].SquadID != "sq-a" {
		t.Fatalf("overdue: want only sq-a, got %+v", overdue)
	}
}

func TestDigestFloorBriefBodyIsCut(t *testing.T) {
	long := strings.Repeat("é", DigestFloorBriefBodyMax+10)
	if got := clipDigestBody(long, DigestFloorBriefBodyMax); len([]rune(got)) != DigestFloorBriefBodyMax {
		t.Fatalf("want %d runes, got %d", DigestFloorBriefBodyMax, len([]rune(got)))
	}
	if got := clipDigestBody("short", DigestFloorBriefBodyMax); got != "short" {
		t.Fatalf("a short body is kept, got %q", got)
	}
}

func strPtr(s string) *string { return &s }
