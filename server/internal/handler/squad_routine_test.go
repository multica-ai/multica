package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func listRoutinesAs(t *testing.T, userID, squadID string) *testutil.Response {
	t.Helper()
	req := squadScopeReq(userID, "GET", "/api/squads/"+squadID+"/routines", nil, map[string]string{"id": squadID})
	return testutil.Call(t, testHandler.ListSquadRoutines, req)
}

func createRoutineAs(t *testing.T, userID, squadID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := squadScopeReq(userID, "POST", "/api/squads/"+squadID+"/routines", body, map[string]string{"id": squadID})
	return testutil.Call(t, testHandler.CreateSquadRoutine, req)
}

func unlinkRoutineAs(t *testing.T, userID, squadID, routineID, query string) *testutil.Response {
	t.Helper()
	path := "/api/squads/" + squadID + "/routines/" + routineID
	if query != "" {
		path += "?" + query
	}
	req := squadScopeReq(userID, "DELETE", path, nil, map[string]string{"id": squadID, "routine_id": routineID})
	return testutil.Call(t, testHandler.DeleteSquadRoutine, req)
}

// newRoutineSquad creates a squad owned by the workspace owner, with a leader that
// has a runtime, so autopilots can be assigned to it.
func newRoutineSquad(t *testing.T, name string) string {
	t.Helper()
	leaderID := createHandlerTestAgent(t, name+" leader "+uniqueSuffix(), nil)
	squadID := dbfx.Squad(t, name, leaderID)
	dbfx.Cleanup(t, `DELETE FROM squad_routine WHERE squad_id = $1`, squadID)
	return squadID
}

// insertSquadAutopilot makes an autopilot assigned to the squad through the
// fixture, so the test controls its status and creator.
func insertSquadAutopilot(t *testing.T, squadID, status string) string {
	t.Helper()
	id := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    dbfx.WorkspaceID,
		"title":           "routine autopilot " + status,
		"assignee_type":   "squad",
		"assignee_id":     squadID,
		"status":          status,
		"execution_mode":  "create_issue",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	return id
}

// cleanupRoutineAutopilot removes an autopilot a test created through the API,
// with the rows that reference it. The fixture cannot see those rows.
func cleanupRoutineAutopilot(t *testing.T, autopilotID string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM squad_routine WHERE autopilot_id = $1`, autopilotID)
	dbfx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, autopilotID)
	dbfx.Cleanup(t, `DELETE FROM autopilot_trigger WHERE autopilot_id = $1`, autopilotID)
	dbfx.Cleanup(t, `DELETE FROM autopilot_rule_version WHERE autopilot_id = $1`, autopilotID)
	dbfx.Cleanup(t, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
}

func autopilotStatus(t *testing.T, autopilotID string) string {
	t.Helper()
	var status string
	dbfx.QueryRow(t, `SELECT status FROM autopilot WHERE id = $1`, autopilotID).Scan(&status)
	return status
}

func countRoutineRows(t *testing.T, where string, args ...any) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM squad_routine WHERE `+where, args...)
}

// TestSquadRoutinePresetsScheduleTheAutopilot: each cadence preset, and a cron
// string, becomes a schedule on an active autopilot assigned to the squad.
func TestSquadRoutinePresetsScheduleTheAutopilot(t *testing.T) {
	squadID := newRoutineSquad(t, "Preset Squad")
	cases := []struct {
		cadence string
		cron    string
	}{
		{"daily", "0 9 * * *"},
		{"weekdays", "0 9 * * 1-5"},
		{"weekly", "0 9 * * 1"},
		{"biweekly", "0 9 1,15 * *"},
		{"monthly", "0 9 1 * *"},
		{"15 10 * * 2", "15 10 * * 2"},
	}
	for _, tc := range cases {
		t.Run(tc.cadence, func(t *testing.T) {
			var got SquadRoutineResponse
			createRoutineAs(t, "", squadID, map[string]any{
				"cadence":     tc.cadence,
				"timezone":    "Europe/Paris",
				"purpose":     "Run " + tc.cadence,
				"description": "Do the recurring work.",
			}).Want(http.StatusCreated).JSON(&got)
			cleanupRoutineAutopilot(t, got.AutopilotID)

			if got.Schedule == nil || got.Schedule.CronExpression != tc.cron || got.Schedule.Timezone != "Europe/Paris" {
				t.Fatalf("expected schedule %q in Europe/Paris, got %+v", tc.cron, got.Schedule)
			}
			if got.NextRunAt == nil || got.LastRun != nil {
				t.Fatalf("a new routine has a next run and no last run, got next=%v last=%+v", got.NextRunAt, got.LastRun)
			}
			if got.AutopilotStatus != "active" || got.Purpose != "Run "+tc.cadence {
				t.Fatalf("expected an active routine with its purpose, got %+v", got)
			}

			var assignee, mode, description string
			dbfx.QueryRow(t, `SELECT assignee_type || ':' || assignee_id::text, execution_mode, COALESCE(description, '') FROM autopilot WHERE id = $1`,
				got.AutopilotID).Scan(&assignee, &mode, &description)
			if assignee != "squad:"+squadID || mode != "create_issue" || description != "Do the recurring work." {
				t.Fatalf("autopilot not assigned to the squad as expected: assignee=%s mode=%s description=%q", assignee, mode, description)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM autopilot_trigger WHERE autopilot_id = $1 AND kind = 'schedule' AND enabled`, got.AutopilotID); n != 1 {
				t.Fatalf("expected one enabled schedule trigger, got %d", n)
			}
		})
	}
}

// TestSquadRoutineRejectsBadInput: malformed requests are refused before any
// autopilot or link is written.
func TestSquadRoutineRejectsBadInput(t *testing.T) {
	squadID := newRoutineSquad(t, "Routine Input Squad")
	cases := []struct {
		name string
		body map[string]any
	}{
		{"no purpose", map[string]any{"cadence": "daily"}},
		{"neither autopilot nor cadence", map[string]any{"purpose": "x"}},
		{"both autopilot and cadence", map[string]any{"purpose": "x", "cadence": "daily", "autopilot_id": uuid.NewString()}},
		{"unparseable cron", map[string]any{"purpose": "x", "cadence": "every tuesday"}},
		{"unknown timezone", map[string]any{"purpose": "x", "cadence": "daily", "timezone": "Mars/Olympus"}},
		{"unknown expected output", map[string]any{"purpose": "x", "cadence": "daily", "expected_output": "email"}},
		{"grace hours out of range", map[string]any{"purpose": "x", "cadence": "daily", "grace_hours": 0}},
		{"malformed autopilot id", map[string]any{"purpose": "x", "autopilot_id": "not-a-uuid"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			createRoutineAs(t, "", squadID, tc.body).Want(http.StatusBadRequest)
		})
	}
	if n := countRoutineRows(t, `squad_id = $1`, squadID); n != 0 {
		t.Fatalf("rejected requests must not create routines, found %d", n)
	}
}

// TestSquadRoutineLinksExistingAutopilot: an autopilot can be linked when it is
// assigned to this squad and not archived, and only once.
func TestSquadRoutineLinksExistingAutopilot(t *testing.T) {
	squadID := newRoutineSquad(t, "Link Squad")
	otherSquadID := newRoutineSquad(t, "Other Link Squad")

	linkable := insertSquadAutopilot(t, squadID, "active")
	cleanupRoutineAutopilot(t, linkable)
	foreign := insertSquadAutopilot(t, otherSquadID, "active")
	cleanupRoutineAutopilot(t, foreign)
	archived := insertSquadAutopilot(t, squadID, "archived")
	cleanupRoutineAutopilot(t, archived)

	createRoutineAs(t, "", squadID, map[string]any{"autopilot_id": foreign, "purpose": "Borrowed"}).Want(http.StatusBadRequest)
	createRoutineAs(t, "", squadID, map[string]any{"autopilot_id": archived, "purpose": "Archived"}).Want(http.StatusBadRequest)

	var got SquadRoutineResponse
	createRoutineAs(t, "", squadID, map[string]any{
		"autopilot_id":    linkable,
		"purpose":         "Weekly scan",
		"expected_output": "proposal",
		"grace_hours":     48,
		"required":        false,
	}).Want(http.StatusCreated).JSON(&got)
	if got.AutopilotID != linkable || got.ExpectedOutput != "proposal" || got.GraceHours != 48 || got.Required {
		t.Fatalf("linked routine did not keep its settings: %+v", got)
	}

	conflict := createRoutineAs(t, "", squadID, map[string]any{"autopilot_id": linkable, "purpose": "Again"}).
		Want(http.StatusConflict).Map()
	if conflict["code"] != "routine_already_linked" {
		t.Fatalf("expected routine_already_linked, got %v", conflict)
	}
}

// TestSquadRoutineListReportsScheduleAndLastRun: the list carries the schedule,
// the next run, and the most recent run once one exists.
func TestSquadRoutineListReportsScheduleAndLastRun(t *testing.T) {
	squadID := newRoutineSquad(t, "List Squad")
	var created SquadRoutineResponse
	createRoutineAs(t, "", squadID, map[string]any{
		"cadence": "weekly", "timezone": "UTC", "purpose": "Monday review",
	}).Want(http.StatusCreated).JSON(&created)
	cleanupRoutineAutopilot(t, created.AutopilotID)

	var before []SquadRoutineResponse
	listRoutinesAs(t, "", squadID).Want(http.StatusOK).JSON(&before)
	if len(before) != 1 {
		t.Fatalf("expected one routine, got %d", len(before))
	}
	if before[0].Schedule == nil || before[0].Schedule.CronExpression != "0 9 * * 1" || before[0].NextRunAt == nil {
		t.Fatalf("expected schedule and next run, got %+v", before[0])
	}
	if before[0].LastRun != nil {
		t.Fatalf("no run yet, expected last_run null, got %+v", before[0].LastRun)
	}

	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": created.AutopilotID,
		"source":       "schedule",
		"status":       "completed",
		"completed_at": testutil.Raw("now()"),
	})
	var after []SquadRoutineResponse
	listRoutinesAs(t, "", squadID).Want(http.StatusOK).JSON(&after)
	last := after[0].LastRun
	if last == nil || last.ID != runID || last.Status != "completed" || last.Source != "schedule" || last.CompletedAt == nil {
		t.Fatalf("expected the completed run as last_run, got %+v", last)
	}
}

// TestSquadRoutineUnlinkKeepsOrPausesAutopilot: an unlink leaves the autopilot
// active; ?pause=true also pauses it.
func TestSquadRoutineUnlinkKeepsOrPausesAutopilot(t *testing.T) {
	squadID := newRoutineSquad(t, "Unlink Squad")
	keep := insertSquadAutopilot(t, squadID, "active")
	cleanupRoutineAutopilot(t, keep)
	pause := insertSquadAutopilot(t, squadID, "active")
	cleanupRoutineAutopilot(t, pause)
	createRoutineAs(t, "", squadID, map[string]any{"autopilot_id": keep, "purpose": "Keep running"}).Want(http.StatusCreated)
	createRoutineAs(t, "", squadID, map[string]any{"autopilot_id": pause, "purpose": "Stop running"}).Want(http.StatusCreated)

	unlinkRoutineAs(t, "", squadID, keep, "").Want(http.StatusNoContent)
	if got := autopilotStatus(t, keep); got != "active" {
		t.Fatalf("unlink without pause must leave the autopilot active, got %q", got)
	}
	if n := countRoutineRows(t, `autopilot_id = $1`, keep); n != 0 {
		t.Fatalf("expected the link removed, found %d", n)
	}

	unlinkRoutineAs(t, "", squadID, pause, "pause=maybe").Want(http.StatusBadRequest)
	unlinkRoutineAs(t, "", squadID, pause, "pause=true").Want(http.StatusNoContent)
	if got := autopilotStatus(t, pause); got != "paused" {
		t.Fatalf("unlink with pause=true must pause the autopilot, got %q", got)
	}

	unlinkRoutineAs(t, "", squadID, keep, "").Want(http.StatusNotFound)
}

// TestSquadRoutinePermissions: the squad's creator and an owner or admin manage
// routines; another member reads them; an outsider sees nothing. An autopilot
// owned by someone else cannot be linked or paused by the creator.
func TestSquadRoutinePermissions(t *testing.T) {
	suffix := uniqueSuffix()
	creatorID := createPlainMember(t, "routine-creator-"+suffix+"@multica.test")
	otherID := createPlainMember(t, "routine-other-"+suffix+"@multica.test")
	outsiderID := dbfx.User(t, "Routine Outsider", "routine-outsider-"+suffix+"@multica.test")
	leaderID := createHandlerTestAgent(t, "routine-perm-leader-"+suffix, nil)
	squad := createSquadAs(t, creatorID, "Routine Permission Squad", leaderID)
	dbfx.Cleanup(t, `DELETE FROM squad_routine WHERE squad_id = $1`, squad.ID)

	listRoutinesAs(t, otherID, squad.ID).Want(http.StatusOK)
	createRoutineAs(t, otherID, squad.ID, map[string]any{"cadence": "daily", "purpose": "Not mine"}).Want(http.StatusForbidden)

	var own SquadRoutineResponse
	createRoutineAs(t, creatorID, squad.ID, map[string]any{"cadence": "daily", "purpose": "Creator's routine"}).
		Want(http.StatusCreated).JSON(&own)
	cleanupRoutineAutopilot(t, own.AutopilotID)
	unlinkRoutineAs(t, otherID, squad.ID, own.AutopilotID, "").Want(http.StatusForbidden)

	ownerAutopilot := insertSquadAutopilot(t, squad.ID, "active")
	cleanupRoutineAutopilot(t, ownerAutopilot)
	createRoutineAs(t, "", squad.ID, map[string]any{"autopilot_id": ownerAutopilot, "purpose": "Owner's autopilot"}).
		Want(http.StatusCreated)
	createRoutineAs(t, creatorID, squad.ID, map[string]any{"autopilot_id": ownerAutopilot, "purpose": "Borrowed"}).
		Want(http.StatusForbidden)
	unlinkRoutineAs(t, creatorID, squad.ID, ownerAutopilot, "pause=true").Want(http.StatusForbidden)
	if got := autopilotStatus(t, ownerAutopilot); got != "active" {
		t.Fatalf("a refused pause must leave the autopilot active, got %q", got)
	}
	unlinkRoutineAs(t, creatorID, squad.ID, ownerAutopilot, "").Want(http.StatusNoContent)

	listRoutinesAs(t, outsiderID, squad.ID).Want(http.StatusNotFound)
	createRoutineAs(t, outsiderID, squad.ID, map[string]any{"cadence": "daily", "purpose": "Outsider"}).Want(http.StatusNotFound)
}

// TestSquadRoutinesIsolatedFromOtherWorkspaces: another workspace's squad and
// routines are not found through this workspace.
func TestSquadRoutinesIsolatedFromOtherWorkspaces(t *testing.T) {
	suffix := uniqueSuffix()
	ws := dbfx.Workspace(t, "Routine Other "+suffix, "routine-other-ws-"+suffix)
	other := testutil.New(testPool, ws, testUserID)
	leaderID := createHandlerTestAgent(t, "routine-iso-leader-"+suffix, nil)
	foreign := other.Squad(t, "Foreign Routine Squad", leaderID)
	routineAutopilot := uuid.NewString()
	other.InsertNoID(t, "squad_routine", testutil.Cols{
		"autopilot_id": routineAutopilot,
		"workspace_id": ws,
		"squad_id":     foreign,
		"purpose":      "Foreign routine",
	}, "autopilot_id = $1", routineAutopilot)

	listRoutinesAs(t, "", foreign).Want(http.StatusNotFound)
	createRoutineAs(t, "", foreign, map[string]any{"cadence": "daily", "purpose": "Hijack"}).Want(http.StatusNotFound)
	unlinkRoutineAs(t, "", foreign, routineAutopilot, "").Want(http.StatusNotFound)

	if n := countRoutineRows(t, `autopilot_id = $1 AND workspace_id = $2`, routineAutopilot, ws); n != 1 {
		t.Fatalf("a cross-workspace unlink must not remove the routine, found %d", n)
	}
}

// TestDeletingAutopilotUnlinksItsRoutine: archiving an autopilot removes its
// routine link in the same request, so no routine points at an archived autopilot.
func TestDeletingAutopilotUnlinksItsRoutine(t *testing.T) {
	squadID := newRoutineSquad(t, "Delete Routine Squad")
	apID := insertSquadAutopilot(t, squadID, "active")
	cleanupRoutineAutopilot(t, apID)
	createRoutineAs(t, "", squadID, map[string]any{"autopilot_id": apID, "purpose": "Soon gone"}).Want(http.StatusCreated)

	req := newRequest("DELETE", "/api/autopilots/"+apID, nil)
	req = testutil.WithURLParams(req, "id", apID)
	testutil.Call(t, testHandler.DeleteAutopilot, req).Want(http.StatusNoContent)

	if n := countRoutineRows(t, `autopilot_id = $1`, apID); n != 0 {
		t.Fatalf("deleting the autopilot must unlink its routine, found %d", n)
	}
	var routines []SquadRoutineResponse
	listRoutinesAs(t, "", squadID).Want(http.StatusOK).JSON(&routines)
	if len(routines) != 0 {
		t.Fatalf("expected no routines after the autopilot was deleted, got %+v", routines)
	}
}
