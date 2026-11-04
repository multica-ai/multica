package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// squadRuns records finished runs of one agent for a squad, so the squad's
// scorecard counts them. The completed and failed runs are stamped at one time.
func (e *orgEnv) squadRuns(agent, squad string, at time.Time, completed, failed int, reason string) {
	e.t.Helper()
	stamp := at.UTC().Format("2006-01-02 15:04:05") + "+00"
	for i := 0; i < completed; i++ {
		e.fx.Task(e.t, agent, testutil.Cols{
			"status": "completed", "squad_id": squad,
			"completed_at": testutil.Raw("'" + stamp + "'::timestamptz"),
		})
	}
	for i := 0; i < failed; i++ {
		e.fx.Task(e.t, agent, testutil.Cols{
			"status": "failed", "failure_reason": reason, "squad_id": squad,
			"completed_at": testutil.Raw("'" + stamp + "'::timestamptz"),
		})
	}
}

// TestProbationWorseForHireAndRetire checks the rule for the two types judged on a
// squad (probation.go): a hire compares the joining squad's scorecard before with
// the new agent's after, and a retire compares the squad's scorecard before with
// its after. Each worse outcome is stored and files its rollback: a worse hire
// proposes retiring the agent it created, and a worse retire proposes unretiring.
func TestProbationWorseForHireAndRetire(t *testing.T) {
	ctx := context.Background()
	checkAt := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

	t.Run("hire", func(t *testing.T) {
		e := newOrgEnv(t)
		runtime := e.fx.Runtime(t, "probation hire runtime")
		squadID := util.UUIDToString(e.squad)
		e.squadRuns(util.UUIDToString(e.subject), squadID, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), 6, 0, "")
		if _, err := e.scorecards(e.now).Compute(ctx, e.q, e.ws, ScorecardScope{SubjectType: ScorecardSubjectSquad, SubjectID: e.squad, SquadID: e.squad}, ScorecardPeriodWeek); err != nil {
			t.Fatal(err)
		}
		app := e.applyOrgChange(`{"type":"hire","squad_id":"` + squadID + `","name":"Probation Hire","template":"critic","runtime_id":"` + runtime + `","rationale":"r"}`)
		if !app.BeforeScorecardID.Valid {
			t.Fatal("a hire must store the joining squad's scorecard as its before figure")
		}
		newAgent := util.UUIDToString(app.SubjectID)
		e.squadRuns(newAgent, squadID, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), 0, 6, "agent_error")
		if _, err := e.scorecards(checkAt).Compute(ctx, e.q, e.ws, ScorecardScope{SubjectType: ScorecardSubjectAgent, SubjectID: app.SubjectID, AgentID: app.SubjectID}, ScorecardPeriodWeek); err != nil {
			t.Fatal(err)
		}
		e.judgeAndExpectWorse(t, app.ID, checkAt)
	})

	t.Run("retire an agent", func(t *testing.T) {
		e := newOrgEnv(t)
		squadID := util.UUIDToString(e.squad)
		e.fx.Exec(t, `UPDATE agent SET home_squad_id = $2 WHERE id = $1`, util.UUIDToString(e.subject), squadID)
		e.squadRuns(util.UUIDToString(e.reviewer.ID), squadID, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), 6, 0, "")
		if _, err := e.scorecards(e.now).Compute(ctx, e.q, e.ws, ScorecardScope{SubjectType: ScorecardSubjectSquad, SubjectID: e.squad, SquadID: e.squad}, ScorecardPeriodWeek); err != nil {
			t.Fatal(err)
		}
		app := e.applyOrgChange(`{"type":"retire","agent_id":"` + util.UUIDToString(e.subject) + `","rationale":"r"}`)
		if !app.BeforeScorecardID.Valid {
			t.Fatal("a retire must store its squad's scorecard as the before figure")
		}
		e.squadRuns(util.UUIDToString(e.reviewer.ID), squadID, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), 0, 6, "agent_error")
		if _, err := e.scorecards(checkAt).Compute(ctx, e.q, e.ws, ScorecardScope{SubjectType: ScorecardSubjectSquad, SubjectID: e.squad, SquadID: e.squad}, ScorecardPeriodWeek); err != nil {
			t.Fatal(err)
		}
		e.judgeAndExpectWorse(t, app.ID, checkAt)
	})
}

// judgeAndExpectWorse runs the probation check at the given time and asserts the
// application's outcome is worse and that a rollback proposal was filed for it.
func (e *orgEnv) judgeAndExpectWorse(t *testing.T, appID pgtype.UUID, checkAt time.Time) {
	t.Helper()
	probation := &ProbationService{Queries: e.q, Scorecards: e.scorecards(checkAt), Proposals: e.props, Now: func() time.Time { return checkAt }}
	if _, err := probation.CheckDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := e.q.GetOrgChangeApplicationInWorkspace(context.Background(), db.GetOrgChangeApplicationInWorkspaceParams{ID: appID, WorkspaceID: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProbationOutcome.String != ProbationWorse {
		t.Fatalf("outcome %q, want %q", got.ProbationOutcome.String, ProbationWorse)
	}
	if !got.RollbackProposalID.Valid {
		t.Fatal("a worse outcome must file a rollback proposal")
	}
}
