package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// orgEnv is a workspace with a boss, a subject agent, a squad led by a leader,
// and a reviewer agent that is a reviewer member of the squad.
type orgEnv struct {
	t        *testing.T
	fx       *testutil.Fixture
	q        *db.Queries
	ws       pgtype.UUID
	wsID     string
	owner    pgtype.UUID
	subject  pgtype.UUID
	reviewer db.Agent
	squad    pgtype.UUID
	props    *ProposalService
	changes  *OrgChangeService
	now      time.Time
}

func newOrgEnv(t *testing.T) *orgEnv {
	t.Helper()
	pool := sharedTestPool(t)
	slug := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	fx := testutil.New(pool, "", "")
	ownerID := fx.User(t, "Org Owner", "org-owner-"+slug+"@example.test")
	fx = testutil.New(pool, "", ownerID)
	wsID := fx.Workspace(t, "Org WS", "org-"+slug)
	fx = testutil.New(pool, wsID, ownerID)
	fx.Member(t, wsID, ownerID, "owner")
	subjectID := fx.Agent(t, "subject", "", testutil.Cols{"instructions": "v1 instructions"})
	reviewerID := fx.Agent(t, "reviewer", "")
	leaderID := fx.Agent(t, "leader", "")
	squadID := fx.Squad(t, "People & Performance", leaderID)
	fx.SquadMember(t, squadID, "agent", reviewerID, testutil.Cols{"role": "reviewer"})

	q := db.New(pool)
	issues := NewIssueService(q, pool, nil, nil, nil)
	_, props := NewInitiativeSet(issues, nil)
	changes := NewOrgChangeService()
	props.registry().Register(proposalkinds.NewOrgChangeKind(changes))
	env := &orgEnv{now: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	changes.Now = func() time.Time { return env.now }
	reviewer, err := q.GetAgentInWorkspace(context.Background(), db.GetAgentInWorkspaceParams{
		ID: mustUUID(t, reviewerID), WorkspaceID: mustUUID(t, wsID),
	})
	if err != nil {
		t.Fatal(err)
	}
	env.t, env.fx, env.q, env.ws, env.wsID = t, fx, q, mustUUID(t, wsID), wsID
	env.owner, env.subject, env.reviewer = mustUUID(t, ownerID), mustUUID(t, subjectID), reviewer
	env.squad, env.props, env.changes = mustUUID(t, squadID), props, changes
	return env
}

func (e *orgEnv) boss() GovernanceActor {
	return GovernanceActor{Type: GovernanceActorMember, ID: e.owner}
}

// finishRuns inserts completed or failed runs for the subject at a time.
func (e *orgEnv) finishRuns(agent pgtype.UUID, at time.Time, completed, failed int, failureReason string) {
	e.t.Helper()
	stamp := at.UTC().Format("2006-01-02 15:04:05") + "+00"
	for i := 0; i < completed; i++ {
		e.fx.Task(e.t, util.UUIDToString(agent), testutil.Cols{
			"status": "completed", "completed_at": testutil.Raw("'" + stamp + "'::timestamptz"),
		})
	}
	for i := 0; i < failed; i++ {
		e.fx.Task(e.t, util.UUIDToString(agent), testutil.Cols{
			"status": "failed", "failure_reason": failureReason,
			"completed_at": testutil.Raw("'" + stamp + "'::timestamptz"),
		})
	}
}

func (e *orgEnv) scorecards(at time.Time) *ScorecardService {
	return &ScorecardService{Queries: e.q, Now: func() time.Time { return at }}
}

func (e *orgEnv) file(kind, payload string) ProposalCreateResult {
	e.t.Helper()
	res, err := e.props.Create(context.Background(), ProposalCreateParams{
		WorkspaceID: e.ws, SquadID: e.squad,
		Content: ProposalContent{
			Title: "Change the subject", Summary: "A change.", Evidence: []string{"https://example.test/e"},
			Recommendation: "Do it.", Options: []string{"Do it", "Wait"}, CostRisk: "Low.", Ask: "Approve?",
		},
		CreatorType: "member", CreatorID: e.owner, Kind: kind, Payload: []byte(payload),
	})
	if err != nil {
		e.t.Fatalf("file %s: %v", kind, err)
	}
	return res
}

func (e *orgEnv) approve(id pgtype.UUID) {
	e.t.Helper()
	if _, err := e.props.Decide(context.Background(), ProposalDecideParams{
		WorkspaceID: e.ws, ProposalID: id, DeciderID: e.owner, Decision: ProposalDecisionApprove,
	}); err != nil {
		e.t.Fatalf("approve: %v", err)
	}
}

func (e *orgEnv) instructions(agent pgtype.UUID) string {
	e.t.Helper()
	a, err := e.q.GetAgent(context.Background(), agent)
	if err != nil {
		e.t.Fatal(err)
	}
	return a.Instructions
}

func (e *orgEnv) revisions(agent pgtype.UUID) []db.AgentInstructionRevision {
	e.t.Helper()
	rows, err := e.q.ListAgentInstructionRevisions(context.Background(), db.ListAgentInstructionRevisionsParams{
		WorkspaceID: e.ws, AgentID: agent, RowLimit: 50,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

// TestInstructionRevisionHookRecordsEveryChange covers the creation and edit
// paths of the hook and the restore.
func TestInstructionRevisionHookRecordsEveryChange(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	human := InstructionChange{Source: InstructionSourceHuman, Actor: e.boss()}

	// Changing to the same text records nothing.
	rev, err := ChangeAgentInstructions(ctx, e.q, e.subject, "v1 instructions", human)
	if err != nil || rev != nil {
		t.Fatalf("unchanged text: rev %v err %v, want nil", rev, err)
	}
	if n := len(e.revisions(e.subject)); n != 0 {
		t.Fatalf("revisions after a no-op = %d, want 0", n)
	}

	// An edit records the new text as the next revision.
	if _, rev, err := SetAgentInstructions(ctx, e.q, e.subject, "v2 instructions", human); err != nil || rev == nil || rev.Revision != 1 {
		t.Fatalf("edit: rev %+v err %v", rev, err)
	}
	// A second path writing the same text records nothing new.
	if _, rev, err := SetAgentInstructions(ctx, e.q, e.subject, "v2 instructions", human); err != nil || rev != nil {
		t.Fatalf("repeat write: rev %+v err %v", rev, err)
	}

	// Revision 1 holds v2, so after a further edit to v3, restoring revision 1
	// writes a rollback revision (number 3) with v2 back in place.
	if _, _, err := SetAgentInstructions(ctx, e.q, e.subject, "v3 instructions", human); err != nil {
		t.Fatal(err)
	}
	restored, rb, err := RestoreAgentInstructionRevision(ctx, e.q, e.ws, e.subject, 1, e.boss())
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Instructions != "v2 instructions" || rb.Revision != 3 || rb.Source != InstructionSourceRollback {
		t.Fatalf("restore wrote the wrong thing: %+v", rb)
	}
	if _, _, err := RestoreAgentInstructionRevision(ctx, e.q, e.ws, e.subject, 1, e.boss()); !errors.Is(err, ErrInstructionsUnchanged) {
		t.Fatalf("restoring the current text: err %v, want ErrInstructionsUnchanged", err)
	}
}

// TestInstructionChangeProposalAppliesAndReverts covers the instruction_change
// kind: the approval writes a proposal revision, and reverting restores the text.
func TestInstructionChangeProposalAppliesAndReverts(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	payload := `{"type":"instruction_change","agent_id":"` + util.UUIDToString(e.subject) + `","instructions":"v2 instructions","rationale":"The runs failed on a missing step."}`
	res := e.file(proposalkinds.KeyOrgChange, payload)
	e.approve(res.Proposal.ID)

	if got := e.instructions(e.subject); got != "v2 instructions" {
		t.Fatalf("instructions after approval = %q", got)
	}
	revs := e.revisions(e.subject)
	if len(revs) != 1 || revs[0].Source != InstructionSourceProposal || !revs[0].ProposalID.Valid {
		t.Fatalf("approval did not record a proposal revision: %+v", revs)
	}

	app, err := e.q.GetOrgChangeApplicationByProposal(ctx, db.GetOrgChangeApplicationByProposalParams{
		WorkspaceID: e.ws, ProposalID: res.Proposal.ID,
	})
	if err != nil {
		t.Fatalf("application row: %v", err)
	}
	if !app.ProbationEndsAt.Valid {
		t.Fatal("an instruction change must open a probation window")
	}
	if _, err := e.changes.RevertOrgChange(ctx, e.q, e.ws, app.ID, e.boss()); err != nil {
		t.Fatalf("revert: %v", err)
	}
	if got := e.instructions(e.subject); got != "v1 instructions" {
		t.Fatalf("instructions after revert = %q, want the earlier text", got)
	}
	if _, err := e.changes.RevertOrgChange(ctx, e.q, e.ws, app.ID, e.boss()); !errors.Is(err, ErrOrgChangeAlreadyReverted) {
		t.Fatalf("second revert: %v, want ErrOrgChangeAlreadyReverted", err)
	}
}

// TestOrgChangePayloadIsRefusedWhenUnknown checks that a payload with a field
// the type does not take is refused at filing.
func TestOrgChangePayloadIsRefusedWhenUnknown(t *testing.T) {
	e := newOrgEnv(t)
	payload := `{"type":"instruction_change","agent_id":"` + util.UUIDToString(e.subject) + `","instructions":"x","rationale":"r","bogus":1}`
	_, err := e.props.Create(context.Background(), ProposalCreateParams{
		WorkspaceID: e.ws, SquadID: e.squad,
		Content:     ProposalContent{Title: "t", Summary: "s", Evidence: []string{"e"}, Recommendation: "r", Options: []string{"a"}, CostRisk: "c", Ask: "a"},
		CreatorType: "member", CreatorID: e.owner, Kind: proposalkinds.KeyOrgChange, Payload: []byte(payload),
	})
	var perr *ProposalPayloadError
	if !errors.As(err, &perr) {
		t.Fatalf("unknown field: err %v, want a payload error", err)
	}
}

// TestScorecardDoesNotChargeRuntimeOffline checks that failures the runtime caused
// stay out of the agent's success rate and are reported as uncharged.
func TestScorecardDoesNotChargeRuntimeOffline(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	e.finishRuns(e.subject, at, 6, 2, "agent_error")
	e.finishRuns(e.subject, at, 0, 5, "runtime_offline")

	sc, err := e.scorecards(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)).Compute(ctx, e.q, e.ws,
		ScorecardScope{SubjectType: ScorecardSubjectAgent, SubjectID: e.subject, AgentID: e.subject}, ScorecardPeriodWeek)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	doc, err := DecodeScorecardDocument(sc.Metrics)
	if err != nil {
		t.Fatal(err)
	}
	rate := doc.Metrics[MetricSuccessRate]
	if rate.Value == nil || *rate.Value != 0.75 {
		t.Fatalf("success rate = %+v, want 0.75 (runtime failures excluded)", rate)
	}
	if doc.Metrics[MetricRunsUncharged].Sample != 5 {
		t.Fatalf("uncharged runs = %d, want 5", doc.Metrics[MetricRunsUncharged].Sample)
	}
	if doc.FailuresByCause["runtime_offline"] != 5 {
		t.Fatalf("failures by cause = %v", doc.FailuresByCause)
	}
}

// TestReviewPublishCitesScorecardAndFilesRecommendations covers the reviewer's
// path: insufficient scorecards are refused, cited figures come from the stored
// scorecard, recommendations become proposals, and a second review is refused.
func TestReviewPublishCitesScorecardAndFilesRecommendations(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	e.finishRuns(e.subject, at, 6, 2, "agent_error")
	svc := &ReviewService{Queries: e.q, Proposals: e.props, OrgChanges: e.changes, Now: func() time.Time { return e.now }}
	// October is the last completed month on 3 November.
	monthScope := e.scorecards(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))
	if _, err := monthScope.Compute(ctx, e.q, e.ws, ScorecardScope{SubjectType: ScorecardSubjectAgent, SubjectID: e.subject, AgentID: e.subject}, ScorecardPeriodMonth); err != nil {
		t.Fatal(err)
	}

	subjectRaw := util.UUIDToString(e.subject)
	req := ReviewRequest{
		SubjectType: "agent", SubjectID: subjectRaw, Period: "month", PeriodStart: "2026-10-01",
		Rating: ReviewRatingBelow, Narrative: "Two runs failed on the agent's own errors.",
		CitedMetrics: []string{MetricSuccessRate},
		Recommendations: []ReviewRecommendationInput{
			{Type: OrgChangeInstruction, Rationale: "Add the missing step.", Payload: []byte(`{"agent_id":"` + subjectRaw + `","instructions":"v2 instructions","rationale":"Add the missing step."}`)},
			{Type: ReviewRecommendObserve, Rationale: "Watch the next month."},
		},
	}
	res, err := svc.Publish(ctx, e.ws, e.reviewer, e.squad, req)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	stored, err := e.q.GetReviewInWorkspace(ctx, db.GetReviewInWorkspaceParams{ID: mustUUID(t, res.ReviewID), WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored.Narrative, "- success_rate: 75% (sample 8)") {
		t.Fatalf("narrative does not carry the stored figure:\n%s", stored.Narrative)
	}
	if len(res.Recommendations) != 2 || res.Recommendations[0].ProposalID == "" || res.Recommendations[1].ProposalID != "" {
		t.Fatalf("recommendations: %+v", res.Recommendations)
	}
	if _, err := svc.Publish(ctx, e.ws, e.reviewer, e.squad, req); !errors.Is(err, ErrReviewExists) {
		t.Fatalf("second review: %v, want ErrReviewExists", err)
	}

	// A subject with too few runs is never reviewed.
	thin := e.fx.Agent(t, "thin", "")
	thinID := mustUUID(t, thin)
	e.finishRuns(thinID, at, 2, 0, "")
	if _, err := monthScope.Compute(ctx, e.q, e.ws, ScorecardScope{SubjectType: ScorecardSubjectAgent, SubjectID: thinID, AgentID: thinID}, ScorecardPeriodMonth); err != nil {
		t.Fatal(err)
	}
	thinReq := req
	thinReq.SubjectID = thin
	thinReq.Recommendations = nil
	if _, err := svc.Publish(ctx, e.ws, e.reviewer, e.squad, thinReq); !errors.Is(err, ErrReviewInsufficient) {
		t.Fatalf("insufficient subject: %v, want ErrReviewInsufficient", err)
	}
}

// TestProbationJudgesTheWindow covers the three outcomes and the defer, with the
// change applied on 20 October and its window ending on 3 November.
func TestProbationJudgesTheWindow(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name        string
		beforeOK    int
		beforeFail  int
		afterOK     int
		afterFail   int
		noAfter     bool
		wantOutcome string
	}{
		{name: "worse", beforeOK: 6, afterFail: 6, wantOutcome: ProbationWorse},
		{name: "improved", beforeOK: 3, beforeFail: 3, afterOK: 6, wantOutcome: ProbationImproved},
		{name: "neutral", beforeOK: 6, afterOK: 6, wantOutcome: ProbationNeutral},
		{name: "defer without figures", beforeOK: 6, noAfter: true, wantOutcome: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newOrgEnv(t)
			e.finishRuns(e.subject, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), c.beforeOK, c.beforeFail, "agent_error")
			before := e.scorecards(e.now)
			if _, err := before.Compute(ctx, e.q, e.ws, ScorecardScope{SubjectType: ScorecardSubjectAgent, SubjectID: e.subject, AgentID: e.subject}, ScorecardPeriodWeek); err != nil {
				t.Fatal(err)
			}
			res := e.file(proposalkinds.KeyOrgChange, `{"type":"instruction_change","agent_id":"`+util.UUIDToString(e.subject)+`","instructions":"v2 instructions","rationale":"r"}`)
			e.approve(res.Proposal.ID)
			app, err := e.q.GetOrgChangeApplicationByProposal(ctx, db.GetOrgChangeApplicationByProposalParams{WorkspaceID: e.ws, ProposalID: res.Proposal.ID})
			if err != nil {
				t.Fatal(err)
			}
			if !app.BeforeScorecardID.Valid {
				t.Fatal("the application must store the before scorecard")
			}
			if !c.noAfter {
				e.finishRuns(e.subject, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), c.afterOK, c.afterFail, "agent_error")
			}
			checkAt := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
			if _, err := e.scorecards(checkAt).Compute(ctx, e.q, e.ws, ScorecardScope{SubjectType: ScorecardSubjectAgent, SubjectID: e.subject, AgentID: e.subject}, ScorecardPeriodWeek); err != nil {
				t.Fatal(err)
			}
			probation := &ProbationService{Queries: e.q, Scorecards: e.scorecards(checkAt), Proposals: e.props, Now: func() time.Time { return checkAt }}
			if _, err := probation.CheckDue(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := e.q.GetOrgChangeApplicationInWorkspace(ctx, db.GetOrgChangeApplicationInWorkspaceParams{ID: app.ID, WorkspaceID: e.ws})
			if err != nil {
				t.Fatal(err)
			}
			if got.ProbationOutcome.String != c.wantOutcome {
				t.Fatalf("outcome %q, want %q", got.ProbationOutcome.String, c.wantOutcome)
			}
			if c.wantOutcome == ProbationWorse && !got.RollbackProposalID.Valid {
				t.Fatal("a worse verdict must file a rollback proposal when a reviewer exists")
			}
		})
	}
}

// TestMoveAndRetireApplyAndRevert covers two more change types: a move sets the
// agent's home squad and membership, and a retire archives the agent and cancels
// what it had queued. Each reverts to the state before.
func TestMoveAndRetireApplyAndRevert(t *testing.T) {
	e := newOrgEnv(t)
	ctx := context.Background()
	fromSquad := e.squad
	toSquadID := e.fx.Squad(t, "Destination", util.UUIDToString(e.reviewer.ID))
	toSquad := mustUUID(t, toSquadID)
	fromID := util.UUIDToString(fromSquad)
	e.fx.Exec(t, `UPDATE agent SET home_squad_id = $2 WHERE id = $1`, util.UUIDToString(e.subject), fromID)
	e.fx.SquadMember(t, fromID, "agent", util.UUIDToString(e.subject))

	move := e.file(proposalkinds.KeyOrgChange, `{"type":"move","agent_id":"`+util.UUIDToString(e.subject)+`","to_squad_id":"`+toSquadID+`","rationale":"Its work now sits with the destination squad."}`)
	e.approve(move.Proposal.ID)
	agent, err := e.q.GetAgent(ctx, e.subject)
	if err != nil {
		t.Fatal(err)
	}
	if agent.HomeSquadID != toSquad {
		t.Fatalf("home squad after move = %s, want the destination", util.UUIDToString(agent.HomeSquadID))
	}
	moveApp, err := e.q.GetOrgChangeApplicationByProposal(ctx, db.GetOrgChangeApplicationByProposalParams{WorkspaceID: e.ws, ProposalID: move.Proposal.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.changes.RevertOrgChange(ctx, e.q, e.ws, moveApp.ID, e.boss()); err != nil {
		t.Fatalf("revert move: %v", err)
	}
	agent, err = e.q.GetAgent(ctx, e.subject)
	if err != nil {
		t.Fatal(err)
	}
	if agent.HomeSquadID != fromSquad {
		t.Fatalf("home squad after revert = %s, want the original", util.UUIDToString(agent.HomeSquadID))
	}

	// Retire: the agent is archived and its queued task cancelled; revert restores it.
	runtimeID := e.fx.Runtime(t, "retire-runtime")
	e.fx.Task(t, util.UUIDToString(e.subject), testutil.Cols{"status": "queued", "runtime_id": runtimeID})
	retire := e.file(proposalkinds.KeyOrgChange, `{"type":"retire","agent_id":"`+util.UUIDToString(e.subject)+`","rationale":"No longer needed."}`)
	e.approve(retire.Proposal.ID)
	agent, err = e.q.GetAgent(ctx, e.subject)
	if err != nil {
		t.Fatal(err)
	}
	if !agent.ArchivedAt.Valid {
		t.Fatal("retire must archive the agent")
	}
	if n := e.fx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE agent_id = $1 AND status = 'queued'`, util.UUIDToString(e.subject)); n != 0 {
		t.Fatalf("queued tasks after retire = %d, want 0", n)
	}
	retireApp, err := e.q.GetOrgChangeApplicationByProposal(ctx, db.GetOrgChangeApplicationByProposalParams{WorkspaceID: e.ws, ProposalID: retire.Proposal.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.changes.RevertOrgChange(ctx, e.q, e.ws, retireApp.ID, e.boss()); err != nil {
		t.Fatalf("revert retire: %v", err)
	}
	agent, err = e.q.GetAgent(ctx, e.subject)
	if err != nil {
		t.Fatal(err)
	}
	if agent.ArchivedAt.Valid {
		t.Fatal("revert must restore the agent")
	}
}
