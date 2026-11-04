package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestPulseShowsAnAtRiskGoalAndAnOverSoftBudget builds the pulse over a workspace
// with one active goal whose confidence is at risk and one company budget past
// its soft line. Both reads come from the goals and budget services, not zeros.
func TestPulseShowsAnAtRiskGoalAndAnOverSoftBudget(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	goals := f.goalService()
	if _, err := f.metricsService().ListMetrics(ctx, f.ws); err != nil {
		t.Fatalf("seed built-ins: %v", err)
	}
	if _, err := f.q.EnsureManualMetric(ctx, db.EnsureManualMetricParams{
		WorkspaceID: f.ws, Key: "mrr", Name: "Revenue", Unit: "USD", Source: MetricSourceManual, Direction: "increase",
	}); err != nil {
		t.Fatalf("create revenue metric: %v", err)
	}

	goal, err := goals.Create(ctx, quarterGoal(f))
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	if _, err := goals.Activate(ctx, f.ws, goal.Goal.ID, f.memberActor()); err != nil {
		t.Fatalf("activate goal: %v", err)
	}
	atRisk := ConfidenceAtRisk
	if _, err := goals.SetConfidenceOverride(ctx, f.ws, goal.Goal.ID, &atRisk, "Two key results are behind the line."); err != nil {
		t.Fatalf("set confidence: %v", err)
	}

	budgets := &BudgetService{Queries: f.q, TxStarter: f.pool}
	actor := GovernanceActor{Type: GovernanceActorMember, ID: util.MustParseUUID(f.owner)}
	b, err := budgets.Create(ctx, f.ws, actor, BudgetInput{
		ScopeType: BudgetScopeCompany, Period: BudgetPeriodMonth, AmountMicros: 1_000_000,
		SoftPercent: 80, Mode: BudgetModeWarn,
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	start, end, err := BudgetPeriodBounds(BudgetPeriodMonth, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// 90 per cent of the limit: past the 80 per cent soft line, under the limit.
	if err := f.q.AddBudgetPeriodSpend(ctx, db.AddBudgetPeriodSpendParams{
		BudgetID: b.ID, WorkspaceID: f.ws,
		PeriodStart: pgtype.Timestamptz{Time: start, Valid: true}, PeriodEnd: pgtype.Timestamptz{Time: end, Valid: true},
		SpentMicros: 900_000, Runs: 1,
	}); err != nil {
		t.Fatalf("record spend: %v", err)
	}

	pulse, err := (&PulseService{Queries: f.q, Goals: goals, Budgets: budgets, Now: func() time.Time { return f.now }}).Build(ctx, f.ws)
	if err != nil {
		t.Fatalf("pulse: %v", err)
	}
	if pulse.GoalsAtRisk != 1 {
		t.Fatalf("goals at risk = %d, want 1", pulse.GoalsAtRisk)
	}
	if pulse.BudgetsAboveSoftLine != 1 {
		t.Fatalf("budgets above the soft line = %d, want 1", pulse.BudgetsAboveSoftLine)
	}
	if pulse.BudgetHolds != 0 {
		t.Fatalf("budget holds = %d, want 0", pulse.BudgetHolds)
	}
}
