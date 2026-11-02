package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// charterBody is a complete, valid charter save at the given revision.
func charterBody(revision int64, mission string) map[string]any {
	return map[string]any{
		"revision": revision,
		"mission":  mission,
		"responsibilities": []map[string]any{
			{"key": "triage", "text": "Triage new inbound requests"},
		},
		"outputs": []map[string]any{
			{"key": "weekly-digest", "kind": "report", "kb_key": "ops/weekly", "cadence_hint": "weekly"},
		},
		"boundaries":     "Never spend money.",
		"escalation":     "Escalate to the boss on anything irreversible.",
		"intake":         "Accept questions about onboarding.",
		"response_hours": 24,
	}
}

func getCharterAs(t *testing.T, userID, squadID string) *testutil.Response {
	t.Helper()
	req := squadScopeReq(userID, "GET", "/api/squads/"+squadID+"/charter", nil, map[string]string{"id": squadID})
	return testutil.Call(t, testHandler.GetSquadCharter, req)
}

func putCharterAs(t *testing.T, userID, squadID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := squadScopeReq(userID, "PUT", "/api/squads/"+squadID+"/charter", body, map[string]string{"id": squadID})
	return testutil.Call(t, testHandler.UpdateSquadCharter, req)
}

func uniqueSuffix() string { return uuid.NewString()[:8] }

// cleanupSquadCharter removes a charter a test saved. The fixture removes the
// squad row, but squad_charter has no foreign key to cascade from.
func cleanupSquadCharter(t *testing.T, squadID string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM squad_charter WHERE squad_id = $1`, squadID)
}

// newCharterSquad creates an unseated squad owned by the workspace owner.
func newCharterSquad(t *testing.T, name, leaderID string) string {
	t.Helper()
	squadID := dbfx.Squad(t, name, leaderID)
	cleanupSquadCharter(t, squadID)
	return squadID
}

// TestSquadCharterDefaultsBeforeFirstSave: a squad with no charter reads as the
// empty charter at revision 0, with empty lists rather than nulls.
func TestSquadCharterDefaultsBeforeFirstSave(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "charter-default-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Charter Default Squad", leaderID)

	resp := getCharterAs(t, "", squadID).Want(http.StatusOK)
	var got SquadCharterResponse
	resp.JSON(&got)
	if got.Revision != 0 || got.Mission != "" || got.SquadID != squadID {
		t.Fatalf("expected empty charter at revision 0 for %s, got %+v", squadID, got)
	}
	if got.ResponseHours != 48 || !got.OperatingReviewEnabled || got.UpdatedAt != nil {
		t.Fatalf("expected default response_hours 48, operating review on, no updated_at; got %+v", got)
	}
	for _, want := range []string{`"responsibilities":[]`, `"outputs":[]`} {
		if !strings.Contains(resp.Text(), want) {
			t.Fatalf("expected %s in empty charter, got %s", want, resp.Text())
		}
	}
}

// TestSquadCharterPermissions: members read; only the creator, and owners or
// admins, save; a user outside the workspace sees nothing at all.
func TestSquadCharterPermissions(t *testing.T) {
	suffix := uniqueSuffix()
	creatorID := createPlainMember(t, "charter-creator-"+suffix+"@multica.test")
	otherID := createPlainMember(t, "charter-other-"+suffix+"@multica.test")
	outsiderID := dbfx.User(t, "Charter Outsider", "charter-outsider-"+suffix+"@multica.test")
	leaderID := createHandlerTestAgent(t, "charter-perm-leader-"+suffix, nil)
	squad := createSquadAs(t, creatorID, "Charter Permission Squad", leaderID)
	cleanupSquadCharter(t, squad.ID)

	getCharterAs(t, otherID, squad.ID).Want(http.StatusOK)
	putCharterAs(t, otherID, squad.ID, charterBody(0, "Not mine")).Want(http.StatusForbidden)

	putCharterAs(t, creatorID, squad.ID, charterBody(0, "Mine")).Want(http.StatusOK)
	putCharterAs(t, "", squad.ID, charterBody(1, "Owner edit")).Want(http.StatusOK)

	getCharterAs(t, outsiderID, squad.ID).Want(http.StatusNotFound)
	putCharterAs(t, outsiderID, squad.ID, charterBody(2, "Outsider")).Want(http.StatusNotFound)
}

// TestSquadCharterRevisionConflict: each save increments the revision, a save at
// a stale revision is refused with the current one and changes nothing.
func TestSquadCharterRevisionConflict(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "charter-rev-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Charter Revision Squad", leaderID)

	var first SquadCharterResponse
	putCharterAs(t, "", squadID, charterBody(0, "v1")).Want(http.StatusOK).JSON(&first)
	if first.Revision != 1 {
		t.Fatalf("first save: expected revision 1, got %d", first.Revision)
	}

	var second SquadCharterResponse
	putCharterAs(t, "", squadID, charterBody(1, "v2")).Want(http.StatusOK).JSON(&second)
	if second.Revision != 2 || second.Mission != "v2" {
		t.Fatalf("second save: expected revision 2 with mission v2, got %+v", second)
	}

	conflict := putCharterAs(t, "", squadID, charterBody(1, "stale")).Want(http.StatusConflict).Map()
	if conflict["code"] != "charter_revision_conflict" || conflict["revision"] != float64(2) {
		t.Fatalf("stale save: expected code charter_revision_conflict at revision 2, got %v", conflict)
	}

	// A save at revision 0 is a first save; the squad already has a charter.
	putCharterAs(t, "", squadID, charterBody(0, "again")).Want(http.StatusConflict)

	var stored SquadCharterResponse
	getCharterAs(t, "", squadID).Want(http.StatusOK).JSON(&stored)
	if stored.Revision != 2 || stored.Mission != "v2" {
		t.Fatalf("refused saves must not change the charter, got %+v", stored)
	}
}

// TestSquadCharterFirstSaveAtNonzeroRevisionConflicts: a client that thinks a
// charter exists (revision 3) against a squad with none gets 409 at revision 0.
func TestSquadCharterFirstSaveAtNonzeroRevisionConflicts(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "charter-nonzero-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Charter Nonzero Squad", leaderID)

	conflict := putCharterAs(t, "", squadID, charterBody(3, "stale")).Want(http.StatusConflict).Map()
	if conflict["revision"] != float64(0) {
		t.Fatalf("expected current revision 0, got %v", conflict)
	}
}

// TestSquadCharterRoundTrip: what a save writes is what a read returns, and the
// save records who made it.
func TestSquadCharterRoundTrip(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "charter-roundtrip-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Charter Round Trip Squad", leaderID)

	putCharterAs(t, "", squadID, charterBody(0, "Keep the backlog healthy")).Want(http.StatusOK)

	var got SquadCharterResponse
	getCharterAs(t, "", squadID).Want(http.StatusOK).JSON(&got)
	if got.Mission != "Keep the backlog healthy" || got.Boundaries != "Never spend money." ||
		got.Escalation != "Escalate to the boss on anything irreversible." || got.Intake != "Accept questions about onboarding." {
		t.Fatalf("text fields did not round-trip: %+v", got)
	}
	if got.ResponseHours != 24 || got.Revision != 1 || !got.OperatingReviewEnabled {
		t.Fatalf("expected response_hours 24, revision 1, review on; got %+v", got)
	}
	if len(got.Responsibilities) != 1 || got.Responsibilities[0].Key != "triage" || got.Responsibilities[0].Text != "Triage new inbound requests" {
		t.Fatalf("responsibilities did not round-trip: %+v", got.Responsibilities)
	}
	if len(got.Outputs) != 1 {
		t.Fatalf("expected one output, got %+v", got.Outputs)
	}
	out := got.Outputs[0]
	if out.Key != "weekly-digest" || out.Kind != "report" || out.KBKey != "ops/weekly" || out.CadenceHint != "weekly" {
		t.Fatalf("output did not round-trip: %+v", out)
	}
	if got.UpdatedByType != "member" || got.UpdatedByID == nil || *got.UpdatedByID != testUserID || got.UpdatedAt == nil {
		t.Fatalf("expected the saving member recorded, got by=%q id=%v at=%v", got.UpdatedByType, got.UpdatedByID, got.UpdatedAt)
	}
}

// TestSquadCharterOperatingReviewFlag: the flag is set on the first save, kept
// when a later save omits it, and changed only when a save names it.
func TestSquadCharterOperatingReviewFlag(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "charter-review-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Charter Review Flag Squad", leaderID)

	off := charterBody(0, "review off")
	off["operating_review_enabled"] = false
	var saved SquadCharterResponse
	putCharterAs(t, "", squadID, off).Want(http.StatusOK).JSON(&saved)
	if saved.OperatingReviewEnabled {
		t.Fatalf("first save should store operating_review_enabled=false, got %+v", saved)
	}

	putCharterAs(t, "", squadID, charterBody(1, "omits flag")).Want(http.StatusOK).JSON(&saved)
	if saved.OperatingReviewEnabled {
		t.Fatalf("a save that omits the flag must keep it off, got %+v", saved)
	}

	on := charterBody(2, "turns it on")
	on["operating_review_enabled"] = true
	putCharterAs(t, "", squadID, on).Want(http.StatusOK).JSON(&saved)
	if !saved.OperatingReviewEnabled {
		t.Fatalf("expected the flag on after an explicit true, got %+v", saved)
	}
}

// TestSquadCharterValidation: each rejected input is refused before anything is
// written, so the charter stays at revision 0.
func TestSquadCharterValidation(t *testing.T) {
	leaderID := createHandlerTestAgent(t, "charter-valid-leader-"+uniqueSuffix(), nil)
	squadID := newCharterSquad(t, "Charter Validation Squad", leaderID)

	cases := []struct {
		name string
		over map[string]any
	}{
		{"missing revision", map[string]any{"revision": nil}},
		{"negative revision", map[string]any{"revision": -1}},
		{"mission too long", map[string]any{"mission": strings.Repeat("m", 2001)}},
		{"response hours zero", map[string]any{"response_hours": 0}},
		{"response hours above 720", map[string]any{"response_hours": 721}},
		{"duplicate responsibility key", map[string]any{"responsibilities": []map[string]any{
			{"key": "a", "text": "one"}, {"key": "a", "text": "two"},
		}}},
		{"empty responsibility text", map[string]any{"responsibilities": []map[string]any{
			{"key": "a", "text": ""},
		}}},
		{"unknown output kind", map[string]any{"outputs": []map[string]any{
			{"key": "a", "kind": "email"},
		}}},
		{"output without key", map[string]any{"outputs": []map[string]any{
			{"kind": "report"},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := charterBody(0, "rejected")
			for k, v := range tc.over {
				body[k] = v
			}
			putCharterAs(t, "", squadID, body).Want(http.StatusBadRequest)
		})
	}

	var got SquadCharterResponse
	getCharterAs(t, "", squadID).Want(http.StatusOK).JSON(&got)
	if got.Revision != 0 {
		t.Fatalf("rejected saves must not write a charter, got revision %d", got.Revision)
	}
}

// TestSquadCharterIsolatedFromOtherWorkspaces: a squad in another workspace is
// not found through this workspace, for reads or saves.
func TestSquadCharterIsolatedFromOtherWorkspaces(t *testing.T) {
	suffix := uniqueSuffix()
	ws := dbfx.Workspace(t, "Charter Other "+suffix, "charter-other-ws-"+suffix)
	other := testutil.New(testPool, ws, testUserID)
	leaderID := createHandlerTestAgent(t, "charter-iso-leader-"+suffix, nil)
	foreign := other.Squad(t, "Foreign Charter Squad", leaderID)
	other.Exec(t, `INSERT INTO squad_charter (squad_id, workspace_id, mission) VALUES ($1, $2, 'foreign')`, foreign, ws)
	other.Cleanup(t, `DELETE FROM squad_charter WHERE squad_id = $1`, foreign)

	getCharterAs(t, "", foreign).Want(http.StatusNotFound)
	putCharterAs(t, "", foreign, charterBody(1, "hijack")).Want(http.StatusNotFound)

	var mission string
	dbfx.QueryRow(t, `SELECT mission FROM squad_charter WHERE squad_id = $1`, foreign).Scan(&mission)
	if mission != "foreign" {
		t.Fatalf("a cross-workspace save must not change the charter, got %q", mission)
	}
}
