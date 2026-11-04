package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DB-backed tests for the phase 4 office steps: the retrospective triggers, the
// scheduler-log job registry, the digest threshold and floor filter, and the
// company export and import round trip. They run against the shared test
// database, with rows that the fixtures clean up.

func officeWorkspace(t *testing.T) (principalFixture, string) {
	t.Helper()
	return newPrincipalFixture(t)
}

// ownerAndLeader returns a runtime and an agent the fixture's workspace owns.
func ownerAndLeader(t *testing.T, f principalFixture, name string) (runtimeID, agentID string) {
	t.Helper()
	runtimeID = f.Runtime(t, name+" runtime")
	agentID = f.Agent(t, name, runtimeID)
	return runtimeID, agentID
}

func TestRetroAgentFailuresCountOnlyAgentCausesAndFireOncePerWindow(t *testing.T) {
	f, _ := officeWorkspace(t)
	ctx := context.Background()
	ws := util.MustParseUUID(f.WorkspaceID)
	_, agentID := ownerAndLeader(t, f, "Build Bot")
	_, offlineAgent := ownerAndLeader(t, f, "Waits For Runtime")

	failedAt := time.Now().Add(-24 * time.Hour)
	for i := 0; i < 3; i++ {
		f.Task(t, agentID, testutil.Cols{"status": "failed", "failure_reason": "agent_error", "completed_at": failedAt})
		f.Task(t, offlineAgent, testutil.Cols{"status": "failed", "failure_reason": "runtime_offline", "completed_at": failedAt})
	}

	svc := &RetroService{Queries: f.q, Now: func() time.Time { return time.Now() }}
	cands, err := svc.Candidates(ctx, ws, time.Now())
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	var agentCands []RetroCandidate
	for _, c := range cands {
		if c.Trigger == RetroTriggerAgentFailures {
			agentCands = append(agentCands, c)
		}
	}
	if len(agentCands) != 1 || util.UUIDToString(agentCands[0].SubjectID) != agentID {
		t.Fatalf("want one agent_failures candidate for the agent with three agent_error runs, got %+v", agentCands)
	}

	// A firing inside the window opens nothing: fire returns before it needs an
	// issue service, so none is wired here.
	f.Insert(t, "retro_trigger", testutil.Cols{
		"workspace_id": f.WorkspaceID, "trigger_kind": RetroTriggerAgentFailures,
		"subject_type": RetroSubjectAgent, "subject_id": agentID,
		"evidence": testutil.Raw("'{}'::jsonb"), "fired_at": time.Now().Add(-48 * time.Hour),
	})
	fired, err := svc.fire(ctx, ws, agentCands[0], time.Now())
	if err != nil || fired {
		t.Fatalf("a subject that fired two days ago is inside its window: fired=%v err=%v", fired, err)
	}
}

func TestRetroSquadRejectionsNeedThreeRejectionsInARow(t *testing.T) {
	f, _ := officeWorkspace(t)
	ctx := context.Background()
	ws := util.MustParseUUID(f.WorkspaceID)
	_, leaderAgent := ownerAndLeader(t, f, "Squad Lead")
	squadID := f.Squad(t, "Review Squad", leaderAgent)

	for _, state := range []string{"rejected", "rejected", "rejected"} {
		issue := f.Issue(t, "Proposal backing issue")
		f.Insert(t, "proposal", testutil.Cols{
			"workspace_id": f.WorkspaceID, "squad_id": squadID, "issue_id": issue,
			"state": state, "created_by_type": "agent", "created_by_id": leaderAgent,
			"decided_at": time.Now().Add(-time.Hour), "kind": "generic",
		})
	}
	rows, err := f.q.ListSquadRecentDecisions(ctx, ws)
	if err != nil {
		t.Fatalf("decisions: %v", err)
	}
	found := false
	for _, id := range RetroRejectedInARow(rows, time.Now().Add(-RetroWindow)) {
		if util.UUIDToString(id) == squadID {
			found = true
		}
	}
	if !found {
		t.Fatalf("a squad whose three newest proposals were rejected must trigger, rows=%d", len(rows))
	}
}

func TestJobStatusReadsTheSchedulerLog(t *testing.T) {
	f, _ := officeWorkspace(t)
	ctx := context.Background()
	now := time.Now()
	f.Insert(t, "sys_cron_executions", testutil.Cols{
		"job_name": "proposal_expiry", "scope_kind": "global", "scope_id": "global",
		"plan_time": now.Add(-time.Minute), "status": "SUCCESS",
		"finished_at": now.Add(-time.Minute), "lease_token": testutil.Raw("gen_random_uuid()"),
	})
	f.Insert(t, "sys_cron_executions", testutil.Cols{
		"job_name": "knowledge_eval", "scope_kind": "global", "scope_id": "global",
		"plan_time": now.Add(-2 * time.Hour), "status": "RUNNING",
		"stale_after": now.Add(-time.Hour), "lease_token": testutil.Raw("gen_random_uuid()"),
	})
	statuses, err := ReadJobStatuses(ctx, f.q, now)
	if err != nil {
		t.Fatalf("read job statuses: %v", err)
	}
	byName := map[string]JobStatus{}
	for _, s := range statuses {
		byName[s.Name] = s
	}
	if s := byName["proposal_expiry"]; s.LastSuccessAt == nil || s.LagSeconds < 0 {
		t.Fatalf("proposal_expiry has a recent success: %+v", s)
	}
	if s := byName["knowledge_eval"]; !s.Stuck {
		t.Fatalf("a run holding its lease past the stale time is stuck: %+v", s)
	}
	risks := (&DigestService{Queries: f.q}).systemRisks(ctx, now)
	found := false
	for _, r := range risks {
		if strings.Contains(r, "Knowledge eval is stuck") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a stuck job is a risk line in the digest, got %v", risks)
	}
}

func TestDigestAutoModeSwitchesAtThirtyOneSquadsAndFloorFilterScopesTheDigest(t *testing.T) {
	f, _ := officeWorkspace(t)
	ctx := context.Background()
	ws := util.MustParseUUID(f.WorkspaceID)
	_, leader := ownerAndLeader(t, f, "Floor Lead")

	floorID := f.Insert(t, "office_floor", testutil.Cols{"workspace_id": f.WorkspaceID, "name": "Research", "position": 0})
	var onFloor []string
	for i := 0; i < 30; i++ {
		id := f.Squad(t, fmt.Sprintf("Squad %02d", i), leader)
		if i < 2 {
			f.Exec(t, `UPDATE squad SET floor_id = $1, office_slot = $2 WHERE id = $3`, floorID, i, id)
			onFloor = append(onFloor, id)
		}
	}

	n, err := f.q.CountLiveSquads(ctx, ws)
	if err != nil {
		t.Fatalf("count squads: %v", err)
	}
	if got := ResolveDigestMode(DigestModeAuto, int(n)); got != DigestModeSquads {
		t.Fatalf("30 active squads read squads, got %q (count %d)", got, n)
	}
	f.Squad(t, "Squad 31", leader)
	n, _ = f.q.CountLiveSquads(ctx, ws)
	if got := ResolveDigestMode(DigestModeAuto, int(n)); got != DigestModeFloors {
		t.Fatalf("31 active squads read floor briefs, got %q (count %d)", got, n)
	}

	svc := NewDigestService(f.q)
	svc.FloorID = util.MustParseUUID(floorID)
	digest, err := svc.Compute(ctx, ws, time.Now().Add(-24*time.Hour), "TST")
	if err != nil {
		t.Fatalf("floor digest: %v", err)
	}
	if digest.Scope.Type != "floor" {
		t.Fatalf("the floor digest says it is a floor digest, got %q", digest.Scope.Type)
	}
	// Quiet squads are counted, not named, past the first few; the floor's two
	// squads have no news, so they show as quiet and the count is what matters.
	if got := len(digest.Squads) + digest.Quiet.Count; got != len(onFloor) {
		t.Fatalf("the floor digest covers only the floor's %d squads, got %d", len(onFloor), got)
	}
	for _, sq := range digest.Squads {
		if sq.FloorID == nil || *sq.FloorID != floorID {
			t.Fatalf("squad %s is not on the floor", sq.Name)
		}
	}
	for _, sq := range digest.Quiet.Squads {
		if !contains(onFloor, sq.ID) {
			t.Fatalf("quiet squad %s is not on the floor", sq.Name)
		}
	}
}

func TestCompanyExportRoundTripsIntoAnEmptyWorkspace(t *testing.T) {
	src, _ := officeWorkspace(t)
	ctx := context.Background()
	srcWS := util.MustParseUUID(src.WorkspaceID)
	srcRuntime, leader := ownerAndLeader(t, src, "Round Lead")
	worker := src.Agent(t, "Round Worker", srcRuntime)
	squadID := src.Squad(t, "Round Squad", leader)
	src.SquadMember(t, squadID, "agent", leader, testutil.Cols{"role": "leader"})
	src.SquadMember(t, squadID, "agent", worker, testutil.Cols{"role": "member"})
	src.Exec(t, `UPDATE agent SET home_squad_id = $1 WHERE id = $2`, squadID, worker)
	floorID := src.Insert(t, "office_floor", testutil.Cols{"workspace_id": src.WorkspaceID, "name": "Round Floor", "position": 0})
	src.Exec(t, `UPDATE squad SET floor_id = $1, office_slot = 0 WHERE id = $2`, floorID, squadID)
	src.Exec(t, `INSERT INTO squad_charter (squad_id, workspace_id, mission, responsibilities, outputs, response_hours, operating_review_enabled, revision)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, 24, true, 1)`,
		squadID, src.WorkspaceID, "Round trip the structure", `["keep the record"]`, `[]`)

	doc, err := ExportCompany(ctx, src.q, srcWS, time.Now())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, _ := json.Marshal(doc)
	if strings.Contains(string(raw), srcRuntime) {
		t.Fatal("the export must not carry runtime ids")
	}
	if !strings.Contains(string(raw), "retention_note") || len(doc.Excluded) == 0 {
		t.Fatal("the export names its exclusions and the retention note")
	}

	dst := newSecondPrincipal(t, src)
	dstWS := util.MustParseUUID(dst.WorkspaceID)
	dstRuntime := dst.Runtime(t, "Dst runtime")
	opts := ImportOptions{ActorID: util.MustParseUUID(dst.UserID), RuntimeID: util.MustParseUUID(dstRuntime)}

	dry, err := ImportCompany(ctx, dst.q, dst.Pool, dstWS, doc, ImportOptions{DryRun: true, ActorID: opts.ActorID})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dry.DryRun || dry.Created == 0 || dry.Blocked == 0 {
		t.Fatalf("a dry run without a runtime plans creations and blocks agents, got %+v", dry)
	}
	if n := dst.Count(t, `SELECT count(*) FROM squad WHERE workspace_id = $1`, dst.WorkspaceID); n != 0 {
		t.Fatalf("a dry run writes nothing, found %d squads", n)
	}

	if _, err := ImportCompany(ctx, dst.q, dst.Pool, dstWS, doc, opts); err != nil {
		t.Fatalf("apply: %v", err)
	}
	again, err := ImportCompany(ctx, dst.q, dst.Pool, dstWS, doc, opts)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if again.Created != 0 {
		t.Fatalf("a second import changes nothing, got %d creations", again.Created)
	}

	back, err := ExportCompany(ctx, dst.q, dstWS, time.Now())
	if err != nil {
		t.Fatalf("export of the copy: %v", err)
	}
	if fmt.Sprint(names(back.Floors)) != fmt.Sprint(names(doc.Floors)) {
		t.Fatalf("floors: got %v, want %v", names(back.Floors), names(doc.Floors))
	}
	if fmt.Sprint(agentNames(back.Agents)) != fmt.Sprint(agentNames(doc.Agents)) {
		t.Fatalf("agents: got %v, want %v", agentNames(back.Agents), agentNames(doc.Agents))
	}
	if len(back.Squads) != 1 || back.Squads[0].Leader != "Round Lead" || len(back.Squads[0].Members) != len(doc.Squads[0].Members) {
		t.Fatalf("squad structure differs: %+v", back.Squads)
	}
	if back.Squads[0].Floor != "Round Floor" || back.Squads[0].Charter == nil || back.Squads[0].Charter.Mission != "Round trip the structure" {
		t.Fatalf("the squad keeps its floor and charter: %+v", back.Squads[0])
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func names(floors []ExportFloor) []string {
	out := make([]string, 0, len(floors))
	for _, f := range floors {
		out = append(out, f.Name)
	}
	return out
}

func agentNames(agents []ExportAgent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, a.Name)
	}
	return out
}

// newSecondPrincipal builds a second workspace with an owner, for the import target.
func newSecondPrincipal(t *testing.T, base principalFixture) principalFixture {
	t.Helper()
	fx := testutil.New(base.Pool, "", "")
	owner := fx.User(t, "import owner", fmt.Sprintf("import-owner-%d@multica.test", time.Now().UnixNano()))
	ws := fx.Workspace(t, "import target", fmt.Sprintf("import-target-%d", time.Now().UnixNano()))
	fx.Member(t, ws, owner, "owner")
	fx.WorkspaceID = ws
	fx.UserID = owner
	return principalFixture{Fixture: fx, q: db.New(base.Pool)}
}
