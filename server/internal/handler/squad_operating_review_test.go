package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// cleanupSquadRoutines removes a squad's routines and the autopilots behind them
// when the test ends. The fixture cannot see these rows; routines have no foreign
// keys, and the autopilots are the ones a save or a link created.
func cleanupSquadRoutines(t *testing.T, squadID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, sql := range []string{
			`DELETE FROM autopilot_run WHERE autopilot_id IN (SELECT autopilot_id FROM squad_routine WHERE squad_id = $1)`,
			`DELETE FROM autopilot_trigger WHERE autopilot_id IN (SELECT autopilot_id FROM squad_routine WHERE squad_id = $1)`,
			`DELETE FROM autopilot_rule_version WHERE autopilot_id IN (SELECT autopilot_id FROM squad_routine WHERE squad_id = $1)`,
			`DELETE FROM autopilot WHERE id IN (SELECT autopilot_id FROM squad_routine WHERE squad_id = $1)`,
			`DELETE FROM squad_routine WHERE squad_id = $1`,
		} {
			_, _ = testPool.Exec(ctx, sql, squadID)
		}
	})
}

// operatingReviewOf returns the autopilot of the squad's operating review, or ""
// when the squad has none.
func operatingReviewOf(t *testing.T, squadID string) string {
	t.Helper()
	if dbfx.Count(t, `SELECT count(*) FROM squad_routine WHERE squad_id = $1 AND purpose = 'Operating review'`, squadID) == 0 {
		return ""
	}
	var id string
	dbfx.QueryRow(t, `SELECT autopilot_id::text FROM squad_routine WHERE squad_id = $1 AND purpose = 'Operating review'`, squadID).Scan(&id)
	return id
}

// saveCharterWithSwitch saves the charter at the revision, with the operating
// review switch set to enabled.
func saveCharterWithSwitch(t *testing.T, squadID string, revision int64, enabled bool) SquadCharterResponse {
	t.Helper()
	body := charterBody(revision, "Mission at revision")
	body["operating_review_enabled"] = enabled
	var got SquadCharterResponse
	putCharterAs(t, "", squadID, body).Want(http.StatusOK).JSON(&got)
	return got
}

// TestOperatingReviewCreatedOnceOnFirstSave: the first save creates the weekly
// review once, assigned to the squad; later saves create nothing more.
func TestOperatingReviewCreatedOnceOnFirstSave(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "review-once-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Review Once Squad", leaderID)

	if operatingReviewOf(t, squadID) != "" {
		t.Fatalf("a squad with no charter has no operating review")
	}
	putCharterAs(t, "", squadID, charterBody(0, "First")).Want(http.StatusOK)
	reviewID := operatingReviewOf(t, squadID)
	if reviewID == "" {
		t.Fatalf("first save did not create the operating review")
	}

	var assignee, title, status, cron, expected string
	var required bool
	dbfx.QueryRow(t, `SELECT a.assignee_type || ':' || a.assignee_id::text, a.title, a.status, t.cron_expression, r.expected_output, r.required
		FROM autopilot a
		JOIN autopilot_trigger t ON t.autopilot_id = a.id AND t.kind = 'schedule' AND t.enabled
		JOIN squad_routine r ON r.autopilot_id = a.id
		WHERE a.id = $1`, reviewID).Scan(&assignee, &title, &status, &cron, &expected, &required)
	if assignee != "squad:"+squadID || title != "Operating review" || status != "active" {
		t.Fatalf("review autopilot: assignee=%s title=%q status=%s", assignee, title, status)
	}
	if cron != "0 9 * * 1" || expected != "report" || !required {
		t.Fatalf("review schedule and routine: cron=%q expected=%s required=%v", cron, expected, required)
	}

	saveCharterWithSwitch(t, squadID, 1, true)
	putCharterAs(t, "", squadID, charterBody(2, "Second")).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot WHERE assignee_type = 'squad' AND assignee_id = $1 AND title = 'Operating review'`, squadID); n != 1 {
		t.Fatalf("saving again created or lost reviews: %d autopilots, want 1", n)
	}
	if n := countRoutineRows(t, `squad_id = $1 AND purpose = 'Operating review'`, squadID); n != 1 {
		t.Fatalf("expected one review routine, got %d", n)
	}
}

// TestOperatingReviewDescriptionCarriesCharterAndGoals: the description is built
// from the charter saved first, names the goals as none recorded, and lists the
// five steps.
func TestOperatingReviewDescriptionCarriesCharterAndGoals(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "review-desc-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Review Description Squad", leaderID)
	putCharterAs(t, "", squadID, charterBody(0, "Keep the research queue honest")).Want(http.StatusOK)

	var description string
	dbfx.QueryRow(t, `SELECT COALESCE(description, '') FROM autopilot WHERE id = $1`, operatingReviewOf(t, squadID)).Scan(&description)
	for _, want := range []string{
		"Keep the research queue honest",
		"Triage new inbound requests",
		"weekly-digest (report, weekly)",
		"Never spend money.",
		"Escalate to the boss on anything irreversible.",
		"Goals: none recorded yet",
		"1. Compare last week's outputs with the charter",
		"4. File it as a proposal (the attention limits apply)",
		"5. Draft updates for stale knowledge-base documents (drafts only).",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("review description lacks %q:\n%s", want, description)
		}
	}
}

// TestOperatingReviewSwitchPausesAndResumes: turning the switch off pauses the
// review, turning it on resumes it. A save that leaves the switch where it was
// does not touch the review, so a manual state survives it.
func TestOperatingReviewSwitchPausesAndResumes(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "review-switch-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Review Switch Squad", leaderID)
	first := saveCharterWithSwitch(t, squadID, 0, true)
	reviewID := operatingReviewOf(t, squadID)

	saveCharterWithSwitch(t, squadID, first.Revision, false)
	if got := autopilotStatus(t, reviewID); got != "paused" {
		t.Fatalf("switch off: review is %s, want paused", got)
	}

	// An admin resumes the review by hand; a save that keeps the switch off leaves it.
	dbfx.Exec(t, `UPDATE autopilot SET status = 'active' WHERE id = $1`, reviewID)
	saveCharterWithSwitch(t, squadID, first.Revision+1, false)
	if got := autopilotStatus(t, reviewID); got != "active" {
		t.Fatalf("a save that leaves the switch alone changed the review to %s", got)
	}

	saveCharterWithSwitch(t, squadID, first.Revision+2, true)
	if got := autopilotStatus(t, reviewID); got != "active" {
		t.Fatalf("switch on: review is %s, want active", got)
	}
	saveCharterWithSwitch(t, squadID, first.Revision+3, false)
	saveCharterWithSwitch(t, squadID, first.Revision+4, true)
	if got := autopilotStatus(t, reviewID); got != "active" {
		t.Fatalf("switch off then on: review is %s, want active", got)
	}
}

// TestOperatingReviewOffAtFirstSaveStartsPaused: a first save that turns the review
// off creates it paused, so turning it on later resumes it rather than finding
// nothing.
func TestOperatingReviewOffAtFirstSaveStartsPaused(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "review-off-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Review Off Squad", leaderID)
	saveCharterWithSwitch(t, squadID, 0, false)
	reviewID := operatingReviewOf(t, squadID)
	if reviewID == "" || autopilotStatus(t, reviewID) != "paused" {
		t.Fatalf("first save with the review off: want a paused review, got %q", reviewID)
	}
}

// TestOperatingReviewIsCoveredByCompliance: the review is a routine like any other,
// so the routines list reports its state.
func TestOperatingReviewIsCoveredByCompliance(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "review-compliance-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Review Compliance Squad", leaderID)
	saveCharterWithSwitch(t, squadID, 0, true)
	reviewID := operatingReviewOf(t, squadID)

	var routines []SquadRoutineResponse
	listRoutinesAs(t, "", squadID).Want(http.StatusOK).JSON(&routines)
	if len(routines) != 1 || routines[0].AutopilotID != reviewID {
		t.Fatalf("routines = %+v, want the review alone", routines)
	}
	if routines[0].ExpectedOutput != "report" || !routines[0].Required || routines[0].State != "never_ran" {
		t.Fatalf("review routine = %+v, want a required report routine that has never run", routines[0])
	}
}

// TestOperatingReviewPurposeIsReserved: a routine cannot take the review's purpose,
// so the purpose keeps identifying the one review.
func TestOperatingReviewPurposeIsReserved(t *testing.T) {
	squadID := newRoutineSquad(t, "Reserved Purpose Squad")
	createRoutineAs(t, "", squadID, map[string]any{
		"cadence": "daily", "purpose": "Operating review",
	}).Want(http.StatusBadRequest)
	if n := countRoutineRows(t, `squad_id = $1`, squadID); n != 0 {
		t.Fatalf("a refused reserved purpose must create nothing, found %d routines", n)
	}
}
