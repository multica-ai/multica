package service

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// TestFloorDigestCountsOnlyTheFloorAcrossTwoFloors checks the floor-scoped digest
// (spec 4.8) on a company with two floors and a squad on no floor. Each floor's
// digest counts its own squads, initiatives, pending decisions, budgets and goals,
// and its runs; the company digest counts all of them.
func TestFloorDigestCountsOnlyTheFloorAcrossTwoFloors(t *testing.T) {
	f, _ := officeWorkspace(t)
	ctx := context.Background()
	ws := util.MustParseUUID(f.WorkspaceID)
	actor := GovernanceActor{Type: GovernanceActorMember, ID: util.MustParseUUID(f.UserID)}
	runtimeID, leader := ownerAndLeader(t, f, "Floor Digest Lead")

	floorA := f.Insert(t, "office_floor", testutil.Cols{"workspace_id": f.WorkspaceID, "name": "Floor A", "position": 0})
	floorB := f.Insert(t, "office_floor", testutil.Cols{"workspace_id": f.WorkspaceID, "name": "Floor B", "position": 1})
	squadA1 := f.Squad(t, "A1", leader)
	squadA2 := f.Squad(t, "A2", leader)
	squadB1 := f.Squad(t, "B1", leader)
	loose := f.Squad(t, "Loose", leader)
	f.Exec(t, `UPDATE squad SET floor_id = $1, office_slot = 0 WHERE id = $2`, floorA, squadA1)
	f.Exec(t, `UPDATE squad SET floor_id = $1, office_slot = 1 WHERE id = $2`, floorA, squadA2)
	f.Exec(t, `UPDATE squad SET floor_id = $1, office_slot = 0 WHERE id = $2`, floorB, squadB1)

	// Two pending decisions on A1, one on B1, none on the loose squad.
	for _, sq := range []string{squadA1, squadA1, squadB1} {
		f.Insert(t, "proposal", testutil.Cols{
			"workspace_id": f.WorkspaceID, "squad_id": sq, "issue_id": f.Issue(t, "decision"), "state": "pending",
			"created_by_type": "agent", "created_by_id": leader, "kind": "decision",
			"payload": `{}`, "expires_at": time.Now().AddDate(0, 0, 14),
		})
	}

	// One initiative per owning squad; the loose squad's initiative is on no floor.
	pipeline := f.Insert(t, "pipeline", testutil.Cols{
		"workspace_id": f.WorkspaceID, "name": "Floor pipeline", "workflow_id": newUUIDText(),
		"project_id": newUUIDText(), "stages": string(pipelineStagesJSON(t)), "is_default": true,
	})
	for i, sq := range []string{squadA1, squadB1, loose} {
		f.Insert(t, "initiative", testutil.Cols{
			"workspace_id": f.WorkspaceID, "pipeline_id": pipeline, "seq": i + 1, "title": "Initiative " + sq[:8],
			"thesis": "t", "stage_key": "define", "owner_squad_id": sq, "status": "active", "issue_id": f.Issue(t, "ini"),
		})
	}

	// Budgets: a company budget, one on floor A, one on squad A1, one on floor B.
	budgets := &BudgetService{Queries: f.q, TxStarter: f.Pool}
	for _, in := range []BudgetInput{
		{ScopeType: BudgetScopeCompany, Period: BudgetPeriodMonth, AmountMicros: 1_000_000_000, SoftPercent: 80, Mode: BudgetModeWarn},
		{ScopeType: BudgetScopeFloor, ScopeID: util.MustParseUUID(floorA), Period: BudgetPeriodMonth, AmountMicros: 100_000_000, SoftPercent: 80, Mode: BudgetModeWarn},
		{ScopeType: BudgetScopeSquad, ScopeID: util.MustParseUUID(squadA1), Period: BudgetPeriodWeek, AmountMicros: 10_000_000, SoftPercent: 80, Mode: BudgetModeWarn},
		{ScopeType: BudgetScopeFloor, ScopeID: util.MustParseUUID(floorB), Period: BudgetPeriodMonth, AmountMicros: 100_000_000, SoftPercent: 80, Mode: BudgetModeWarn},
	} {
		if _, err := budgets.Create(ctx, ws, actor, in); err != nil {
			t.Fatalf("budget %s: %v", in.ScopeType, err)
		}
	}

	// Goals: one company goal, one per floor. Each is active.
	goalIn := func(scopeType string, scopeID any, title string) {
		t.Helper()
		f.Insert(t, "goal", testutil.Cols{
			"workspace_id": f.WorkspaceID, "scope_type": scopeType, "scope_id": scopeID, "title": title,
			"period_start": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "period_end": time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			"owner_type": "member", "owner_id": actor.ID, "status": "active",
			"created_by_type": "member", "created_by_id": actor.ID,
		})
	}
	goalIn("company", nil, "Company goal")
	goalIn("floor", floorA, "Floor A goal")
	goalIn("floor", floorB, "Floor B goal")

	// Runs: a failed run for A1 and a completed run for B1, by the leader.
	now := time.Now()
	f.Task(t, leader, testutil.Cols{"squad_id": squadA1, "status": "failed", "completed_at": now.Add(-time.Hour), "failure_reason": "agent_error"})
	f.Task(t, leader, testutil.Cols{"squad_id": squadB1, "status": "completed", "completed_at": now.Add(-time.Hour)})
	_ = runtimeID

	since := now.Add(-24 * time.Hour)
	floorDigest := func(floor string) Digest {
		t.Helper()
		svc := NewDigestService(f.q)
		svc.FloorID = util.MustParseUUID(floor)
		d, err := svc.Compute(ctx, ws, since, "TST")
		if err != nil {
			t.Fatalf("floor digest: %v", err)
		}
		return d
	}

	a := floorDigest(floorA)
	if a.Scope.Type != "floor" || a.Scope.Counts.Squads != 2 || a.Scope.Counts.Initiatives != 1 ||
		a.Scope.Counts.PendingDecisions != 2 || a.Scope.Counts.Budgets != 2 || a.Scope.Counts.Goals != 1 {
		t.Fatalf("floor A counts: %+v", a.Scope.Counts)
	}
	if len(a.Pipeline) != 1 || len(a.Goals) != 1 || a.Goals[0].Title != "Floor A goal" {
		t.Fatalf("floor A lists its own initiative and goal only: pipeline %d, goals %+v", len(a.Pipeline), a.Goals)
	}
	if a.Workspace.Runs != 1 || a.Workspace.FailedRuns != 1 || len(a.Workspace.AgentsFailed.Items) != 1 {
		t.Fatalf("floor A counts its own runs: runs %d failed %d agents %d", a.Workspace.Runs, a.Workspace.FailedRuns, len(a.Workspace.AgentsFailed.Items))
	}
	if a.Workspace.UnseatedSquads.Count != 0 {
		t.Fatalf("a floor has no unseated squads, got %d", a.Workspace.UnseatedSquads.Count)
	}
	if a.Budget.HoldsInForce != 0 {
		t.Fatalf("floor A holds: %d", a.Budget.HoldsInForce)
	}

	b := floorDigest(floorB)
	if b.Scope.Counts.Squads != 1 || b.Scope.Counts.Initiatives != 1 || b.Scope.Counts.PendingDecisions != 1 ||
		b.Scope.Counts.Budgets != 1 || b.Scope.Counts.Goals != 1 {
		t.Fatalf("floor B counts: %+v", b.Scope.Counts)
	}
	if b.Workspace.Runs != 1 || b.Workspace.FailedRuns != 0 || len(b.Workspace.AgentsFailed.Items) != 0 {
		t.Fatalf("floor B counts its own runs only: runs %d failed %d", b.Workspace.Runs, b.Workspace.FailedRuns)
	}

	company, err := NewDigestService(f.q).Compute(ctx, ws, since, "TST")
	if err != nil {
		t.Fatalf("company digest: %v", err)
	}
	if company.Scope.Type != "company" || company.Scope.Counts.Squads != 4 || company.Scope.Counts.Initiatives != 3 ||
		company.Scope.Counts.PendingDecisions != 3 || company.Scope.Counts.Budgets != 4 || company.Scope.Counts.Goals != 3 {
		t.Fatalf("company counts: %+v", company.Scope.Counts)
	}
	if company.Workspace.Runs != 2 {
		t.Fatalf("the company digest counts every run: %d", company.Workspace.Runs)
	}
}
