package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCompanyRoundTripReproducesStructureInAnEmptyWorkspace exports a company with
// every structural section and imports it into an empty workspace. It checks that
// a dry run writes nothing, that the apply creates everything without a block,
// that a second apply changes nothing, and that the copy exports the same
// structure (spec 4.9, plan 11.18). Routines come back paused; the comparison
// ignores their status for that reason.
func TestCompanyRoundTripReproducesStructureInAnEmptyWorkspace(t *testing.T) {
	src, _ := officeWorkspace(t)
	ctx := context.Background()
	srcWS := util.MustParseUUID(src.WorkspaceID)
	srcActor := GovernanceActor{Type: GovernanceActorMember, ID: util.MustParseUUID(src.UserID)}
	runtimeID, leader := ownerAndLeader(t, src, "RT Lead")
	worker := src.Agent(t, "RT Worker", runtimeID)
	squadID := src.Squad(t, "RT Squad", leader)
	squadUUID := util.MustParseUUID(squadID)
	src.SquadMember(t, squadID, "agent", leader, testutil.Cols{"role": "leader"})
	src.SquadMember(t, squadID, "agent", worker, testutil.Cols{"role": "member"})
	src.Exec(t, `UPDATE agent SET home_squad_id = $1 WHERE id = ANY($2::uuid[])`, squadID, []string{leader, worker})
	floorID := src.Insert(t, "office_floor", testutil.Cols{"workspace_id": src.WorkspaceID, "name": "RT Floor", "position": 0})
	src.Exec(t, `UPDATE squad SET floor_id = $1, office_slot = 0 WHERE id = $2`, floorID, squadID)
	src.Exec(t, `INSERT INTO squad_charter (squad_id, workspace_id, mission, responsibilities, outputs, response_hours, operating_review_enabled, revision)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, 24, true, 1)`,
		squadID, src.WorkspaceID, "Round trip the structure", `["keep the record"]`, `[]`)
	// The worker carries a starter template's instructions unchanged.
	template := "critic"
	templateText, ok := companyInstructionText(template)
	if !ok {
		t.Fatalf("template %q is missing from the company data", template)
	}
	src.Exec(t, `UPDATE agent SET instructions = $1 WHERE id = $2`, templateText, worker)

	if _, err := ApplyAutonomyChange(ctx, src.q, srcWS, squadUUID, 2, srcActor, "Round trip setup."); err != nil {
		t.Fatalf("autonomy: %v", err)
	}
	ap, err := CreateSquadScheduledAutopilot(ctx, src.q, SquadScheduledAutopilot{
		WorkspaceID: srcWS, SquadID: squadUUID, ActorUserID: srcActor.ID, Title: "RT Weekly review",
		Status: "active", Cron: "0 9 * * 1", Timezone: "UTC", NextRun: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("routine autopilot: %v", err)
	}
	if _, err := src.q.InsertSquadRoutine(ctx, db.InsertSquadRoutineParams{
		AutopilotID: ap.ID, WorkspaceID: srcWS, SquadID: squadUUID, Purpose: "Review the squad weekly",
		ExpectedOutput: "report", Required: true, GraceHours: 24,
	}); err != nil {
		t.Fatalf("routine: %v", err)
	}
	if _, err := SeedKnowledgeKindIn(ctx, src.q, db.SeedKnowledgeKindParams{
		WorkspaceID: srcWS, Key: "rt_review", Name: "Round trip review", DefaultBaseKey: "research",
		Template: "# Review", ReviewDays: 30, RequiredSections: []string{"Summary"},
	}); err != nil {
		t.Fatalf("knowledge kind: %v", err)
	}
	if _, err := NewPipelineService(src.q, src.Pool).InstallDefault(ctx, InstallPipelineParams{
		WorkspaceID: srcWS,
		ResolveSquad: func(context.Context, string) (pgtype.UUID, bool, error) {
			return pgtype.UUID{}, false, nil
		},
	}); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	seedRoundTripMetric(t, src, srcWS)

	goals := NewGoalService(src.q, src.Pool)
	goals.Now = func() time.Time { return time.Date(2026, 11, 3, 6, 0, 0, 0, time.UTC) }
	parent, err := goals.Create(ctx, roundTripGoal(srcWS, srcActor, ScopeCompany, pgtype.UUID{}, "RT Grow revenue"))
	if err != nil {
		t.Fatalf("goal: %v", err)
	}
	if _, err := goals.Activate(ctx, srcWS, parent.Goal.ID, GoalActor{Type: GovernanceActorMember, ID: srcActor.ID}); err != nil {
		t.Fatalf("activate goal: %v", err)
	}
	child := roundTripGoal(srcWS, srcActor, ScopeFloor, util.MustParseUUID(floorID), "RT Floor goal")
	child.ParentGoalID = parent.Goal.ID
	child.OwnerType, child.OwnerID = "squad", squadUUID
	if _, err := goals.Create(ctx, child); err != nil {
		t.Fatalf("floor goal: %v", err)
	}

	budgets := &BudgetService{Queries: src.q, TxStarter: src.Pool}
	if _, err := budgets.Create(ctx, srcWS, srcActor, BudgetInput{
		ScopeType: BudgetScopeCompany, Period: BudgetPeriodMonth, AmountMicros: 1_000_000_000,
		SoftPercent: 80, Mode: BudgetModeWarn,
	}); err != nil {
		t.Fatalf("company budget: %v", err)
	}
	squadBudget, err := budgets.Create(ctx, srcWS, srcActor, BudgetInput{
		ScopeType: BudgetScopeSquad, ScopeID: squadUUID, Period: BudgetPeriodWeek, AmountMicros: 200_000_000,
		SoftPercent: 90, Mode: BudgetModeHold, Classes: []string{string(RunClassRoutine)},
	})
	if err != nil {
		t.Fatalf("squad budget: %v", err)
	}
	paused := BudgetStatusPaused
	if _, err := budgets.Update(ctx, srcWS, srcActor, squadBudget.ID, BudgetPatch{Status: &paused}); err != nil {
		t.Fatalf("pause squad budget: %v", err)
	}

	kinds := proposalkinds.NewRegistry(nil)
	policies := &ApprovalPolicyService{Queries: src.q, TxStarter: src.Pool, Kinds: kinds}
	maxPerWeek := int32(3)
	if _, err := policies.Create(ctx, srcWS, srcActor, proposalkinds.PolicySpec{
		Name: "RT decision auto", Kind: proposalkinds.KeyDecision, MaxPerWeek: &maxPerWeek,
		Match: proposalkinds.PolicyMatch{SquadIDs: []string{squadID}},
	}); err != nil {
		t.Fatalf("policy: %v", err)
	}

	doc, err := ExportCompany(ctx, src.q, srcWS, time.Now())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(doc.Goals) != 2 || len(doc.Budgets) != 2 || len(doc.ApprovalPolicies) != 1 || len(doc.Pipelines) != 1 {
		t.Fatalf("export carries goals %d, budgets %d, policies %d, pipelines %d", len(doc.Goals), len(doc.Budgets), len(doc.ApprovalPolicies), len(doc.Pipelines))
	}
	raw, _ := json.Marshal(doc)
	if strings.Contains(string(raw), runtimeID) {
		t.Fatal("the export must not carry runtime ids")
	}

	dst := newSecondPrincipal(t, src)
	dstWS := util.MustParseUUID(dst.WorkspaceID)
	seedRoundTripMetric(t, dst, dstWS)
	dstActor := util.MustParseUUID(dst.UserID)
	dstRuntime := dst.Runtime(t, "RT dst runtime")
	opts := ImportOptions{ActorID: dstActor, RuntimeID: util.MustParseUUID(dstRuntime), Kinds: kinds}

	before := roundTripCounts(t, dst)
	dry, err := ImportCompany(ctx, dst.q, dst.Pool, dstWS, doc, ImportOptions{DryRun: true, ActorID: opts.ActorID, RuntimeID: opts.RuntimeID, Kinds: kinds})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dry.DryRun || dry.Blocked != 0 {
		t.Fatalf("a dry run with a runtime blocks nothing: %+v", blockedItems(dry))
	}
	if after := roundTripCounts(t, dst); !reflect.DeepEqual(before, after) {
		t.Fatalf("a dry run wrote rows: before %v after %v", before, after)
	}

	applied, err := ImportCompany(ctx, dst.q, dst.Pool, dstWS, doc, opts)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Blocked != 0 {
		t.Fatalf("apply blocked items: %+v", blockedItems(applied))
	}
	again, err := ImportCompany(ctx, dst.q, dst.Pool, dstWS, doc, opts)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if again.Created != 0 || again.Blocked != 0 {
		t.Fatalf("a second import changes nothing: created %d, blocked %+v", again.Created, blockedItems(again))
	}

	back, err := ExportCompany(ctx, dst.q, dstWS, time.Now())
	if err != nil {
		t.Fatalf("export of the copy: %v", err)
	}
	if want, got := canonicalStructure(t, doc), canonicalStructure(t, back); want != got {
		t.Fatalf("the copy's structure differs\nsource: %s\ncopy:   %s", want, got)
	}
	if n := dst.Count(t, `SELECT COUNT(*) FROM autopilot a JOIN squad_routine r ON r.autopilot_id = a.id
		WHERE r.workspace_id = $1 AND a.status <> 'paused'`, dst.WorkspaceID); n != 0 {
		t.Fatalf("%d imported routines are not paused", n)
	}
}

// roundTripGoal is a goal with two key results on the quarter the test reads.
func roundTripGoal(ws pgtype.UUID, actor GovernanceActor, scopeType string, scopeID pgtype.UUID, title string) GoalWriteParams {
	return GoalWriteParams{
		WorkspaceID: ws, ScopeType: scopeType, ScopeID: scopeID, Title: title, Why: "Round trip",
		PeriodStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		OwnerType: "member", OwnerID: actor.ID,
		KeyResults: []GoalKeyResultInput{
			{Title: "Revenue", MetricKey: "mrr", StartValue: 0, TargetValue: 1000, Weight: 2},
			{Title: "Runs", MetricKey: "runs.count", StartValue: 0, TargetValue: 10, Weight: 1},
		},
		Actor: GoalActor{Type: GovernanceActorMember, ID: actor.ID},
	}
}

// seedRoundTripMetric makes the workspace's built-in metrics and the revenue metric
// the test's key results name.
func seedRoundTripMetric(t *testing.T, f principalFixture, ws pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	if _, err := NewMetricService(f.q, f.Pool).ListMetrics(ctx, ws); err != nil {
		t.Fatalf("seed built-in metrics: %v", err)
	}
	if _, err := f.q.EnsureManualMetric(ctx, db.EnsureManualMetricParams{
		WorkspaceID: ws, Key: "mrr", Name: "Revenue", Unit: "USD", Source: MetricSourceManual, Direction: "increase",
	}); err != nil {
		t.Fatalf("revenue metric: %v", err)
	}
}

// roundTripCounts counts the workspace's structural rows, so a dry run can be
// shown to write none of them.
func roundTripCounts(t *testing.T, f principalFixture) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"squad", "agent", "squad_charter", "squad_routine", "goal", "budget",
		"approval_policy", "pipeline", "knowledge_kind", "office_floor", "initiative", "issue", "autopilot",
		"floor_chief", "governance_event"} {
		out[table] = f.Count(t, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE workspace_id = $1", table), f.WorkspaceID)
	}
	return out
}

func blockedItems(r ImportReport) []ImportItem {
	var out []ImportItem
	for _, it := range r.Items {
		if it.Action == ImportBlocked {
			out = append(out, it)
		}
	}
	return out
}

// canonicalStructure is the export as a comparable string: the export time and
// each routine's status are left out, since a routine comes back paused, and each
// squad goal's status is left out, since an import writes a squad goal as a draft.
func canonicalStructure(t *testing.T, doc CompanyExport) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	delete(generic, "exported_at")
	if squads, ok := generic["squads"].([]any); ok {
		for _, sq := range squads {
			squad, _ := sq.(map[string]any)
			routines, _ := squad["routines"].([]any)
			for _, r := range routines {
				if routine, ok := r.(map[string]any); ok {
					delete(routine, "status")
				}
			}
		}
	}
	if goals, ok := generic["goals"].([]any); ok {
		for _, g := range goals {
			if goal, ok := g.(map[string]any); ok && goal["scope_type"] == ScopeSquad {
				delete(goal, "status")
			}
		}
	}
	out, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestCompanyRoundTripCarriesEverySquadGoalInitiativeScopeRevocationAndArchive
// exports a workspace with each entity the phase 4 round trip used to leave out:
// a squad goal, an initiative with its goal and budget, a revoked policy, an
// archived squad and a floor chief. It imports the export into an empty workspace
// and checks the spec 4.9 rules: a squad goal comes back as a draft, an initiative
// comes back with its goal and budget, a revocation is kept, an archived squad is
// archived with its routines paused, a dry run writes nothing, and a second import
// creates nothing.
func TestCompanyRoundTripCarriesEverySquadGoalInitiativeScopeRevocationAndArchive(t *testing.T) {
	src, _ := officeWorkspace(t)
	ctx := context.Background()
	srcWS := util.MustParseUUID(src.WorkspaceID)
	srcActor := GovernanceActor{Type: GovernanceActorMember, ID: util.MustParseUUID(src.UserID)}
	runtimeID, leader := ownerAndLeader(t, src, "RT2 Lead")
	worker := src.Agent(t, "RT2 Worker", runtimeID)

	floorID := src.Insert(t, "office_floor", testutil.Cols{"workspace_id": src.WorkspaceID, "name": "RT2 Floor", "position": 0})
	squadID := src.Squad(t, "RT2 Squad", leader)
	src.SquadMember(t, squadID, "agent", leader, testutil.Cols{"role": "leader"})
	src.SquadMember(t, squadID, "agent", worker, testutil.Cols{"role": "member"})
	src.Exec(t, `UPDATE squad SET floor_id = $1, office_slot = 0 WHERE id = $2`, floorID, squadID)
	archivedID := src.Squad(t, "RT2 Archived", leader)
	src.SquadMember(t, archivedID, "agent", leader, testutil.Cols{"role": "leader"})
	src.SquadMember(t, archivedID, "agent", worker, testutil.Cols{"role": "member"})
	routineAP, err := CreateSquadScheduledAutopilot(ctx, src.q, SquadScheduledAutopilot{
		WorkspaceID: srcWS, SquadID: util.MustParseUUID(archivedID), ActorUserID: srcActor.ID, Title: "RT2 Archived routine",
		Status: "active", Cron: "0 9 * * 1", Timezone: "UTC", NextRun: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("archived routine: %v", err)
	}
	if _, err := src.q.InsertSquadRoutine(ctx, db.InsertSquadRoutineParams{
		AutopilotID: routineAP.ID, WorkspaceID: srcWS, SquadID: util.MustParseUUID(archivedID), Purpose: "Keep the archived record",
		ExpectedOutput: "report", Required: false, GraceHours: 24,
	}); err != nil {
		t.Fatalf("archived squad routine: %v", err)
	}
	src.Exec(t, `UPDATE squad SET archived_at = now(), archived_by = $2, floor_id = NULL, office_slot = NULL WHERE id = $1`,
		archivedID, srcActor.ID)
	seedRoundTripMetric(t, src, srcWS)

	if _, err := NewPipelineService(src.q, src.Pool).InstallDefault(ctx, InstallPipelineParams{
		WorkspaceID: srcWS,
		ResolveSquad: func(context.Context, string) (pgtype.UUID, bool, error) {
			return pgtype.UUID{}, false, nil
		},
	}); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	issues := NewIssueService(src.q, src.Pool, nil, nil, nil)
	initSvc, _ := NewInitiativeSet(issues, nil)
	pilot, err := initSvc.Create(ctx, CreateInitiativeParams{
		WorkspaceID: srcWS, Title: "RT2 Pilot", Thesis: "A pilot to round trip.",
		Actor: InitiativeActor{Type: "member", ID: srcActor.ID},
	})
	if err != nil {
		t.Fatalf("initiative: %v", err)
	}
	parked, err := initSvc.Create(ctx, CreateInitiativeParams{
		WorkspaceID: srcWS, Title: "RT2 Parked", Thesis: "A parked venture.",
		Actor: InitiativeActor{Type: "member", ID: srcActor.ID},
	})
	if err != nil {
		t.Fatalf("second initiative: %v", err)
	}
	if _, err := initSvc.Shelve(ctx, ActionParams{
		WorkspaceID: srcWS, InitiativeID: parked.ID, Actor: InitiativeActor{Type: "member", ID: srcActor.ID}, Note: "Parked for the test.",
	}); err != nil {
		t.Fatalf("shelve: %v", err)
	}

	goals := NewGoalService(src.q, src.Pool)
	goals.Now = func() time.Time { return time.Date(2026, 11, 3, 6, 0, 0, 0, time.UTC) }
	iniGoal := roundTripGoal(srcWS, srcActor, ScopeInitiative, pilot.ID, "RT2 Pilot demand")
	iniGoal.KeyResults[0].ScopeType, iniGoal.KeyResults[0].ScopeID = ScopeInitiative, pilot.ID
	created, err := goals.Create(ctx, iniGoal)
	if err != nil {
		t.Fatalf("initiative goal: %v", err)
	}
	if _, err := goals.Activate(ctx, srcWS, created.Goal.ID, GoalActor{Type: GovernanceActorMember, ID: srcActor.ID}); err != nil {
		t.Fatalf("activate initiative goal: %v", err)
	}
	// A squad's goal is written as a row here, because the service refuses a direct
	// squad goal. One is a draft and one is active; the export carries both.
	for _, g := range []struct{ title, status string }{{"RT2 Squad draft goal", GoalStatusDraft}, {"RT2 Squad active goal", GoalStatusActive}} {
		src.Insert(t, "goal", testutil.Cols{
			"workspace_id": src.WorkspaceID, "scope_type": ScopeSquad, "scope_id": squadID, "title": g.title, "why": "Round trip",
			"period_start": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "period_end": time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			"owner_type": "squad", "owner_id": squadID, "status": g.status,
			"created_by_type": "member", "created_by_id": srcActor.ID,
		})
	}

	budgets := &BudgetService{Queries: src.q, TxStarter: src.Pool}
	if _, err := budgets.Create(ctx, srcWS, srcActor, BudgetInput{
		ScopeType: BudgetScopeInitiative, ScopeID: pilot.ID, Period: BudgetPeriodMonth, AmountMicros: 50_000_000,
		SoftPercent: 80, Mode: BudgetModeWarn,
	}); err != nil {
		t.Fatalf("initiative budget: %v", err)
	}

	kinds := proposalkinds.NewRegistry(nil)
	policies := &ApprovalPolicyService{Queries: src.q, TxStarter: src.Pool, Kinds: kinds}
	revokedSpec := proposalkinds.PolicySpec{
		Name: "RT2 retired decisions", Kind: proposalkinds.KeyDecision, MaxPerWeek: int32Ptr(2),
		Match: proposalkinds.PolicyMatch{SquadIDs: []string{squadID}},
	}
	retired, err := policies.Create(ctx, srcWS, srcActor, revokedSpec)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if _, err := policies.Revoke(ctx, srcWS, retired.ID, srcActor); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := SetFloorChiefIn(ctx, src.q, FloorChiefInput{
		WorkspaceID: srcWS, FloorID: util.MustParseUUID(floorID), AgentID: util.MustParseUUID(worker),
		ActorUserID: srcActor.ID, RoutineStatus: "active",
	}); err != nil {
		t.Fatalf("floor chief: %v", err)
	}

	doc, err := ExportCompany(ctx, src.q, srcWS, time.Now())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !hasExportGoal(doc, "RT2 Squad draft goal") || !hasExportGoal(doc, "RT2 Squad active goal") || !hasExportGoal(doc, "RT2 Pilot demand") {
		t.Fatalf("the export carries the squad goals and the initiative goal: %+v", goalTitles(doc))
	}
	if !hasExportPolicy(doc, "RT2 retired decisions", ExportPolicyRevoked) {
		t.Fatal("the export carries the revoked policy with status revoked")
	}
	if !hasExportSquad(doc, "RT2 Archived", ExportSquadArchived) || len(doc.Initiatives) != 2 {
		t.Fatalf("the export carries the archived squad and both initiatives: initiatives %d, squads %+v", len(doc.Initiatives), doc.Squads)
	}

	dst := newSecondPrincipal(t, src)
	dstWS := util.MustParseUUID(dst.WorkspaceID)
	seedRoundTripMetric(t, dst, dstWS)
	dstActor := util.MustParseUUID(dst.UserID)
	dstRuntime := util.MustParseUUID(dst.Runtime(t, "RT2 dst runtime"))
	opts := ImportOptions{ActorID: dstActor, RuntimeID: dstRuntime, Kinds: kinds, Issues: NewIssueService(dst.q, dst.Pool, nil, nil, nil)}

	before := roundTripCounts(t, dst)
	dry, err := ImportCompany(ctx, dst.q, dst.Pool, dstWS, doc, ImportOptions{DryRun: true, ActorID: dstActor, RuntimeID: dstRuntime, Kinds: kinds})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if blocked := blockedItems(dry); len(blocked) != 0 {
		t.Fatalf("a dry run with a runtime blocks nothing: %+v", blocked)
	}
	if after := roundTripCounts(t, dst); !reflect.DeepEqual(before, after) {
		t.Fatalf("a dry run wrote rows: before %v after %v", before, after)
	}

	applied, err := ImportCompany(ctx, dst.q, dst.Pool, dstWS, doc, opts)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if blocked := blockedItems(applied); len(blocked) != 0 || applied.Skipped != 0 {
		t.Fatalf("apply blocked or skipped items: blocked %+v, skipped %d", blocked, applied.Skipped)
	}
	again, err := ImportCompany(ctx, dst.q, dst.Pool, dstWS, doc, opts)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if again.Created != 0 || again.Blocked != 0 || again.Skipped != 0 {
		t.Fatalf("a second import creates nothing: created %d, items %+v", again.Created, again.Items)
	}

	back, err := ExportCompany(ctx, dst.q, dstWS, time.Now())
	if err != nil {
		t.Fatalf("export of the copy: %v", err)
	}
	if want, got := canonicalStructure(t, doc), canonicalStructure(t, back); want != got {
		i := 0
		for i < len(want) && i < len(got) && want[i] == got[i] {
			i++
		}
		from := i - 200
		if from < 0 {
			from = 0
		}
		t.Fatalf("the copy's structure differs at byte %d\nsource: %s\ncopy:   %s", i, want[from:min(len(want), i+200)], got[from:min(len(got), i+200)])
	}

	// A squad goal comes back as a draft, with a governance entry that says so.
	for _, title := range []string{"RT2 Squad draft goal", "RT2 Squad active goal"} {
		if status := dst.Count(t, `SELECT COUNT(*) FROM goal WHERE workspace_id = $1 AND title = $2 AND status = 'draft' AND source_proposal_id IS NULL`, dst.WorkspaceID, title); status != 1 {
			t.Fatalf("%q is imported as a draft", title)
		}
	}
	if n := dst.Count(t, `SELECT COUNT(*) FROM governance_event WHERE workspace_id = $1 AND kind = 'goal_imported'`, dst.WorkspaceID); n != 2 {
		t.Fatalf("each imported squad goal says so in the governance log: %d", n)
	}
	// The initiative's goal and budget belong to the initiative the import created.
	if n := dst.Count(t, `SELECT COUNT(*) FROM goal g JOIN initiative i ON i.id = g.scope_id
		WHERE g.workspace_id = $1 AND g.scope_type = 'initiative' AND i.title = 'RT2 Pilot' AND g.title = 'RT2 Pilot demand'`, dst.WorkspaceID); n != 1 {
		t.Fatalf("the initiative goal is attached to the imported initiative: %d", n)
	}
	if n := dst.Count(t, `SELECT COUNT(*) FROM budget b JOIN initiative i ON i.id = b.scope_id
		WHERE b.workspace_id = $1 AND b.scope_type = 'initiative' AND i.title = 'RT2 Pilot'`, dst.WorkspaceID); n != 1 {
		t.Fatalf("the initiative budget is attached to the imported initiative: %d", n)
	}
	if n := dst.Count(t, `SELECT COUNT(*) FROM initiative WHERE workspace_id = $1 AND title = 'RT2 Parked' AND status = 'shelved'`, dst.WorkspaceID); n != 1 {
		t.Fatalf("the shelved initiative comes back shelved: %d", n)
	}
	// The revocation is kept: the policy exists and is not active.
	if n := dst.Count(t, `SELECT COUNT(*) FROM approval_policy WHERE workspace_id = $1 AND name = 'RT2 retired decisions' AND active = false`, dst.WorkspaceID); n != 1 {
		t.Fatalf("the revoked policy comes back revoked, never active: %d", n)
	}
	if n := dst.Count(t, `SELECT COUNT(*) FROM governance_event WHERE workspace_id = $1 AND kind = 'policy_revoked'`, dst.WorkspaceID); n != 1 {
		t.Fatalf("the revocation is in the governance log: %d", n)
	}
	// The archived squad is archived, and neither its members nor its routines are re-activated.
	if n := dst.Count(t, `SELECT COUNT(*) FROM squad WHERE workspace_id = $1 AND name = 'RT2 Archived' AND archived_at IS NOT NULL`, dst.WorkspaceID); n != 1 {
		t.Fatalf("the archived squad comes back archived: %d", n)
	}
	if n := dst.Count(t, `SELECT COUNT(*) FROM autopilot a JOIN squad_routine r ON r.autopilot_id = a.id
		WHERE r.workspace_id = $1 AND a.status <> 'paused'`, dst.WorkspaceID); n != 0 {
		t.Fatalf("%d imported routines are not paused", n)
	}
	// The floor chief's brief routine is recreated paused as well.
	if n := dst.Count(t, `SELECT COUNT(*) FROM autopilot WHERE workspace_id = $1 AND title LIKE 'Daily floor brief: %' AND status = 'paused'`, dst.WorkspaceID); n != 1 {
		t.Fatalf("the imported floor chief's routine is paused: %d", n)
	}
}

func int32Ptr(v int32) *int32 { return &v }

func hasExportGoal(doc CompanyExport, title string) bool {
	for _, g := range doc.Goals {
		if g.Title == title {
			return true
		}
	}
	return false
}

func goalTitles(doc CompanyExport) []string {
	var out []string
	for _, g := range doc.Goals {
		out = append(out, g.Title)
	}
	return out
}

func hasExportPolicy(doc CompanyExport, name, status string) bool {
	for _, p := range doc.ApprovalPolicies {
		if p.Name == name && p.Status == status {
			return true
		}
	}
	return false
}

func hasExportSquad(doc CompanyExport, name, status string) bool {
	for _, sq := range doc.Squads {
		if sq.Name == name && sq.Status == status {
			return true
		}
	}
	return false
}
