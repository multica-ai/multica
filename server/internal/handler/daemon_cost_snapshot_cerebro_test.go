package handler

// CEREBRO-PATCH(daemon-snapshot-saving): tests for the snapshot_prompt renderer + measurement params (FIR-2384).

import (
	"fmt"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestRenderIssueSnapshot verifies the inlined block carries the issue core and
// recent thread, excludes the trigger comment (it is inlined separately),
// skips system rows, and labels the running agent's own past comments.
func TestRenderIssueSnapshot(t *testing.T) {
	t.Parallel()

	trigger := parseUUID("11111111-1111-1111-1111-111111111111")
	selfAgent := parseUUID("22222222-2222-2222-2222-222222222222")
	otherAgent := parseUUID("33333333-3333-3333-3333-333333333333")
	c1 := parseUUID("aaaaaaaa-0000-0000-0000-000000000001")
	c3 := parseUUID("aaaaaaaa-0000-0000-0000-000000000003")
	c4 := parseUUID("aaaaaaaa-0000-0000-0000-000000000004")
	c5 := parseUUID("aaaaaaaa-0000-0000-0000-000000000005")

	issue := db.Issue{
		Number:      7,
		Title:       "Fix login",
		Status:      "in_progress",
		Priority:    "high",
		Description: pgtype.Text{String: "Login broken on Safari", Valid: true},
	}
	comments := []db.Comment{
		{ID: c1, AuthorType: "member", Content: "first user note", Type: "comment"},
		{ID: trigger, AuthorType: "member", Content: "TRIGGER must be excluded", Type: "comment"},
		{ID: c3, AuthorType: "agent", AuthorID: selfAgent, Content: "my earlier reply", Type: "comment"},
		{ID: c4, AuthorType: "system", Content: "status changed to in_progress", Type: "system"},
		{ID: c5, AuthorType: "agent", AuthorID: otherAgent, Content: "another agent note", Type: "comment"},
	}

	got := renderIssueSnapshot(issue, comments, trigger, uuidToString(selfAgent))

	for _, want := range []string{
		"Issue #7: Fix login",
		"Status: in_progress · Priority: high",
		"Login broken on Safari",
		"first user note",
		"my earlier reply",
		"another agent note",
		"You (earlier):",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot missing %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "TRIGGER must be excluded") {
		t.Fatalf("trigger comment must be excluded from the thread, got:\n%s", got)
	}
	if strings.Contains(got, "status changed to in_progress") {
		t.Fatalf("system rows must be skipped, got:\n%s", got)
	}
}

// TestRenderIssueSnapshot_Caps keeps only the most recent comments so the start
// prompt stays bounded on long threads.
func TestRenderIssueSnapshot_Caps(t *testing.T) {
	t.Parallel()

	var comments []db.Comment
	for i := 0; i < snapshotThreadCommentLimit+5; i++ {
		comments = append(comments, db.Comment{
			AuthorType: "member",
			Content:    fmt.Sprintf("c%d", i),
			Type:       "comment",
		})
	}
	got := renderIssueSnapshot(db.Issue{Number: 1, Title: "t", Status: "todo", Priority: "low"}, comments, pgtype.UUID{}, "")

	// The five oldest (c0..c4) are dropped; the newest survives.
	for _, dropped := range []string{"\nc0\n", "\nc4\n"} {
		if strings.Contains(got, dropped) {
			t.Fatalf("expected oldest comment %q to be dropped, got:\n%s", dropped, got)
		}
	}
	if !strings.Contains(got, fmt.Sprintf("c%d", snapshotThreadCommentLimit+4)) {
		t.Fatalf("expected newest comment to survive, got:\n%s", got)
	}
}

// TestSnapshotMeasurementParams: "on" records an applied saving, "shadow"
// records a would-save (not applied). Baseline is the two reads removed.
func TestSnapshotMeasurementParams(t *testing.T) {
	t.Parallel()

	// CEREBRO-PATCH(cost-optimization-token-metrics): snapshot_prompt records token deltas.
	ws := parseUUID("44444444-4444-4444-4444-444444444444")
	task := parseUUID("55555555-5555-5555-5555-555555555555")

	snapshot := "Issue #1: Test\n\nSome thread content for token estimate."
	on := snapshotMeasurementParams(ws, task, costSavingModeOn, snapshot)
	if !on.Applied || on.HeldOut {
		t.Fatalf("on mode must be applied and not held out, got applied=%v held=%v", on.Applied, on.HeldOut)
	}
	if on.SavingKey != costSavingSnapshotKey || on.Metric != costMetricInputTokens {
		t.Fatalf("unexpected key/metric: %+v", on)
	}
	if on.BaselineValue <= 0 || on.EffectiveValue != 0 {
		t.Fatalf("expected positive token baseline and effective=0, got baseline=%d effective=%d", on.BaselineValue, on.EffectiveValue)
	}

	shadow := snapshotMeasurementParams(ws, task, costSavingModeShadow, snapshot)
	if shadow.Applied {
		t.Fatalf("shadow mode must not be applied, got %+v", shadow)
	}
	if shadow.BaselineValue != on.BaselineValue {
		t.Fatalf("shadow would-save baseline must match snapshot tokens, got %d vs %d", shadow.BaselineValue, on.BaselineValue)
	}
}
