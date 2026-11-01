package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// setupEnv is a workspace of its own, owned by the test user, with one online
// Hermes runtime, so a test sees only what its own setup made.
type setupEnv struct {
	WorkspaceID string
	RuntimeID   string
}

func newSetupEnv(t *testing.T) setupEnv {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ws := dbfx.Workspace(t, "Setup "+uuid.NewString(), "setup-"+uuid.NewString(), testutil.Cols{"issue_prefix": "SET"})
	dbfx.Member(t, ws, testUserID, "owner")
	rt := dbfx.Runtime(t, "Hermes "+uuid.NewString(), testutil.Cols{
		"workspace_id": ws,
		"provider":     "hermes",
		"status":       "online",
	})
	return setupEnv{WorkspaceID: ws, RuntimeID: rt}
}

func setupBody(over ...map[string]any) map[string]any {
	body := map[string]any{
		"secretary_instructions": "You are the Secretary. Read the digest and write the brief.",
		"timezone":               "Europe/Paris",
	}
	for _, o := range over {
		for k, v := range o {
			body[k] = v
		}
	}
	return body
}

func (e setupEnv) post(t *testing.T, userID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest("POST", "/api/office/setup", body)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	if userID != "" {
		req = asUser(req, userID)
	}
	return testutil.Call(t, testHandler.SetupOffice, req)
}

func (e setupEnv) get(t *testing.T, userID string) *testutil.Response {
	t.Helper()
	req := newRequest("GET", "/api/office/setup", nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	if userID != "" {
		req = asUser(req, userID)
	}
	return testutil.Call(t, testHandler.GetOfficeSetup, req)
}

// secretaryID reads the id of the workspace's Secretary straight from the row.
func (e setupEnv) secretaryID(t *testing.T) string {
	t.Helper()
	var id string
	dbfx.QueryRow(t, `SELECT COALESCE(secretary_agent_id::text, '') FROM workspace WHERE id = $1`, e.WorkspaceID).Scan(&id)
	return id
}

func (e setupEnv) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	return dbfx.Count(t, sql, append([]any{e.WorkspaceID}, args...)...)
}

// brief autopilot fields, read from the row the setup wrote.
type briefAutopilot struct {
	AssigneeType  string
	AssigneeID    string
	ExecutionMode string
	Status        string
	Cron          string
	Timezone      string
}

func (e setupEnv) briefAutopilot(t *testing.T, title string) briefAutopilot {
	t.Helper()
	var ap briefAutopilot
	dbfx.QueryRow(t, `
		SELECT a.assignee_type, a.assignee_id::text, a.execution_mode, a.status,
		       t.cron_expression, t.timezone
		FROM autopilot a
		JOIN autopilot_trigger t ON t.autopilot_id = a.id
		WHERE a.workspace_id = $1 AND a.title = $2 AND t.kind = 'schedule'`,
		e.WorkspaceID, title,
	).Scan(&ap.AssigneeType, &ap.AssigneeID, &ap.ExecutionMode, &ap.Status, &ap.Cron, &ap.Timezone)
	return ap
}

func TestOfficeSetup_CreatesEverythingOnce(t *testing.T) {
	e := newSetupEnv(t)

	var first OfficeSetupReadinessResponse
	e.post(t, "", setupBody()).Want(http.StatusOK).JSON(&first)
	if !first.Ready || first.SecretaryID == nil || !first.ExecutiveFloor || !first.AutopilotDaily || !first.AutopilotWeeklyPaused || !first.RuntimeOnline {
		t.Fatalf("first setup readiness = %+v, want every check true", first)
	}
	secretary := *first.SecretaryID
	if e.secretaryID(t) != secretary {
		t.Fatalf("workspace secretary_agent_id = %q, want %q", e.secretaryID(t), secretary)
	}

	counts := func() [4]int {
		return [4]int{
			e.count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1 AND archived_at IS NULL`),
			e.count(t, `SELECT count(*) FROM office_floor WHERE workspace_id = $1`),
			e.count(t, `SELECT count(*) FROM squad WHERE workspace_id = $1 AND archived_at IS NULL`),
			e.count(t, `SELECT count(*) FROM autopilot WHERE workspace_id = $1`),
		}
	}
	before := counts()

	// The second call finds everything the first one made and changes nothing.
	var second OfficeSetupReadinessResponse
	e.post(t, "", setupBody()).Want(http.StatusOK).JSON(&second)
	if second.SecretaryID == nil || *second.SecretaryID != secretary || !second.Ready {
		t.Fatalf("second setup readiness = %+v, want the same secretary and ready", second)
	}
	if after := counts(); after != before {
		t.Fatalf("second setup changed rows: before %v, after %v", before, after)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM squad WHERE workspace_id = $1 AND name = $2`, e.WorkspaceID, service.ExecutiveSquadName); n != 1 {
		t.Fatalf("Executive Office squads = %d, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM office_floor WHERE workspace_id = $1 AND name = $2`, e.WorkspaceID, service.ExecutiveFloorName); n != 1 {
		t.Fatalf("Executive floors = %d, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = $2`, e.WorkspaceID, service.SecretaryAgentName); n != 1 {
		t.Fatalf("Secretary agents = %d, want 1", n)
	}
}

func TestOfficeSetup_SecretaryAndSquadShape(t *testing.T) {
	e := newSetupEnv(t)
	e.post(t, "", setupBody()).Want(http.StatusOK)

	var provider, visibility, permission string
	var maxTasks int
	var runtimeID, squadLeader string
	dbfx.QueryRow(t, `
		SELECT r.provider, a.visibility, a.permission_mode, a.max_concurrent_tasks, a.runtime_id::text, s.leader_id::text
		FROM agent a
		JOIN agent_runtime r ON r.id = a.runtime_id
		JOIN squad s ON s.leader_id = a.id AND s.workspace_id = a.workspace_id
		WHERE a.workspace_id = $1 AND a.name = $2 AND s.name = $3`,
		e.WorkspaceID, service.SecretaryAgentName, service.ExecutiveSquadName,
	).Scan(&provider, &visibility, &permission, &maxTasks, &runtimeID, &squadLeader)

	if provider != "hermes" || runtimeID != e.RuntimeID {
		t.Fatalf("secretary runtime = %s on %s, want hermes on %s", provider, runtimeID, e.RuntimeID)
	}
	if visibility != "private" || permission != "private" || maxTasks != 1 {
		t.Fatalf("secretary visibility %s/%s, concurrency %d; want private, private, 1", visibility, permission, maxTasks)
	}
	if squadLeader != e.secretaryID(t) {
		t.Fatalf("Executive Office leader = %s, want the secretary %s", squadLeader, e.secretaryID(t))
	}

	var floorName string
	var slot int
	dbfx.QueryRow(t, `
		SELECT f.name, s.office_slot
		FROM squad s JOIN office_floor f ON f.id = s.floor_id
		WHERE s.workspace_id = $1 AND s.name = $2`,
		e.WorkspaceID, service.ExecutiveSquadName,
	).Scan(&floorName, &slot)
	if floorName != service.ExecutiveFloorName || slot != 0 {
		t.Fatalf("Executive Office seat = %s slot %d, want %s slot 0", floorName, slot, service.ExecutiveFloorName)
	}

	var homeSquad string
	dbfx.QueryRow(t, `
		SELECT COALESCE(a.home_squad_id::text, '') FROM agent a WHERE a.workspace_id = $1 AND a.name = $2`,
		e.WorkspaceID, service.SecretaryAgentName,
	).Scan(&homeSquad)
	var squadID string
	dbfx.QueryRow(t, `SELECT id::text FROM squad WHERE workspace_id = $1 AND name = $2`, e.WorkspaceID, service.ExecutiveSquadName).Scan(&squadID)
	if homeSquad != squadID {
		t.Fatalf("secretary home squad = %q, want the Executive Office %q", homeSquad, squadID)
	}

	var humanMembers int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM squad_member sm JOIN squad s ON s.id = sm.squad_id
		WHERE s.workspace_id = $1 AND s.name = $2 AND sm.member_type = 'member' AND sm.member_id = $3`,
		e.WorkspaceID, service.ExecutiveSquadName, testUserID,
	).Scan(&humanMembers)
	if humanMembers != 1 {
		t.Fatalf("boss memberships in Executive Office = %d, want 1", humanMembers)
	}
}

func TestOfficeSetup_DailyBriefTargetsTheAgentNotTheSquad(t *testing.T) {
	e := newSetupEnv(t)
	e.post(t, "", setupBody()).Want(http.StatusOK)

	daily := e.briefAutopilot(t, service.DailyBriefTitle)
	if daily.AssigneeType != "agent" || daily.AssigneeID != e.secretaryID(t) {
		t.Fatalf("daily brief assignee = %s %s, want the secretary agent", daily.AssigneeType, daily.AssigneeID)
	}
	if daily.ExecutionMode != "run_only" || daily.Status != "active" {
		t.Fatalf("daily brief mode %s status %s, want run_only and active", daily.ExecutionMode, daily.Status)
	}
	if daily.Cron != "0 8 * * *" || daily.Timezone != "Europe/Paris" {
		t.Fatalf("daily brief schedule %q in %q, want 0 8 * * * in Europe/Paris", daily.Cron, daily.Timezone)
	}
}

func TestOfficeSetup_WeeklyReviewStartsPaused(t *testing.T) {
	e := newSetupEnv(t)
	var out OfficeSetupReadinessResponse
	e.post(t, "", setupBody()).Want(http.StatusOK).JSON(&out)

	weekly := e.briefAutopilot(t, service.WeeklyReviewTitle)
	if weekly.Status != "paused" {
		t.Fatalf("weekly review status = %s, want paused", weekly.Status)
	}
	if weekly.Cron != "0 17 * * 5" {
		t.Fatalf("weekly review cron = %q, want Friday 17:00", weekly.Cron)
	}
	if weekly.AssigneeType != "agent" || weekly.AssigneeID != e.secretaryID(t) {
		t.Fatalf("weekly review assignee = %s %s, want the secretary agent", weekly.AssigneeType, weekly.AssigneeID)
	}
	if !out.AutopilotWeeklyPaused {
		t.Fatalf("readiness autopilot_weekly_paused = false after setup")
	}
}

func TestOfficeSetup_TimezoneFallsBackToUTC(t *testing.T) {
	e := newSetupEnv(t)
	e.post(t, "", map[string]any{"secretary_instructions": "You are the Secretary."}).Want(http.StatusOK)

	if daily := e.briefAutopilot(t, service.DailyBriefTitle); daily.Timezone != "UTC" {
		t.Fatalf("daily brief timezone = %q, want UTC when no timezone is given", daily.Timezone)
	}
}

func TestOfficeSetup_RefusedWithoutOnlineHermesRuntime(t *testing.T) {
	e := newSetupEnv(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, e.RuntimeID)

	resp := e.post(t, "", setupBody()).Want(http.StatusConflict)
	body := resp.Map()
	if body["code"] != "runtime_required" {
		t.Fatalf("code = %v, want runtime_required; body %v", body["code"], body)
	}
	if body["docs_path"] != "/install-agent-runtime" || body["hint"] == nil {
		t.Fatalf("refusal lacks the install hint: %v", body)
	}

	// Nothing was written.
	if n := e.count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1`); n != 0 {
		t.Fatalf("agents after refused setup = %d, want 0", n)
	}
	if e.secretaryID(t) != "" {
		t.Fatalf("secretary set after refused setup")
	}
}

func TestOfficeSetup_RefusedWhenOnlyOtherRuntimesAreOnline(t *testing.T) {
	e := newSetupEnv(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET provider = 'claude' WHERE id = $1`, e.RuntimeID)

	resp := e.post(t, "", setupBody()).Want(http.StatusConflict)
	if resp.Map()["code"] != "runtime_required" {
		t.Fatalf("code = %v, want runtime_required", resp.Map()["code"])
	}
}

func TestOfficeSetup_RefusedForMembers(t *testing.T) {
	e := newSetupEnv(t)
	member := dbfx.User(t, "Plain member "+uuid.NewString(), "setup-member-"+uuid.NewString()+"@example.com")
	dbfx.Member(t, e.WorkspaceID, member, "member")

	e.post(t, member, setupBody()).Want(http.StatusForbidden)
	e.get(t, member).Want(http.StatusForbidden)
	if n := e.count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1`); n != 0 {
		t.Fatalf("a member's refused setup created %d agents", n)
	}
}

func TestOfficeSetup_ReadinessBeforeSetupShowsWhatIsMissing(t *testing.T) {
	e := newSetupEnv(t)

	var out OfficeSetupReadinessResponse
	e.get(t, "").Want(http.StatusOK).JSON(&out)

	if !out.RuntimeOnline {
		t.Fatalf("runtime_online = false, want true with an online Hermes runtime")
	}
	if out.SecretaryID != nil || out.ExecutiveFloor || out.AutopilotDaily || out.AutopilotWeeklyPaused || out.Ready {
		t.Fatalf("readiness before setup = %+v, want everything missing", out)
	}
}

func TestOfficeSetup_WorkspaceIsolation(t *testing.T) {
	one := newSetupEnv(t)
	two := newSetupEnv(t)
	one.post(t, "", setupBody()).Want(http.StatusOK)

	var out OfficeSetupReadinessResponse
	two.get(t, "").Want(http.StatusOK).JSON(&out)
	if out.SecretaryID != nil || out.ExecutiveFloor || out.Ready {
		t.Fatalf("another workspace's setup leaked into this one: %+v", out)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM office_floor WHERE workspace_id = $1`, two.WorkspaceID); n != 0 {
		t.Fatalf("floors in the untouched workspace = %d, want 0", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = $2`, two.WorkspaceID, service.SecretaryAgentName); n != 0 {
		t.Fatalf("secretaries in the untouched workspace = %d, want 0", n)
	}
}

func TestOfficeSetup_ReusesTheWorkspacesSecretaryAndFloor(t *testing.T) {
	e := newSetupEnv(t)
	secretary := dbfx.Agent(t, service.SecretaryAgentName, e.RuntimeID, testutil.Cols{"workspace_id": e.WorkspaceID})
	dbfx.Exec(t, `UPDATE workspace SET secretary_agent_id = $1 WHERE id = $2`, secretary, e.WorkspaceID)
	dbfx.Insert(t, "office_floor", testutil.Cols{"workspace_id": e.WorkspaceID, "name": service.ExecutiveFloorName, "position": 0})

	var out OfficeSetupReadinessResponse
	e.post(t, "", setupBody()).Want(http.StatusOK).JSON(&out)

	if out.SecretaryID == nil || *out.SecretaryID != secretary {
		t.Fatalf("secretary = %v, want the existing %s", out.SecretaryID, secretary)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = $2`, e.WorkspaceID, service.SecretaryAgentName); n != 1 {
		t.Fatalf("Secretary agents = %d, want the one already there", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM office_floor WHERE workspace_id = $1 AND name = $2`, e.WorkspaceID, service.ExecutiveFloorName); n != 1 {
		t.Fatalf("Executive floors = %d, want the one already there", n)
	}
	if e.secretaryID(t) != secretary {
		t.Fatalf("setup changed the workspace secretary to %s", e.secretaryID(t))
	}
}

func TestOfficeSetup_RefusesToAdoptAnUnnamedSecretaryAgent(t *testing.T) {
	e := newSetupEnv(t)
	// An agent called Secretary that the workspace does not name: setup must
	// not take it over, so it refuses and names the conflict.
	dbfx.Agent(t, service.SecretaryAgentName, e.RuntimeID, testutil.Cols{"workspace_id": e.WorkspaceID})

	resp := e.post(t, "", setupBody()).Want(http.StatusConflict)
	if resp.Map()["code"] != "secretary_name_taken" {
		t.Fatalf("code = %v, want secretary_name_taken", resp.Map()["code"])
	}
	if e.secretaryID(t) != "" {
		t.Fatalf("setup named a secretary after a refusal")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM squad WHERE workspace_id = $1 AND name = $2`, e.WorkspaceID, service.ExecutiveSquadName); n != 0 {
		t.Fatalf("a refused setup still made the Executive Office squad (%d)", n)
	}
}

func TestOfficeSetup_RefusesWhenTheExecutiveFloorIsFull(t *testing.T) {
	e := newSetupEnv(t)
	floor := dbfx.Insert(t, "office_floor", testutil.Cols{"workspace_id": e.WorkspaceID, "name": service.ExecutiveFloorName, "position": 0})
	for slot := 0; slot < service.OfficeSlotsPerFloor; slot++ {
		leader := dbfx.Agent(t, "Filler "+uuid.NewString(), e.RuntimeID, testutil.Cols{"workspace_id": e.WorkspaceID})
		dbfx.Squad(t, "Filled "+uuid.NewString(), leader, testutil.Cols{
			"workspace_id": e.WorkspaceID,
			"floor_id":     floor,
			"office_slot":  slot,
		})
	}

	resp := e.post(t, "", setupBody()).Want(http.StatusConflict)
	if resp.Map()["code"] != "floor_full" {
		t.Fatalf("code = %v, want floor_full", resp.Map()["code"])
	}
	// The refusal rolls the whole setup back.
	if e.secretaryID(t) != "" {
		t.Fatalf("setup named a secretary after floor_full")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = $2`, e.WorkspaceID, service.SecretaryAgentName); n != 0 {
		t.Fatalf("a refused setup left %d Secretary agents behind", n)
	}
}

func TestOfficeSetup_ValidatesTheBody(t *testing.T) {
	e := newSetupEnv(t)

	e.post(t, "", map[string]any{"timezone": "Europe/Paris"}).Want(http.StatusBadRequest)
	e.post(t, "", setupBody(map[string]any{"secretary_instructions": "   "})).Want(http.StatusBadRequest)
	e.post(t, "", setupBody(map[string]any{"timezone": "Mars/Olympus"})).Want(http.StatusBadRequest)

	if n := e.count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1`); n != 0 {
		t.Fatalf("a rejected body still created %d agents", n)
	}
}

func TestOfficeSetup_FloorLimitMatchesTheService(t *testing.T) {
	// The office floor endpoints keep their own copy of the layout limits; this
	// keeps the two from drifting apart.
	if officeMaxFloors != service.OfficeMaxFloors {
		t.Fatalf("officeMaxFloors = %d, service.OfficeMaxFloors = %d", officeMaxFloors, service.OfficeMaxFloors)
	}
	if officeSlotsPerFloor != service.OfficeSlotsPerFloor {
		t.Fatalf("officeSlotsPerFloor = %d, service.OfficeSlotsPerFloor = %d", officeSlotsPerFloor, service.OfficeSlotsPerFloor)
	}
}
