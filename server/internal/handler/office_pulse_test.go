package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestOfficePulseShowsAnAtRiskGoalAndAnOverSoftBudget checks the wiring of
// GET /api/office/pulse: the goal and budget figures come from the goals and
// budget services, not zeros (spec 4.8).
func TestOfficePulseShowsAnAtRiskGoalAndAnOverSoftBudget(t *testing.T) {
	ws := newBudgetWorkspace(t)
	ctx := context.Background()
	wsID := util.MustParseUUID(ws.ID)
	owner := util.MustParseUUID(ws.Owner)
	actor := service.GovernanceActor{Type: service.GovernanceActorMember, ID: owner}

	if _, err := service.NewMetricService(testHandler.Queries, testPool).ListMetrics(ctx, wsID); err != nil {
		t.Fatalf("seed built-in metrics: %v", err)
	}
	goals := service.NewGoalService(testHandler.Queries, testPool)
	start := time.Now().UTC().AddDate(0, -1, 0)
	goal, err := goals.Create(ctx, service.GoalWriteParams{
		WorkspaceID: wsID, ScopeType: service.ScopeCompany, Title: "Pulse goal",
		PeriodStart: start, PeriodEnd: start.AddDate(0, 3, 0),
		OwnerType: "member", OwnerID: owner,
		KeyResults: []service.GoalKeyResultInput{{Title: "Runs", MetricKey: "runs.count", StartValue: 0, TargetValue: 10, Weight: 1}},
		Actor:      service.GoalActor{Type: service.GovernanceActorMember, ID: owner},
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	if _, err := goals.Activate(ctx, wsID, goal.Goal.ID, service.GoalActor{Type: service.GovernanceActorMember, ID: owner}); err != nil {
		t.Fatalf("activate goal: %v", err)
	}
	atRisk := service.ConfidenceAtRisk
	if _, err := goals.SetConfidenceOverride(ctx, wsID, goal.Goal.ID, &atRisk, "Two key results are behind the line."); err != nil {
		t.Fatalf("set confidence: %v", err)
	}

	budgets := &service.BudgetService{Queries: testHandler.Queries, TxStarter: testPool}
	b, err := budgets.Create(ctx, wsID, actor, service.BudgetInput{
		ScopeType: service.BudgetScopeCompany, Period: service.BudgetPeriodMonth, AmountMicros: 1_000_000,
		SoftPercent: 80, Mode: service.BudgetModeWarn,
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	periodStart, periodEnd, err := service.BudgetPeriodBounds(service.BudgetPeriodMonth, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// 90 per cent of the limit: past the soft line, under the limit.
	if err := testHandler.Queries.AddBudgetPeriodSpend(ctx, db.AddBudgetPeriodSpendParams{
		BudgetID: b.ID, WorkspaceID: wsID,
		PeriodStart: pgtype.Timestamptz{Time: periodStart, Valid: true}, PeriodEnd: pgtype.Timestamptz{Time: periodEnd, Valid: true},
		SpentMicros: 900_000, Runs: 1,
	}); err != nil {
		t.Fatalf("record spend: %v", err)
	}

	var pulse service.Pulse
	testutil.Call(t, testHandler.GetOfficePulse, companyReq(ws.Owner, ws.ID, http.MethodGet, "/api/office/pulse", nil)).
		Want(http.StatusOK).JSON(&pulse)
	if pulse.GoalsAtRisk != 1 {
		t.Fatalf("goals at risk = %d, want 1", pulse.GoalsAtRisk)
	}
	if pulse.BudgetsAboveSoftLine != 1 {
		t.Fatalf("budgets above the soft line = %d, want 1", pulse.BudgetsAboveSoftLine)
	}
}
