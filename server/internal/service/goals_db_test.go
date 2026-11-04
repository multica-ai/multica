package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// quarterGoal is a company goal for Q4 2026 with two key results: revenue to 1000
// (weight 2) and runs to 10 (weight 1).
func quarterGoal(f govFixture) GoalWriteParams {
	return GoalWriteParams{
		WorkspaceID: f.ws, ScopeType: ScopeCompany, Title: "Grow revenue",
		PeriodStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		OwnerType:   "member", OwnerID: util.MustParseUUID(f.owner),
		KeyResults: []GoalKeyResultInput{
			{Title: "Revenue", MetricKey: "mrr", StartValue: 0, TargetValue: 1000, Weight: 2},
			{Title: "Runs", MetricKey: "runs.count", StartValue: 0, TargetValue: 10, Weight: 1},
		},
		Actor: f.memberActor(),
	}
}

// TestGoalLifecycleProgressConfidenceAndOverride walks a company goal through its
// life on the pinned clock: drafted, activated (logged), read, derived on track and
// then off track, overridden with a note, and closed. Each rule that refuses a move
// is checked on the way.
func TestGoalLifecycleProgressConfidenceAndOverride(t *testing.T) {
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

	draft, err := goals.Create(ctx, quarterGoal(f))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if draft.Goal.Status != GoalStatusDraft || draft.Derived != ConfidenceUnknown || len(draft.KeyResults) != 2 {
		t.Fatalf("draft = status %s derived %s, %d key results", draft.Goal.Status, draft.Derived, len(draft.KeyResults))
	}
	goalID := util.UUIDToString(draft.Goal.ID)
	goalPG := util.MustParseUUID(goalID)

	// A squad's goal is not written directly.
	squadGoal := quarterGoal(f)
	squadGoal.ScopeType = ScopeSquad
	squadGoal.ScopeID = util.MustParseUUID(f.squad)
	if _, err := goals.Create(ctx, squadGoal); !errors.Is(err, ErrGoalSquadScope) {
		t.Fatalf("a squad goal written directly: %v, want ErrGoalSquadScope", err)
	}
	// Key results are checked: a target equal to the start, and a metric the
	// workspace does not have.
	badTarget := quarterGoal(f)
	badTarget.KeyResults[0].TargetValue = 0
	if _, err := goals.Create(ctx, badTarget); !errors.Is(err, ErrGoalInvalid) {
		t.Fatalf("target equal to start: %v, want ErrGoalInvalid", err)
	}
	noMetric := quarterGoal(f)
	noMetric.KeyResults[0].MetricKey = "no.such.metric"
	if _, err := goals.Create(ctx, noMetric); !errors.Is(err, ErrGoalInvalid) {
		t.Fatalf("unknown metric: %v, want ErrGoalInvalid", err)
	}

	// Activation is the boss's, and it is logged.
	active, err := goals.Activate(ctx, f.ws, goalPG, f.memberActor())
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if active.Goal.Status != GoalStatusActive {
		t.Fatalf("status after activation = %s", active.Goal.Status)
	}
	if _, err := goals.Activate(ctx, f.ws, goalPG, f.memberActor()); !errors.Is(err, ErrGoalState) {
		t.Fatalf("second activation: %v, want ErrGoalState", err)
	}
	var logged int
	f.QueryRow(t, `SELECT count(*) FROM governance_event WHERE workspace_id = $1 AND kind = 'goal_activated' AND subject_id = $2`, f.wsText, goalID).Scan(&logged)
	if logged != 1 {
		t.Fatalf("goal_activated entries = %d, want 1", logged)
	}
	// An active goal's key results are fixed.
	fixed := GoalUpdate{Title: "Grow revenue", PeriodStart: quarterGoal(f).PeriodStart, PeriodEnd: quarterGoal(f).PeriodEnd,
		OwnerType: "member", OwnerID: util.MustParseUUID(f.owner), KeyResults: []GoalKeyResultInput{{Title: "x", MetricKey: "mrr", StartValue: 0, TargetValue: 5}}}
	if _, err := goals.Update(ctx, f.ws, goalPG, fixed); !errors.Is(err, ErrGoalState) {
		t.Fatalf("replacing an active goal's key results: %v, want ErrGoalState", err)
	}

	// Nothing read yet: no progress, and the confidence is unknown.
	view, _ := goals.Get(ctx, f.ws, goalPG)
	if view.Progress != nil || view.Derived != ConfidenceUnknown || view.ReadingsMissing != 2 {
		t.Fatalf("before readings: progress %v derived %s missing %d", view.Progress, view.Derived, view.ReadingsMissing)
	}

	// Revenue at 500 of 1000, and the runs fixture at none: on the line for
	// week five of thirteen, the weighted progress is 1/3 against an expected share
	// of 33 days in 92, so on track.
	svc := f.metricsService()
	if _, err := svc.RecordReading(ctx, ReadingInput{WorkspaceID: f.ws, Key: "mrr", ScopeType: ScopeCompany, Value: 500, ActorType: ReadingActorMember, ActorID: util.MustParseUUID(f.owner)}, f.now); err != nil {
		t.Fatalf("report revenue: %v", err)
	}
	if _, err := f.metricsService().RollupBuiltinMetrics(ctx, f.ws, f.now); err != nil {
		t.Fatalf("rollup runs: %v", err)
	}
	view, _ = goals.Get(ctx, f.ws, goalPG)
	if view.Progress == nil || !closeTo(*view.Progress, 1.0/3.0) || view.Derived != ConfidenceOnTrack {
		t.Fatalf("after readings: progress %v derived %s, want 1/3 and on_track", view.Progress, view.Derived)
	}
	if got := len(view.KeyResults[0].Series); got != 1 {
		t.Errorf("revenue series has %d points, want 1", got)
	}

	// Revenue falls to nothing: off track, and the stored confidence follows once
	// the goals are refreshed, which a reading does.
	if _, err := svc.RecordReading(ctx, ReadingInput{WorkspaceID: f.ws, Key: "mrr", ScopeType: ScopeCompany, Value: 0, ActorType: ReadingActorMember, ActorID: util.MustParseUUID(f.owner)}, f.now.Add(time.Minute)); err != nil {
		t.Fatalf("report falling revenue: %v", err)
	}
	if _, err := goals.RefreshConfidence(ctx, f.ws); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	view, _ = goals.Get(ctx, f.ws, goalPG)
	if view.Derived != ConfidenceOffTrack || view.Goal.Confidence != ConfidenceOffTrack {
		t.Fatalf("after the fall: derived %s stored %s, want off_track", view.Derived, view.Goal.Confidence)
	}

	// The owner overrides it, with a note. A value with no note is refused.
	on := ConfidenceOnTrack
	if _, err := goals.SetConfidenceOverride(ctx, f.ws, goalPG, &on, "  "); !errors.Is(err, ErrGoalInvalid) {
		t.Fatalf("override without a note: %v, want ErrGoalInvalid", err)
	}
	over, err := goals.SetConfidenceOverride(ctx, f.ws, goalPG, &on, "A signed contract starts in November.")
	if err != nil {
		t.Fatalf("override: %v", err)
	}
	if !over.Overridden || over.Effective != ConfidenceOnTrack || over.Derived != ConfidenceOffTrack {
		t.Fatalf("override: overridden %v effective %s derived %s", over.Overridden, over.Effective, over.Derived)
	}
	cleared, err := goals.SetConfidenceOverride(ctx, f.ws, goalPG, nil, "")
	if err != nil || cleared.Overridden || cleared.Effective != ConfidenceOffTrack {
		t.Fatalf("clearing the override: %+v, %v", cleared, err)
	}

	// Closing: active goals close; a closed goal does not move again.
	closed, err := goals.SetStatus(ctx, f.ws, goalPG, GoalStatusMissed)
	if err != nil || closed.Goal.Status != GoalStatusMissed {
		t.Fatalf("close: status %v, %v", closed.Goal.Status, err)
	}
	if _, err := goals.SetStatus(ctx, f.ws, goalPG, GoalStatusAchieved); !errors.Is(err, ErrGoalState) {
		t.Fatalf("a closed goal changed: %v, want ErrGoalState", err)
	}
	if err := goals.Delete(ctx, f.ws, goalPG); !errors.Is(err, ErrGoalState) {
		t.Fatalf("deleting a goal that was active: %v, want ErrGoalState", err)
	}

	// A draft can be dropped and deleted.
	other, err := goals.Create(ctx, quarterGoal(f))
	if err != nil {
		t.Fatalf("second draft: %v", err)
	}
	if err := goals.Delete(ctx, f.ws, other.Goal.ID); err != nil {
		t.Fatalf("delete draft: %v", err)
	}
	if _, err := goals.Get(ctx, f.ws, other.Goal.ID); !errors.Is(err, ErrGoalNotFound) {
		t.Fatalf("a deleted draft is still there: %v", err)
	}
}

// TestGoalActivationFromAGoalProposalWritesTheGoalAndTheLog: the approval of a
// squad's goal proposal writes an active goal, scoped and owned by the squad, with
// its key results, and a log entry naming the decider.
func TestGoalActivationFromAGoalProposalWritesTheGoalAndTheLog(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	if _, err := f.metricsService().ListMetrics(ctx, f.ws); err != nil {
		t.Fatalf("seed built-ins: %v", err)
	}
	issue := f.Issue(t, "goal proposal")
	f.Insert(t, "proposal", testutil.Cols{
		"workspace_id": f.wsText, "squad_id": f.squad, "issue_id": issue, "state": "approved",
		"created_by_type": "agent", "created_by_id": f.agent, "kind": proposalkinds.KeyGoal,
		"payload": `{}`, "expires_at": f.now.AddDate(0, 0, 14),
	})
	var proposalID string
	f.QueryRow(t, `SELECT id::text FROM proposal WHERE issue_id = $1`, issue).Scan(&proposalID)
	prop, err := f.q.GetProposalInWorkspace(ctx, db.GetProposalInWorkspaceParams{ID: util.MustParseUUID(proposalID), WorkspaceID: f.ws})
	if err != nil {
		t.Fatalf("load proposal: %v", err)
	}

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	filing := proposalkinds.GoalFiling{
		Title: "Ship the onboarding flow", PeriodStart: "2026-10-01", PeriodEnd: "2026-12-31",
		KeyResults: []proposalkinds.GoalFilingKeyResult{{Title: "Activated teams", MetricKey: "runs.count", StartValue: 0, TargetValue: 20}},
	}
	d := proposalkinds.Decision{Tx: tx, WorkspaceID: f.ws, Proposal: prop, DeciderID: util.MustParseUUID(f.owner)}
	if err := f.goalService().ActivateFromProposal(ctx, f.q.WithTx(tx), d, filing); err != nil {
		t.Fatalf("activate from proposal: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var status, scopeType string
	var sourceProposal, scopeID string
	f.QueryRow(t, `SELECT status, scope_type, scope_id::text, source_proposal_id::text FROM goal
		WHERE workspace_id = $1 AND title = 'Ship the onboarding flow'`, f.wsText).Scan(&status, &scopeType, &scopeID, &sourceProposal)
	if status != GoalStatusActive || scopeType != ScopeSquad || scopeID != f.squad || sourceProposal != proposalID {
		t.Fatalf("goal = %s %s %s from %s", status, scopeType, scopeID, sourceProposal)
	}
	var logged int
	f.QueryRow(t, `SELECT count(*) FROM governance_event WHERE workspace_id = $1 AND kind = 'goal_activated' AND actor_id = $2 AND summary LIKE '%onboarding%'`, f.wsText, f.owner).Scan(&logged)
	if logged != 1 {
		t.Fatalf("goal_activated entries naming the decider = %d, want 1", logged)
	}
}

// TestPlanningCycleOpensOneIssuePerSquadAndPeriod: a squad with a charter gets one
// planning issue for the quarter, assigned to it, and a second run for the same
// quarter opens nothing. The next quarter gets its own.
func TestPlanningCycleOpensOneIssuePerSquadAndPeriod(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	f.Exec(t, `INSERT INTO squad_charter (squad_id, workspace_id, mission, responsibilities)
		VALUES ($1, $2, 'Keep the research queue honest', '[{"key":"triage","text":"Triage inbound requests"}]')`, f.squad, f.wsText)
	// A second squad with no charter gets no planning issue.
	f.Squad(t, "uncharted squad", f.agent)

	goals := f.goalService()
	inWorkspace := func(issues []PlanningIssue) []PlanningIssue {
		var out []PlanningIssue
		for _, p := range issues {
			if p.WorkspaceID == f.ws {
				out = append(out, p)
			}
		}
		return out
	}
	first, err := goals.RunPlanningCycle(ctx, f.now)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	got := inWorkspace(first)
	if len(got) != 1 {
		t.Fatalf("first run opened %d issues here, want 1", len(got))
	}
	issue := got[0].Issue
	if issue.AssigneeType.String != "squad" || util.UUIDToString(issue.AssigneeID) != f.squad || issue.Status != "todo" {
		t.Fatalf("issue assigned to %s %s, status %s", issue.AssigneeType.String, util.UUIDToString(issue.AssigneeID), issue.Status)
	}
	if !strings.Contains(issue.Title, "Q4 2026") || !strings.Contains(issue.Description.String, "Keep the research queue honest") {
		t.Fatalf("issue title %q lacks the quarter or the charter", issue.Title)
	}
	if !strings.Contains(issue.Description.String, `"payload"`) && !strings.Contains(issue.Description.String, "proposal of kind goal") {
		t.Fatalf("issue does not say how to file the goals")
	}

	second, err := goals.RunPlanningCycle(ctx, f.now.Add(time.Hour))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := len(inWorkspace(second)); n != 0 {
		t.Fatalf("second run in the same quarter opened %d issues, want 0", n)
	}
	var rows int
	f.QueryRow(t, `SELECT count(*) FROM planning_cycle WHERE workspace_id = $1`, f.wsText).Scan(&rows)
	if rows != 1 {
		t.Fatalf("planning rows = %d, want 1", rows)
	}

	nextQuarter, err := goals.RunPlanningCycle(ctx, time.Date(2027, time.January, 5, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("next quarter: %v", err)
	}
	if n := len(inWorkspace(nextQuarter)); n != 1 {
		t.Fatalf("the next quarter opened %d issues, want 1", n)
	}
}

// A goal with no key results is a valid draft and can be activated. Its progress
// is not set and its confidence is unknown, so nothing reads as on track.
func TestGoalWithNoKeyResultsDraftsAndActivatesWithProgressNotSet(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	goals := f.goalService()
	if _, err := f.metricsService().ListMetrics(ctx, f.ws); err != nil {
		t.Fatalf("seed built-ins: %v", err)
	}
	g := quarterGoal(f)
	g.KeyResults = nil
	draft, err := goals.Create(ctx, g)
	if err != nil {
		t.Fatalf("create a goal with no key results: %v", err)
	}
	if draft.Goal.Status != GoalStatusDraft || len(draft.KeyResults) != 0 || draft.Progress != nil || draft.Derived != ConfidenceUnknown {
		t.Fatalf("draft = status %s, %d key results, progress %v, derived %s; want draft, none, not set, unknown",
			draft.Goal.Status, len(draft.KeyResults), draft.Progress, draft.Derived)
	}
	active, err := goals.Activate(ctx, f.ws, draft.Goal.ID, f.memberActor())
	if err != nil {
		t.Fatalf("activate a goal with no key results: %v", err)
	}
	if active.Goal.Status != GoalStatusActive || active.Progress != nil || active.Effective != ConfidenceUnknown {
		t.Fatalf("active = status %s, progress %v, confidence %s; want active, not set, unknown",
			active.Goal.Status, active.Progress, active.Effective)
	}
}
