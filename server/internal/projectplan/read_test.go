package projectplan

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestReaderRollupsMidPlanGapAndCoverageStates(t *testing.T) {
	fixture := newPlanTestFixture(t)
	planID := fixture.createManual(t, "Overview plan")
	foundationsID := fixture.addPhase(t, planID, "Foundations", 0)
	gapPhaseID := fixture.addPhase(t, planID, "Marketplace gap", 1)
	fixture.addPhase(t, planID, "Empty manual phase", 2)
	laterPhaseID := fixture.addPhase(t, planID, "Rollout", 3)

	completePartID := fixture.addPart(t, planID, foundationsID, "Complete", 0)
	gapPartID := fixture.addPart(t, planID, gapPhaseID, "No tasks yet", 0)
	inProgressPartID := fixture.addPart(t, planID, laterPhaseID, "In progress", 0)
	notStartedPartID := fixture.addPart(t, planID, laterPhaseID, "Not started", 1)
	inactivePartID := fixture.addPart(t, planID, laterPhaseID, "Covered, no active tasks", 2)

	completeIssueID := fixture.issue(t, "Complete issue", "")
	inProgressDoneID := fixture.issue(t, "Completed slice", "")
	inProgressOpenID := fixture.issue(t, "Active slice", "")
	notStartedIssueID := fixture.issue(t, "Queued slice", "")
	cancelledIssueID := fixture.issue(t, "Cancelled slice", "")
	deletedIssueID := fixture.issue(t, "Deleted slice", "")

	setIssueStatus(t, fixture, completeIssueID, "done")
	setIssueStatus(t, fixture, inProgressDoneID, "done")
	setIssueStatus(t, fixture, inProgressOpenID, "in_progress")
	setIssueStatus(t, fixture, cancelledIssueID, "cancelled")

	linkIssue(t, fixture, planID, completePartID, completeIssueID)
	linkIssue(t, fixture, planID, inProgressPartID, inProgressDoneID)
	linkIssue(t, fixture, planID, inProgressPartID, inProgressOpenID)
	linkIssue(t, fixture, planID, notStartedPartID, notStartedIssueID)
	linkIssue(t, fixture, planID, inactivePartID, cancelledIssueID)
	linkIssue(t, fixture, planID, inactivePartID, deletedIssueID)
	if _, err := fixture.pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, deletedIssueID); err != nil {
		t.Fatalf("delete linked issue: %v", err)
	}

	queries := db.New(fixture.pool)
	if _, err := queries.CreateProjectPlanDependency(context.Background(), db.CreateProjectPlanDependencyParams{
		ProjectPlanID: planID, BlockedPhaseID: laterPhaseID, BlockingPartID: completePartID,
	}); err != nil {
		t.Fatalf("create phase-to-part dependency: %v", err)
	}
	if _, err := queries.CreateProjectPlanDependency(context.Background(), db.CreateProjectPlanDependencyParams{
		ProjectPlanID: planID, BlockedPartID: inProgressPartID, BlockingPhaseID: gapPhaseID,
	}); err != nil {
		t.Fatalf("create part-to-phase dependency: %v", err)
	}

	overview, err := NewReader(queries).ReadActive(
		context.Background(), fixture.workspaceID, fixture.projectID,
	)
	if err != nil {
		t.Fatalf("ReadActive: %v", err)
	}

	if got, want := overview.Rollup, (Rollup{
		TasksDone: 2, TasksTotal: 4, Percent: 50,
		PartsCovered: 4, PartsTotal: 5, PartsWithoutTasks: 1,
	}); got != want {
		t.Fatalf("plan rollup = %+v, want %+v", got, want)
	}
	if len(overview.Phases) != 4 {
		t.Fatalf("phases = %d, want 4 (including the empty phase)", len(overview.Phases))
	}
	if got := overview.Phases[1].Rollup; got != (TaskRollup{}) {
		t.Fatalf("mid-plan gap phase rollup = %+v, want zero", got)
	}
	if got := overview.Phases[3].Rollup; got != (TaskRollup{TasksDone: 1, TasksTotal: 3, Percent: 33}) {
		t.Fatalf("later phase rollup = %+v, want 1/3 (33%%)", got)
	}

	states := make(map[string]string)
	for _, phase := range overview.Phases {
		for _, part := range phase.Parts {
			states[part.ID] = part.CoverageState
		}
	}
	for partID, want := range map[string]string{
		uuidString(completePartID):   CoverageComplete,
		uuidString(gapPartID):        CoverageNoTasksYet,
		uuidString(inProgressPartID): CoverageInProgress,
		uuidString(notStartedPartID): CoverageNotStarted,
		uuidString(inactivePartID):   CoverageCoveredNoActiveTasks,
	} {
		if got := states[partID]; got != want {
			t.Errorf("part %s coverage = %q, want %q", partID, got, want)
		}
	}

	if len(overview.UncoveredParts) != 1 || overview.UncoveredParts[0].ID != uuidString(gapPartID) {
		t.Fatalf("uncovered parts = %+v, want only the mid-plan gap", overview.UncoveredParts)
	}
	if len(overview.Dependencies) != 2 {
		t.Fatalf("dependencies = %d, want 2", len(overview.Dependencies))
	}
	if overview.Dependencies[0].Blocked.Title == "" || overview.Dependencies[0].Blocking.Title == "" ||
		overview.Dependencies[1].Blocked.Title == "" || overview.Dependencies[1].Blocking.Title == "" {
		t.Fatalf("dependency endpoint labels are incomplete: %+v", overview.Dependencies)
	}

	inactivePart := findPart(t, overview, inactivePartID)
	if inactivePart.Rollup != (TaskRollup{}) || len(inactivePart.Issues) != 2 {
		t.Fatalf("inactive part = %+v, want zero rollup and two historical rows", inactivePart)
	}
	deletedRows := 0
	for _, issue := range inactivePart.Issues {
		if issue.Deleted {
			deletedRows++
			if issue.ID != nil || issue.Status != "deleted" || issue.StatusCategory != "deleted" {
				t.Errorf("deleted issue detail = %+v", issue)
			}
		}
	}
	if deletedRows != 1 {
		t.Fatalf("deleted issue rows = %d, want 1", deletedRows)
	}
}

func TestReaderSupersededPlanUsesLiveIssueStatus(t *testing.T) {
	fixture := newPlanTestFixture(t)
	oldPlanID := fixture.createManual(t, "Version one")
	phaseID := fixture.addPhase(t, oldPlanID, "Build", 0)
	partID := fixture.addPart(t, oldPlanID, phaseID, "Read path", 0)
	issueID := fixture.issue(t, "Read API", "")
	linkIssue(t, fixture, oldPlanID, partID, issueID)

	if _, err := fixture.service.Supersede(context.Background(), SupersedeParams{
		WorkspaceID: fixture.workspaceID, PlanID: oldPlanID, CreatedBy: fixture.actor(),
	}); err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	setIssueStatus(t, fixture, issueID, "done")

	overview, err := NewReader(db.New(fixture.pool)).Read(
		context.Background(), fixture.workspaceID, fixture.projectID, oldPlanID,
	)
	if err != nil {
		t.Fatalf("Read superseded plan: %v", err)
	}
	if !overview.Plan.Superseded {
		t.Fatal("old plan is not marked superseded")
	}
	if got := overview.Rollup; got.TasksDone != 1 || got.TasksTotal != 1 || got.Percent != 100 {
		t.Fatalf("superseded plan rollup = %+v, want live 1/1", got)
	}
	issue := findPart(t, overview, partID).Issues[0]
	if issue.Status != "done" || issue.StatusCategory != "done" {
		t.Fatalf("superseded plan issue = %+v, want current done status", issue)
	}
}

func linkIssue(t *testing.T, fixture *planTestFixture, planID, partID, issueID pgtype.UUID) {
	t.Helper()
	if _, err := fixture.service.LinkIssue(
		context.Background(), fixture.workspaceID, planID, partID, issueID,
	); err != nil {
		t.Fatalf("LinkIssue: %v", err)
	}
}

func setIssueStatus(t *testing.T, fixture *planTestFixture, issueID pgtype.UUID, status string) {
	t.Helper()
	if _, err := fixture.pool.Exec(
		context.Background(), `UPDATE issue SET status = $2 WHERE id = $1`, issueID, status,
	); err != nil {
		t.Fatalf("set issue status to %q: %v", status, err)
	}
}

func findPart(t *testing.T, overview Overview, partID pgtype.UUID) Part {
	t.Helper()
	wantID := uuidString(partID)
	for _, phase := range overview.Phases {
		for _, part := range phase.Parts {
			if part.ID == wantID {
				return part
			}
		}
	}
	t.Fatalf("part %s not found", wantID)
	return Part{}
}
