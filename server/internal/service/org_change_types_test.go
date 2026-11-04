package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// These tests cover the org change types that org_change_test.go does not: each
// applies, reverts to the state before, and opens a probation window (spec 4.6,
// plan 11.14). The instruction, move and retire types are covered there.

// applyOrgChange files a change, approves it and returns its application row.
func (e *orgEnv) applyOrgChange(payload string) db.OrgChangeApplication {
	e.t.Helper()
	res := e.file(proposalkinds.KeyOrgChange, payload)
	e.approve(res.Proposal.ID)
	app, err := e.q.GetOrgChangeApplicationByProposal(context.Background(), db.GetOrgChangeApplicationByProposalParams{
		WorkspaceID: e.ws, ProposalID: res.Proposal.ID,
	})
	if err != nil {
		e.t.Fatalf("application row: %v", err)
	}
	return app
}

// revertOrgChange reverts an application as the boss.
func (e *orgEnv) revertOrgChange(app db.OrgChangeApplication) {
	e.t.Helper()
	if _, err := e.changes.RevertOrgChange(context.Background(), e.q, e.ws, app.ID, e.boss()); err != nil {
		e.t.Fatalf("revert %s: %v", app.ChangeType, err)
	}
}

// requireProbationWindow checks that the application opened the 14-day window
// from the day it was applied.
func (e *orgEnv) requireProbationWindow(app db.OrgChangeApplication) {
	e.t.Helper()
	if !app.ProbationEndsAt.Valid || !app.ProbationEndsAt.Time.Equal(e.now.Add(OrgChangeProbationWindow)) {
		e.t.Fatalf("%s: probation window = %v, want 14 days from %s", app.ChangeType, app.ProbationEndsAt, e.now.Format(time.RFC3339))
	}
}

// skill creates a workspace skill and returns its id as text.
func (e *orgEnv) skill(name string) pgtype.UUID {
	e.t.Helper()
	sk, err := e.q.CreateSkill(context.Background(), db.CreateSkillParams{
		WorkspaceID: e.ws, Name: name, Description: "", Content: "Do the thing.", Config: []byte("{}"), CreatedBy: e.owner,
	})
	if err != nil {
		e.t.Fatalf("create skill: %v", err)
	}
	return sk.ID
}

// skillIDs returns the subject's enabled skills as a set of id texts.
func (e *orgEnv) skillIDs(agent pgtype.UUID) map[string]bool {
	e.t.Helper()
	rows, err := e.q.ListAgentSkills(context.Background(), agent)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]bool{}
	for _, r := range rows {
		out[util.UUIDToString(r.ID)] = true
	}
	return out
}

func TestSkillChangeAppliesAndReverts(t *testing.T) {
	e := newOrgEnv(t)
	kept := e.skill("kept skill")
	added := e.skill("added skill")
	if err := e.q.AddAgentSkill(context.Background(), db.AddAgentSkillParams{AgentID: e.subject, SkillID: kept}); err != nil {
		t.Fatal(err)
	}
	app := e.applyOrgChange(`{"type":"skill_change","agent_id":"` + util.UUIDToString(e.subject) +
		`","add_skill_ids":["` + util.UUIDToString(added) + `"],"remove_skill_ids":["` + util.UUIDToString(kept) +
		`"],"rationale":"The runs need the new skill."}`)

	got := e.skillIDs(e.subject)
	if !got[util.UUIDToString(added)] || got[util.UUIDToString(kept)] {
		t.Fatalf("skills after apply = %v, want the added skill only", got)
	}
	e.requireProbationWindow(app)

	e.revertOrgChange(app)
	got = e.skillIDs(e.subject)
	if !got[util.UUIDToString(kept)] || got[util.UUIDToString(added)] {
		t.Fatalf("skills after revert = %v, want the kept skill only", got)
	}
}

// charter creates a squad charter at revision 1, as an earlier write would.
func (e *orgEnv) charter(mission string) {
	e.t.Helper()
	e.fx.Exec(e.t, `INSERT INTO squad_charter (squad_id, workspace_id, mission, responsibilities, outputs, response_hours, operating_review_enabled, revision)
		VALUES ($1, $2, $3, '[]'::jsonb, '[]'::jsonb, 24, false, 1)`, util.UUIDToString(e.squad), e.wsID, mission)
}

func TestCharterChangeAppliesAndReverts(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	e.charter("Keep the reviews")
	squadID := util.UUIDToString(e.squad)

	app := e.applyOrgChange(`{"type":"charter_change","squad_id":"` + squadID + `","base_revision":1,` +
		`"charter":{"mission":"Review every squad monthly","responsibilities":[],"outputs":[],"boundaries":"","escalation":"","intake":"","response_hours":48},` +
		`"rationale":"The review cadence needs to be explicit."}`)
	c, err := e.q.GetSquadCharter(ctx, db.GetSquadCharterParams{SquadID: e.squad, WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if c.Mission != "Review every squad monthly" || c.Revision != 2 {
		t.Fatalf("charter after apply = %q at revision %d", c.Mission, c.Revision)
	}
	e.requireProbationWindow(app)

	// A revert writes the earlier charter as a new revision, so no write is lost.
	e.revertOrgChange(app)
	c, err = e.q.GetSquadCharter(ctx, db.GetSquadCharterParams{SquadID: e.squad, WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if c.Mission != "Keep the reviews" || c.Revision != 3 {
		t.Fatalf("charter after revert = %q at revision %d, want the earlier mission at revision 3", c.Mission, c.Revision)
	}
}

// A charter change against a stale revision is refused at filing, so a charter
// edited since the proposal was written is never overwritten.
func TestCharterChangeRefusesAStaleRevision(t *testing.T) {
	e := newOrgEnv(t)
	e.charter("Keep the reviews")
	payload := `{"type":"charter_change","squad_id":"` + util.UUIDToString(e.squad) + `","base_revision":0,` +
		`"charter":{"mission":"x","responsibilities":[],"outputs":[],"boundaries":"","escalation":"","intake":"","response_hours":48},"rationale":"r"}`
	_, err := e.props.Create(context.Background(), ProposalCreateParams{
		WorkspaceID: e.ws, SquadID: e.squad,
		Content:     ProposalContent{Title: "t", Summary: "s", Evidence: []string{"e"}, Recommendation: "r", Options: []string{"a"}, CostRisk: "c", Ask: "a"},
		CreatorType: "member", CreatorID: e.owner, Kind: proposalkinds.KeyOrgChange, Payload: []byte(payload),
	})
	var perr *ProposalPayloadError
	if !errors.As(err, &perr) {
		t.Fatalf("stale base_revision: err %v, want a payload error", err)
	}
}

func TestAutonomyChangeAppliesAndReverts(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	sq, err := e.q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: e.squad, WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	from := int(sq.AutonomyLevel)
	to := 2
	if from == to {
		to = 3
	}
	app := e.applyOrgChange(`{"type":"autonomy_change","squad_id":"` + util.UUIDToString(e.squad) +
		`","from":` + strconv.Itoa(from) + `,"to":` + strconv.Itoa(to) + `,"rationale":"The squad has earned more room."}`)
	sq, _ = e.q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: e.squad, WorkspaceID: e.ws})
	if int(sq.AutonomyLevel) != to {
		t.Fatalf("autonomy after apply = %d, want %d", sq.AutonomyLevel, to)
	}
	e.requireProbationWindow(app)

	e.revertOrgChange(app)
	sq, _ = e.q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: e.squad, WorkspaceID: e.ws})
	if int(sq.AutonomyLevel) != from {
		t.Fatalf("autonomy after revert = %d, want %d", sq.AutonomyLevel, from)
	}
}

// A hire creates the agent from its template on the runtime, makes the squad its
// home and writes its instructions as the first revision. Revert archives the
// agent and cancels what it had queued: a hired agent has history, so it is not
// deleted.
func TestHireCreatesTheAgentWithTheSquadAsHome(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	runtime := e.fx.Runtime(t, "hire runtime")
	const template = "critic"
	instructions, ok := companyInstructionText(template)
	if !ok {
		t.Fatalf("template %q is missing from the company data", template)
	}
	app := e.applyOrgChange(`{"type":"hire","squad_id":"` + util.UUIDToString(e.squad) + `","name":"Hired Critic","template":"` + template +
		`","runtime_id":"` + runtime + `","rationale":"The squad needs a second reader."}`)

	hired, err := e.q.GetAgent(ctx, app.SubjectID)
	if err != nil {
		t.Fatalf("hired agent: %v", err)
	}
	if hired.Name != "Hired Critic" || hired.HomeSquadID != e.squad {
		t.Fatalf("hired agent: name %q home %s, want the squad as home", hired.Name, util.UUIDToString(hired.HomeSquadID))
	}
	if hired.Instructions != instructions {
		t.Fatal("the hired agent does not carry the template's instructions")
	}
	if member, err := e.q.IsSquadMember(ctx, db.IsSquadMemberParams{SquadID: e.squad, MemberType: "agent", MemberID: hired.ID}); err != nil || !member {
		t.Fatalf("hired agent is not a member of the squad: %v", err)
	}
	if revs := e.revisions(hired.ID); len(revs) != 1 || revs[0].Source != InstructionSourceProposal {
		t.Fatalf("hire revisions = %+v, want one proposal revision", revs)
	}
	e.requireProbationWindow(app)

	e.fx.Task(t, util.UUIDToString(hired.ID), testutil.Cols{"status": "queued", "runtime_id": runtime})
	e.revertOrgChange(app)
	hired, err = e.q.GetAgent(ctx, app.SubjectID)
	if err != nil {
		t.Fatalf("hired agent after revert: %v", err)
	}
	if !hired.ArchivedAt.Valid {
		t.Fatal("revert must archive the hired agent")
	}
	if n := e.fx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1 AND status = 'queued'`, util.UUIDToString(hired.ID)); n != 0 {
		t.Fatalf("queued tasks after reverting a hire = %d, want 0", n)
	}
}

// A merge moves the source's members and routines into the target, archives the
// source and opens a window on the target. Revert restores the source and puts
// the members and routines back.
func TestMergeSquadsMovesMembersAndRoutinesAndReverts(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	target := mustUUID(t, e.fx.Squad(t, "Merge target", util.UUIDToString(e.reviewer.ID)))
	e.fx.Exec(t, `UPDATE agent SET home_squad_id = $2 WHERE id = $1`, util.UUIDToString(e.subject), util.UUIDToString(e.squad))
	e.fx.SquadMember(t, util.UUIDToString(e.squad), "agent", util.UUIDToString(e.subject))
	ap, err := CreateSquadScheduledAutopilot(ctx, e.q, SquadScheduledAutopilot{
		WorkspaceID: e.ws, SquadID: e.squad, ActorUserID: e.owner, Title: "Weekly check",
		Status: "active", Cron: "0 9 * * 1", Timezone: "UTC", NextRun: e.now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("routine autopilot: %v", err)
	}
	if _, err := e.q.InsertSquadRoutine(ctx, db.InsertSquadRoutineParams{
		AutopilotID: ap.ID, WorkspaceID: e.ws, SquadID: e.squad, Purpose: "Weekly check",
		ExpectedOutput: "none", Required: true, GraceHours: 24,
	}); err != nil {
		t.Fatalf("routine: %v", err)
	}

	app := e.applyOrgChange(`{"type":"merge_squads","from_squad_id":"` + util.UUIDToString(e.squad) +
		`","into_squad_id":"` + util.UUIDToString(target) + `","base_revision":0,"rationale":"Two squads now do one job."}`)
	from, err := e.q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: e.squad, WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if !from.ArchivedAt.Valid {
		t.Fatal("merge must archive the source squad")
	}
	subject, err := e.q.GetAgent(ctx, e.subject)
	if err != nil {
		t.Fatal(err)
	}
	if subject.HomeSquadID != target {
		t.Fatalf("subject home after merge = %s, want the target", util.UUIDToString(subject.HomeSquadID))
	}
	if n := e.fx.Count(t, `SELECT COUNT(*) FROM squad_routine WHERE squad_id = $1`, util.UUIDToString(target)); n != 1 {
		t.Fatalf("routines in the target = %d, want 1", n)
	}
	e.requireProbationWindow(app)

	e.revertOrgChange(app)
	from, err = e.q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: e.squad, WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if from.ArchivedAt.Valid {
		t.Fatal("revert must unarchive the source squad")
	}
	subject, err = e.q.GetAgent(ctx, e.subject)
	if err != nil {
		t.Fatal(err)
	}
	if subject.HomeSquadID != e.squad {
		t.Fatalf("subject home after revert = %s, want the source", util.UUIDToString(subject.HomeSquadID))
	}
	if n := e.fx.Count(t, `SELECT COUNT(*) FROM squad_routine WHERE squad_id = $1`, util.UUIDToString(e.squad)); n != 1 {
		t.Fatalf("routines in the source after revert = %d, want 1", n)
	}
}

// A window with no figures before the change is never judged: it stays open and
// records no outcome, even after it ends (plan 11.15, insufficient data defers).
func TestHireMergeAndRetireWindowsDeferWithoutFigures(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	runtime := e.fx.Runtime(t, "defer runtime")
	hire := e.applyOrgChange(`{"type":"hire","squad_id":"` + util.UUIDToString(e.squad) + `","name":"Deferred Hire","template":"critic","runtime_id":"` +
		runtime + `","rationale":"r"}`)
	leaver := e.fx.Agent(t, "leaver", "")
	retire := e.applyOrgChange(`{"type":"retire","agent_id":"` + leaver + `","rationale":"r"}`)

	checkAt := e.now.Add(OrgChangeProbationWindow + 24*time.Hour)
	probation := &ProbationService{Queries: e.q, Scorecards: e.scorecards(checkAt), Proposals: e.props, Now: func() time.Time { return checkAt }}
	if _, err := probation.CheckDue(ctx); err != nil {
		t.Fatalf("check due: %v", err)
	}
	for _, app := range []db.OrgChangeApplication{hire, retire} {
		got, err := e.q.GetOrgChangeApplicationInWorkspace(ctx, db.GetOrgChangeApplicationInWorkspaceParams{ID: app.ID, WorkspaceID: e.ws})
		if err != nil {
			t.Fatal(err)
		}
		if got.ProbationOutcome.Valid {
			t.Fatalf("%s judged %q with no figures before the change", app.ChangeType, got.ProbationOutcome.String)
		}
	}
}

// A charter change that gives a squad its first charter cannot be reverted: no
// charter row is deleted, and there is no earlier version to write back. The
// revert refuses and the application stays unreverted (a known limit, not a design).
func TestFirstCharterChangeCannotRevert(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	app := e.applyOrgChange(`{"type":"charter_change","squad_id":"` + util.UUIDToString(e.squad) + `","base_revision":0,` +
		`"charter":{"mission":"First mission","responsibilities":[],"outputs":[],"boundaries":"","escalation":"","intake":"","response_hours":48},"rationale":"The squad needs a charter."}`)
	if _, err := e.changes.RevertOrgChange(ctx, e.q, e.ws, app.ID, e.boss()); err == nil {
		t.Fatal("reverting a first charter must refuse")
	}
	got, err := e.q.GetOrgChangeApplicationInWorkspace(ctx, db.GetOrgChangeApplicationInWorkspaceParams{ID: app.ID, WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if got.RevertedAt.Valid {
		t.Fatal("a refused revert must not mark the application reverted")
	}
}
