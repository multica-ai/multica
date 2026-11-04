package service

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// TestBudgetHoldTriggerReadsTheHoldsOfTheBudgetsScope checks spec 4.7: a budget
// hold recorded in the budget_hold table becomes a budget_hold candidate for the
// scope the budget covers, which is the squad whose leader takes the retrospective.
func TestBudgetHoldTriggerReadsTheHoldsOfTheBudgetsScope(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	squadID := util.MustParseUUID(f.squad)
	actor := GovernanceActor{Type: GovernanceActorMember, ID: util.MustParseUUID(f.owner)}

	b, err := (&BudgetService{Queries: f.q}).Create(ctx, f.ws, actor, BudgetInput{
		ScopeType: BudgetScopeSquad, ScopeID: squadID, Period: BudgetPeriodMonth,
		AmountMicros: 100_000_000, SoftPercent: 80, Mode: BudgetModeHold,
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	taskID := f.Task(t, f.agent, testutil.Cols{"squad_id": f.squad, "status": "completed", "completed_at": f.now.Add(-time.Hour)})
	if _, err := f.q.InsertBudgetHoldIfNone(ctx, db.InsertBudgetHoldIfNoneParams{
		ID: dbid.NewV7(), WorkspaceID: f.ws, BudgetID: b.ID, TaskID: util.MustParseUUID(taskID),
		AgentID: util.MustParseUUID(f.agent), SquadID: squadID, RunClass: "routine",
	}); err != nil {
		t.Fatalf("record hold: %v", err)
	}

	cands, err := (&RetroService{Queries: f.q}).budgetHoldCandidates(ctx, f.ws, f.now)
	if err != nil {
		t.Fatalf("budget hold candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1 for the held squad", len(cands))
	}
	c := cands[0]
	if c.Trigger != RetroTriggerBudgetHold || c.SubjectType != RetroSubjectSquad {
		t.Fatalf("candidate %s on %s, want budget_hold on a squad", c.Trigger, c.SubjectType)
	}
	if util.UUIDToString(c.LeaderSquadID) != f.squad {
		t.Fatalf("leader squad = %s, want the held squad %s", util.UUIDToString(c.LeaderSquadID), f.squad)
	}
}
