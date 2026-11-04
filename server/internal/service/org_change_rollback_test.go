package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// requireRollback files the rollback of an applied change through probation and
// checks the proposal it files: pending for the boss, of the org_change kind
// (which no standing approval may decide), and carrying the expected payload.
func (e *orgEnv) requireRollback(app db.OrgChangeApplication, wantType string, want map[string]string) {
	e.t.Helper()
	ctx := context.Background()
	probation := &ProbationService{Queries: e.q, Changes: e.changes, Scorecards: e.scorecards(e.now), Proposals: e.props, Now: func() time.Time { return e.now }}
	probation.fileRollback(ctx, app)
	got, err := e.q.GetOrgChangeApplicationInWorkspace(ctx, db.GetOrgChangeApplicationInWorkspaceParams{ID: app.ID, WorkspaceID: e.ws})
	if err != nil {
		e.t.Fatal(err)
	}
	if !got.RollbackProposalID.Valid {
		e.t.Fatalf("%s: a worse verdict filed no rollback proposal", app.ChangeType)
	}
	prop, err := e.q.GetProposalInWorkspace(ctx, db.GetProposalInWorkspaceParams{ID: got.RollbackProposalID, WorkspaceID: e.ws})
	if err != nil {
		e.t.Fatal(err)
	}
	if prop.State != "pending" {
		e.t.Fatalf("%s: rollback proposal state %q, want pending (the boss decides)", app.ChangeType, prop.State)
	}
	if prop.Kind != proposalkinds.KeyOrgChange || proposalkinds.ForbiddenForStandingApproval(prop.Kind, false, prop.Payload) != proposalkinds.PolicyRefusalForbidden {
		e.t.Fatalf("%s: rollback kind %q must stay forbidden for standing approvals", app.ChangeType, prop.Kind)
	}
	var payload map[string]any
	if err := json.Unmarshal(prop.Payload, &payload); err != nil {
		e.t.Fatal(err)
	}
	if payload["type"] != wantType {
		e.t.Fatalf("%s: rollback type %v, want %s", app.ChangeType, payload["type"], wantType)
	}
	for k, v := range want {
		if payload[k] != v {
			e.t.Fatalf("%s: rollback %s = %v, want %s", app.ChangeType, k, payload[k], v)
		}
	}
}

// Unretire restores an archived agent without re-queuing anything, and its
// revert retires the agent again, cancelling what it had queued.
func TestUnretireRestoresAnAgentAndItsRevertRetiresIt(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	if _, _, err := e.changes.ValidateOrgChange(ctx, e.q, e.ws, []byte(`{"type":"unretire","agent_id":"`+util.UUIDToString(e.subject)+`","rationale":"r"}`)); err == nil {
		t.Fatal("unretire of a live agent must be refused at filing")
	}
	leaver := e.fx.Agent(t, "unretire leaver", "")
	runtimeID := e.fx.Runtime(t, "unretire runtime")
	e.fx.Task(t, leaver, testutil.Cols{"status": "queued", "runtime_id": runtimeID})
	e.applyOrgChange(`{"type":"retire","agent_id":"` + leaver + `","rationale":"No longer needed."}`)
	if n := e.fx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1 AND status = 'queued'`, leaver); n != 0 {
		t.Fatalf("queued tasks after retire = %d, want 0", n)
	}

	restore := e.applyOrgChange(`{"type":"unretire","agent_id":"` + leaver + `","rationale":"The work came back."}`)
	agent, err := e.q.GetAgent(ctx, mustUUID(t, leaver))
	if err != nil {
		t.Fatal(err)
	}
	if agent.ArchivedAt.Valid {
		t.Fatal("unretire must restore the agent")
	}
	if n := e.fx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1 AND status = 'queued'`, leaver); n != 0 {
		t.Fatalf("unretire re-queued %d tasks, want none", n)
	}
	e.requireProbationWindow(restore)

	e.revertOrgChange(restore)
	agent, err = e.q.GetAgent(ctx, mustUUID(t, leaver))
	if err != nil {
		t.Fatal(err)
	}
	if !agent.ArchivedAt.Valid {
		t.Fatal("reverting an unretire must retire the agent again")
	}
}

func TestUnretireRestoresASquadAndItsRevertRetiresIt(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	squad := e.fx.Squad(t, "Retired squad", util.UUIDToString(e.reviewer.ID))
	e.applyOrgChange(`{"type":"retire","squad_id":"` + squad + `","rationale":"The floor no longer needs it."}`)
	restore := e.applyOrgChange(`{"type":"unretire","squad_id":"` + squad + `","rationale":"It is needed again."}`)
	sq, err := e.q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: mustUUID(t, squad), WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if sq.ArchivedAt.Valid {
		t.Fatal("unretire must unarchive the squad")
	}
	e.revertOrgChange(restore)
	sq, err = e.q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: mustUUID(t, squad), WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if !sq.ArchivedAt.Valid {
		t.Fatal("reverting an unretire must archive the squad again")
	}
}

// Unmerge restores the source squad from the merge's snapshot: its routines and
// the members the merge moved come back, and the merged charter text stays as
// the boss left it. The merge is marked undone so it cannot be unmerged twice.
func TestUnmergeRestoresTheSourceAndLeavesTheMergedCharter(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	// The source is a new squad: the merge archives it, and the reviewer's squad
	// (e.squad) must stay live to file the unmerge from.
	source := mustUUID(t, e.fx.Squad(t, "Unmerge source", util.UUIDToString(e.reviewer.ID)))
	sourceID := util.UUIDToString(source)
	target := util.UUIDToString(e.squad)
	e.fx.Exec(t, `UPDATE agent SET home_squad_id = $2 WHERE id = $1`, util.UUIDToString(e.subject), sourceID)
	e.fx.SquadMember(t, sourceID, "agent", util.UUIDToString(e.subject))
	ap, err := CreateSquadScheduledAutopilot(ctx, e.q, SquadScheduledAutopilot{
		WorkspaceID: e.ws, SquadID: source, ActorUserID: e.owner, Title: "Weekly check",
		Status: "active", Cron: "0 9 * * 1", Timezone: "UTC", NextRun: e.now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("routine autopilot: %v", err)
	}
	if _, err := e.q.InsertSquadRoutine(ctx, db.InsertSquadRoutineParams{
		AutopilotID: ap.ID, WorkspaceID: e.ws, SquadID: source, Purpose: "Weekly check",
		ExpectedOutput: "none", Required: true, GraceHours: 24,
	}); err != nil {
		t.Fatalf("routine: %v", err)
	}

	merge := e.applyOrgChange(`{"type":"merge_squads","from_squad_id":"` + sourceID +
		`","into_squad_id":"` + target + `","base_revision":0,` +
		`"charter":{"mission":"Merged mission","responsibilities":[],"outputs":[],"boundaries":"","escalation":"","intake":"","response_hours":24},` +
		`"rationale":"Two squads now do one job."}`)
	if n := e.fx.Count(t, `SELECT COUNT(*) FROM governance_event WHERE workspace_id = $1 AND kind = 'org_change_applied' AND subject_id = $2 AND details ? 'from_members' AND details ? 'from_routines'`, e.wsID, target); n != 1 {
		t.Fatalf("merge governance entry with the snapshot = %d, want 1", n)
	}

	unmerge := e.applyOrgChange(`{"type":"unmerge","application_id":"` + util.UUIDToString(merge.ID) + `","rationale":"The merge did not work."}`)
	from, err := e.q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: source, WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if from.ArchivedAt.Valid {
		t.Fatal("unmerge must unarchive the source squad")
	}
	subject, err := e.q.GetAgent(ctx, e.subject)
	if err != nil {
		t.Fatal(err)
	}
	if subject.HomeSquadID != source {
		t.Fatalf("subject home after unmerge = %s, want the source", util.UUIDToString(subject.HomeSquadID))
	}
	if n := e.fx.Count(t, `SELECT COUNT(*) FROM squad_routine WHERE squad_id = $1`, sourceID); n != 1 {
		t.Fatalf("routines in the source after unmerge = %d, want 1", n)
	}
	if n := e.fx.Count(t, `SELECT COUNT(*) FROM squad_routine WHERE squad_id = $1`, target); n != 0 {
		t.Fatalf("routines in the target after unmerge = %d, want 0", n)
	}
	member, err := e.q.IsSquadMember(ctx, db.IsSquadMemberParams{SquadID: e.squad, MemberType: "agent", MemberID: e.subject})
	if err != nil {
		t.Fatal(err)
	}
	if member {
		t.Fatal("the member the merge added to the target must leave it")
	}
	charter, err := e.q.GetSquadCharter(ctx, db.GetSquadCharterParams{SquadID: e.squad, WorkspaceID: e.ws})
	if err != nil {
		t.Fatalf("merged charter: %v", err)
	}
	if charter.Mission != "Merged mission" {
		t.Fatalf("the merged charter = %q after unmerge, want the text the merge wrote", charter.Mission)
	}
	e.requireProbationWindow(unmerge)

	// Refusals: the same merge cannot be unmerged again, an unmerge cannot be
	// reverted, and reverting the merge is refused because it is already undone.
	if _, _, err := e.changes.ValidateOrgChange(ctx, e.q, e.ws, []byte(`{"type":"unmerge","application_id":"`+util.UUIDToString(merge.ID)+`","rationale":"again"}`)); err == nil {
		t.Fatal("a second unmerge of the same merge must be refused at filing")
	}
	if _, err := e.changes.RevertOrgChange(ctx, e.q, e.ws, unmerge.ID, e.boss()); err == nil {
		t.Fatal("an unmerge must not be revertible")
	}
	if _, err := e.changes.RevertOrgChange(ctx, e.q, e.ws, merge.ID, e.boss()); err == nil {
		t.Fatal("a merge already unmerged must not be reverted again")
	}
}

// Each rollback is filed as a pending proposal for the boss. The rollback of a
// hire retires the agent, of a retire unretires it, of a merge unmerges it, and
// of an unmerge merges the two squads again.
func TestRollbacksOfTheRollbackTypesFileProposalsForTheBoss(t *testing.T) {
	t.Run("hire", func(t *testing.T) {
		e := newOrgEnv(t)
		runtime := e.fx.Runtime(t, "rollback runtime")
		hire := e.applyOrgChange(`{"type":"hire","squad_id":"` + util.UUIDToString(e.squad) + `","name":"Rolled Back Hire","template":"critic","runtime_id":"` + runtime + `","rationale":"r"}`)
		e.requireRollback(hire, OrgChangeRetire, map[string]string{"agent_id": util.UUIDToString(hire.SubjectID)})
	})
	t.Run("retire agent", func(t *testing.T) {
		e := newOrgEnv(t)
		leaver := e.fx.Agent(t, "rollback leaver", "")
		retire := e.applyOrgChange(`{"type":"retire","agent_id":"` + leaver + `","rationale":"r"}`)
		e.requireRollback(retire, OrgChangeUnretire, map[string]string{"agent_id": leaver})
	})
	t.Run("retire squad", func(t *testing.T) {
		e := newOrgEnv(t)
		squad := e.fx.Squad(t, "Rollback squad", util.UUIDToString(e.reviewer.ID))
		retire := e.applyOrgChange(`{"type":"retire","squad_id":"` + squad + `","rationale":"r"}`)
		e.requireRollback(retire, OrgChangeUnretire, map[string]string{"squad_id": squad})
	})
	t.Run("merge", func(t *testing.T) {
		e := newOrgEnv(t)
		// The source is a new squad, so the reviewer's squad stays live to file from.
		source := e.fx.Squad(t, "Merge source", util.UUIDToString(e.reviewer.ID))
		target := util.UUIDToString(e.squad)
		merge := e.applyOrgChange(`{"type":"merge_squads","from_squad_id":"` + source + `","into_squad_id":"` + target + `","base_revision":0,"rationale":"r"}`)
		e.requireRollback(merge, OrgChangeUnmerge, map[string]string{"application_id": util.UUIDToString(merge.ID)})
	})
	t.Run("unmerge", func(t *testing.T) {
		e := newOrgEnv(t)
		source := e.fx.Squad(t, "Unmerge source", util.UUIDToString(e.reviewer.ID))
		target := util.UUIDToString(e.squad)
		merge := e.applyOrgChange(`{"type":"merge_squads","from_squad_id":"` + source + `","into_squad_id":"` + target + `","base_revision":0,"rationale":"r"}`)
		unmerge := e.applyOrgChange(`{"type":"unmerge","application_id":"` + util.UUIDToString(merge.ID) + `","rationale":"r"}`)
		e.requireRollback(unmerge, OrgChangeMerge, map[string]string{"from_squad_id": source, "into_squad_id": target})
	})
}
