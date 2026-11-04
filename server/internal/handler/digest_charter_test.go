package handler

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// digestCharter gives a squad a charter last saved the given interval ago, for
// example "2 days". The charter table has no id column, so the fixture cannot
// insert it; the test removes it itself.
func (e digestEnv) digestCharter(t *testing.T, squadID, savedAgo string) {
	t.Helper()
	dbfx.Exec(t, `INSERT INTO squad_charter (squad_id, workspace_id, mission, updated_at)
		VALUES ($1, $2, 'Keep research moving', now() - $3::interval)`, squadID, e.WorkspaceID, savedAgo)
	dbfx.Cleanup(t, `DELETE FROM squad_charter WHERE squad_id = $1`, squadID)
}

// digestRoutine links a routine to the squad. paused sets the autopilot's status,
// and runStatus, when set, records one run triggered an hour ago.
func (e digestEnv) digestRoutine(t *testing.T, squadID, purpose string, required, paused bool, runStatus string) string {
	t.Helper()
	status := "active"
	if paused {
		status = "paused"
	}
	apID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    e.WorkspaceID,
		"title":           purpose,
		"assignee_type":   "squad",
		"assignee_id":     squadID,
		"status":          status,
		"execution_mode":  "create_issue",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	dbfx.Exec(t, `INSERT INTO squad_routine (autopilot_id, workspace_id, squad_id, purpose, expected_output, required, grace_hours)
		VALUES ($1, $2, $3, $4, 'none', $5, 24)`, apID, e.WorkspaceID, squadID, purpose, required)
	dbfx.Cleanup(t, `DELETE FROM squad_routine WHERE autopilot_id = $1`, apID)
	if runStatus != "" {
		dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": apID,
			"squad_id":     squadID,
			"source":       "schedule",
			"status":       runStatus,
			"triggered_at": testutil.Raw("now() - interval '1 hour'"),
			"created_at":   testutil.Raw("now() - interval '1 hour'"),
		})
	}
	return apID
}

// digestPendingProposalAt keeps a squad in the digest's full list with a pending
// proposal filed at the given age, without it counting as recent activity.
func (e digestEnv) digestPendingProposalAt(t *testing.T, squadID, ago string) {
	t.Helper()
	issueID := dbfx.Issue(t, "old proposal", testutil.Cols{"workspace_id": e.WorkspaceID})
	dbfx.Insert(t, "proposal", testutil.Cols{
		"workspace_id":    e.WorkspaceID,
		"squad_id":        squadID,
		"issue_id":        issueID,
		"created_by_type": "agent",
		"created_by_id":   e.AgentID,
		"created_at":      testutil.Raw(fmt.Sprintf("now() - interval '%s'", ago)),
	})
}

// digestRaw reads the digest as a generic JSON object, so a test can assert the
// exact keys the TypeScript schema depends on.
func (e digestEnv) digestRaw(t *testing.T) map[string]any {
	t.Helper()
	req := newRequest("GET", "/api/office/digest", nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	return testutil.Call(t, testHandler.GetOfficeDigest, req).Want(http.StatusOK).Map()
}

func digestJSONKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func assertDigestKeys(t *testing.T, where string, got map[string]any, want ...string) {
	t.Helper()
	sort.Strings(want)
	if fmt.Sprint(digestJSONKeys(got)) != fmt.Sprint(want) {
		t.Fatalf("%s keys = %v, want %v", where, digestJSONKeys(got), want)
	}
}

func digestObjectAt(t *testing.T, where string, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want an object", where, v)
	}
	return m
}

func digestArrayAt(t *testing.T, where string, v any) []any {
	t.Helper()
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("%s is %T, want an array", where, v)
	}
	return a
}

// TestDigest_CharterSectionAndWorkspaceCounts pins the JSON the digest's charter
// section and workspace routine list carry, and the counts behind them. Only
// required routines that are not on time are listed.
//
// Keys, as the TypeScript digest schema reads them:
//
//	squads[].charter               null, or {silent, routine_count, routines}
//	squads[].charter.routines[]    {autopilot_id, title, purpose, expected_output, required, state}
//	workspace.routines_not_on_time {count, items}
//	...routines_not_on_time.items[] {autopilot_id, squad_id, squad_name, title, state}
func TestDigest_CharterSectionAndWorkspaceCounts(t *testing.T) {
	e := newDigestEnv(t)

	// Squad A: charter saved two days ago. One routine on time, one paused and
	// required, one failed and required, one paused but optional.
	a := e.squad(t, "Alpha Research")
	e.digestCharter(t, a, "2 days")
	e.digestRoutine(t, a, "Done routine", true, false, "completed")
	paused := e.digestRoutine(t, a, "Paused routine", true, true, "")
	failed := e.digestRoutine(t, a, "Failed routine", true, false, "failed")
	e.digestRoutine(t, a, "Optional paused", false, true, "")

	// Squad C has no charter, but its required routine still counts.
	c := e.squad(t, "Gamma Ops")
	noCharter := e.digestRoutine(t, c, "Uncharted failure", true, false, "failed")

	// Squad Z is outside the workspace; its failure must not reach this digest.
	other := newDigestEnv(t)
	z := other.squad(t, "Other Company")
	other.digestRoutine(t, z, "Foreign failure", true, false, "failed")

	d := e.digestRaw(t)
	// The phase 4 keys (goals, people, auto_approved, budget, mode, floors, scope,
	// system_risks) are spec-intended; see digest.go's Digest struct.
	assertDigestKeys(t, "digest", d,
		"since", "until", "workspace", "squads", "omitted_squad_count", "quiet", "knowledge",
		"inquiries", "pipeline", "goals", "overdue_requests", "people", "auto_approved", "budget",
		"mode", "scope", "floors", "system_risks")

	ws := digestObjectAt(t, "workspace", d["workspace"])
	off := digestObjectAt(t, "workspace.routines_not_on_time", ws["routines_not_on_time"])
	assertDigestKeys(t, "routines_not_on_time", off, "count", "items")
	if off["count"] != float64(3) {
		t.Fatalf("routines_not_on_time.count = %v, want 3 (paused, failed, uncharted failure)", off["count"])
	}
	items := digestArrayAt(t, "routines_not_on_time.items", off["items"])
	assertDigestKeys(t, "routines_not_on_time.items[0]", digestObjectAt(t, "items[0]", items[0]),
		"autopilot_id", "squad_id", "squad_name", "title", "state")
	// Ordered by squad name, then title.
	wantOrder := []struct{ squad, title, state string }{
		{"Alpha Research", "Failed routine", "failed"},
		{"Alpha Research", "Paused routine", "paused"},
		{"Gamma Ops", "Uncharted failure", "failed"},
	}
	for i, want := range wantOrder {
		item := digestObjectAt(t, fmt.Sprintf("items[%d]", i), items[i])
		if item["squad_name"] != want.squad || item["title"] != want.title || item["state"] != want.state {
			t.Fatalf("items[%d] = %v, want %+v", i, item, want)
		}
	}
	if items[0].(map[string]any)["autopilot_id"] != failed || items[1].(map[string]any)["autopilot_id"] != paused ||
		items[2].(map[string]any)["autopilot_id"] != noCharter {
		t.Fatalf("items do not carry the routines' autopilot ids: %v", items)
	}

	squadA := digestSquadJSON(t, d, a)
	charterA := digestObjectAt(t, "squad A charter", squadA["charter"])
	assertDigestKeys(t, "charter", charterA, "silent", "routine_count", "routines")
	if charterA["silent"] != false || charterA["routine_count"] != float64(4) {
		t.Fatalf("squad A charter = %v, want not silent with 4 routines", charterA)
	}
	routines := digestArrayAt(t, "charter.routines", charterA["routines"])
	assertDigestKeys(t, "charter.routines[0]", digestObjectAt(t, "routines[0]", routines[0]),
		"autopilot_id", "title", "purpose", "expected_output", "required", "state")

	if squadC := digestSquadJSON(t, d, c); squadC["charter"] != nil {
		t.Fatalf("a squad without a charter reports charter null, got %v", squadC["charter"])
	}
}

// digestSquadJSON finds a squad in the raw digest's full list.
func digestSquadJSON(t *testing.T, d map[string]any, squadID string) map[string]any {
	t.Helper()
	for _, s := range digestArrayAt(t, "squads", d["squads"]) {
		m := digestObjectAt(t, "squad", s)
		if m["id"] == squadID {
			return m
		}
	}
	t.Fatalf("squad %s not reported in full", squadID)
	return nil
}

// TestDigest_SilentAfterSevenDaysWithoutActivity: a chartered squad is silent when
// nothing has happened for more than seven days. A charter saved today is not
// silent, and a routine run counts as activity.
func TestDigest_SilentAfterSevenDaysWithoutActivity(t *testing.T) {
	e := newDigestEnv(t)

	// Charter saved eight days ago, and nothing since.
	silent := e.squad(t, "Quiet Squad")
	e.digestCharter(t, silent, "8 days")
	e.digestPendingProposalAt(t, silent, "10 days")

	// Charter saved eight days ago, but a routine ran a day ago.
	recent := e.squad(t, "Busy Squad")
	e.digestCharter(t, recent, "8 days")
	e.digestRoutine(t, recent, "Daily sweep", false, false, "completed")
	e.digestPendingProposalAt(t, recent, "10 days")

	// Charter saved just now.
	fresh := e.squad(t, "New Squad")
	e.digestCharter(t, fresh, "0 seconds")
	e.digestPendingProposalAt(t, fresh, "10 days")

	d := e.digest(t, timeAgoHours(24))
	if !findDigestSquad(t, d, silent).Charter.Silent {
		t.Fatalf("squad with a week of silence: charter.silent = false, want true")
	}
	if findDigestSquad(t, d, recent).Charter.Silent {
		t.Fatalf("squad whose routine ran a day ago: charter.silent = true, want false")
	}
	if findDigestSquad(t, d, fresh).Charter.Silent {
		t.Fatalf("squad chartered just now: charter.silent = true, want false")
	}
}

// TestDigest_CharterSectionIsolatedPerWorkspace: a workspace's routine states and
// counts never include another workspace's routines.
func TestDigest_CharterSectionIsolatedPerWorkspace(t *testing.T) {
	mine := newDigestEnv(t)
	squad := mine.squad(t, "Mine")
	mine.digestCharter(t, squad, "1 day")
	mine.digestPendingProposalAt(t, squad, "10 days")

	theirs := newDigestEnv(t)
	theirSquad := theirs.squad(t, "Theirs")
	theirs.digestRoutine(t, theirSquad, "Their failure", true, false, "failed")

	d := mine.digestRaw(t)
	off := digestObjectAt(t, "routines_not_on_time", digestObjectAt(t, "workspace", d["workspace"])["routines_not_on_time"])
	if off["count"] != float64(0) || len(digestArrayAt(t, "items", off["items"])) != 0 {
		t.Fatalf("another workspace's failure reached this digest: %v", off)
	}
	if got := digestSquadJSON(t, d, squad)["charter"]; got == nil {
		t.Fatalf("own charter section missing")
	}
}

// timeAgoHours is the instant h hours ago, for the digest's since parameter.
func timeAgoHours(h int) time.Time {
	return time.Now().Add(-time.Duration(h) * time.Hour)
}
