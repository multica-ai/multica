package sprints

// FIR-2500: integration coverage for the workspace-wide sprint queries that
// back the CLI: ListCerebroSprintsByWorkspace (find sprints — notably the
// active one — without knowing the owning project) and
// ListCerebroSprintIssueDetailsBySprint (sprint overview with issue titles
// and statuses). Reuses the shared fixture from sweeper_db_test.go and skips
// cleanly when no test DB is reachable.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestListCerebroSprintsByWorkspace_StatusFilter(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	q := cerebrodb.New(sweeperTestPool)

	seedSettingsForProject(t, ctx, sweeperTestProjectID, true, false)
	planned := seedSprintForProject(t, ctx, sweeperTestProjectID, "Planned S1", 1)

	otherProject := insertProject(t, ctx, "Other Project")
	seedSettingsForProject(t, ctx, otherProject, true, false)
	active := seedSprintForProject(t, ctx, otherProject, "Active S1", 1)
	if err := q.SetCerebroSprintStatus(ctx, cerebrodb.SetCerebroSprintStatusParams{
		ID:     active.ID,
		Status: StatusActive,
	}); err != nil {
		t.Fatalf("set sprint active: %v", err)
	}

	all, err := q.ListCerebroSprintsByWorkspace(ctx, cerebrodb.ListCerebroSprintsByWorkspaceParams{
		WorkspaceID: sweeperTestWorkspaceID,
	})
	if err != nil {
		t.Fatalf("list workspace sprints: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("workspace sprints = %d, want 2", len(all))
	}
	// Active sprints sort first so a CLI caller sees the current sprint on top.
	if all[0].ID != active.ID {
		t.Fatalf("first sprint = %s (%s), want the active one", all[0].Name, all[0].Status)
	}
	if all[0].ProjectTitle != "Other Project" {
		t.Fatalf("project_title = %q, want %q", all[0].ProjectTitle, "Other Project")
	}

	activeOnly, err := q.ListCerebroSprintsByWorkspace(ctx, cerebrodb.ListCerebroSprintsByWorkspaceParams{
		WorkspaceID: sweeperTestWorkspaceID,
		Status:      pgtype.Text{String: StatusActive, Valid: true},
	})
	if err != nil {
		t.Fatalf("list active sprints: %v", err)
	}
	if len(activeOnly) != 1 || activeOnly[0].ID != active.ID {
		t.Fatalf("active filter returned %d rows, want exactly the active sprint", len(activeOnly))
	}
	if activeOnly[0].ID == planned.ID {
		t.Fatalf("active filter returned the planned sprint")
	}
}

func TestListCerebroSprintIssueDetailsBySprint(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	q := cerebrodb.New(sweeperTestPool)

	seedSettingsForProject(t, ctx, sweeperTestProjectID, true, false)
	sprint := seedSprintForProject(t, ctx, sweeperTestProjectID, "S1", 1)

	issueID := insertIssueInProject(t, ctx, sweeperTestProjectID, "Ship the CLI sprint support")
	if err := q.AssignIssueToCerebroSprint(ctx, cerebrodb.AssignIssueToCerebroSprintParams{
		IssueID:  issueID,
		SprintID: sprint.ID,
	}); err != nil {
		t.Fatalf("assign issue: %v", err)
	}

	rows, err := q.ListCerebroSprintIssueDetailsBySprint(ctx, sprint.ID)
	if err != nil {
		t.Fatalf("list sprint issue details: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sprint issues = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Title != "Ship the CLI sprint support" {
		t.Fatalf("title = %q", row.Title)
	}
	if row.Status == "" || row.Number <= 0 {
		t.Fatalf("expected joined status and number, got status=%q number=%d", row.Status, row.Number)
	}
}
