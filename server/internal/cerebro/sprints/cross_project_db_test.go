package sprints

// FIR-1657: integration coverage for cross-project sprint selection.
//
// The picker and the assign endpoint both rely on the per-project
// accepts_external_issues flag: a sprint in project B is selectable from an
// issue in project A only when B opted in (enabled AND accepts_external_issues).
// These tests exercise ListSelectableCerebroSprintsForIssue against a real DB,
// reusing the shared fixture (workspace/user/project) from sweeper_db_test.go.
// They skip cleanly when no test DB is reachable.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

func insertProject(t *testing.T, ctx context.Context, title string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := sweeperTestPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, sweeperTestWorkspaceID, title).Scan(&id); err != nil {
		t.Fatalf("create project %q: %v", title, err)
	}
	return id
}

func insertIssueInProject(t *testing.T, ctx context.Context, projectID pgtype.UUID, title string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := sweeperTestPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, project_id, title, creator_type, creator_id, number)
		VALUES ($1, $2, $3, 'member', $4, (SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, sweeperTestWorkspaceID, projectID, title, sweeperTestUserID).Scan(&id); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	return id
}

func seedSettingsForProject(t *testing.T, ctx context.Context, projectID pgtype.UUID, enabled, acceptsExternal bool) {
	t.Helper()
	q := cerebrodb.New(sweeperTestPool)
	if _, err := q.UpsertCerebroSprintSettings(ctx, cerebrodb.UpsertCerebroSprintSettingsParams{
		ProjectID:                  projectID,
		WorkspaceID:                sweeperTestWorkspaceID,
		Enabled:                    enabled,
		DurationUnit:               UnitWeek,
		DurationCount:              2,
		StartWeekday:               1,
		NameTemplate:               "Sprint {n}",
		AutoCreateEnabled:          false,
		AutoCreateLeadDays:         2,
		MoveIncompleteEnabled:      true,
		MoveIncompleteTargetStatus: "todo",
		Timezone:                   "UTC",
		AcceptsExternalIssues:      acceptsExternal,
	}); err != nil {
		t.Fatalf("upsert settings: %v", err)
	}
}

func seedSprintForProject(t *testing.T, ctx context.Context, projectID pgtype.UUID, name string, seq int32) cerebrodb.CerebroSprint {
	t.Helper()
	q := cerebrodb.New(sweeperTestPool)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	sprint, err := q.CreateCerebroSprint(ctx, cerebrodb.CreateCerebroSprintParams{
		WorkspaceID: sweeperTestWorkspaceID,
		ProjectID:   projectID,
		Name:        name,
		SequenceNo:  seq,
		Status:      StatusPlanned,
		StartDate:   pgtype.Date{Time: start, Valid: true},
		EndDate:     pgtype.Date{Time: start.AddDate(0, 0, 13), Valid: true},
	})
	if err != nil {
		t.Fatalf("create sprint %q: %v", name, err)
	}
	return sprint
}

// TestSelectableSprints_OwnPlusOptedIn is the core FIR-1657 behaviour: an
// issue sees its own project's sprints plus the sprints of any OTHER project
// that opted in, and never the sprints of a project that did not.
func TestSelectableSprints_OwnPlusOptedIn(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	q := cerebrodb.New(sweeperTestPool)

	// Own project (the issue's home) — its own sprints are always selectable.
	seedSettingsForProject(t, ctx, sweeperTestProjectID, true, false)
	ownSprint := seedSprintForProject(t, ctx, sweeperTestProjectID, "Own S1", 1)

	// Opted-in sibling project — its sprints should appear.
	openProject := insertProject(t, ctx, "Open Project")
	seedSettingsForProject(t, ctx, openProject, true, true)
	openSprint := seedSprintForProject(t, ctx, openProject, "Open S1", 1)

	// Closed sibling project — enabled but not accepting external issues.
	closedProject := insertProject(t, ctx, "Closed Project")
	seedSettingsForProject(t, ctx, closedProject, true, false)
	closedSprint := seedSprintForProject(t, ctx, closedProject, "Closed S1", 1)

	issueID := insertIssueInProject(t, ctx, sweeperTestProjectID, "Home issue")

	rows, err := q.ListSelectableCerebroSprintsForIssue(ctx, cerebrodb.ListSelectableCerebroSprintsForIssueParams{
		WorkspaceID: sweeperTestWorkspaceID,
		ProjectID:   sweeperTestProjectID,
	})
	if err != nil {
		t.Fatalf("list selectable: %v", err)
	}

	got := map[string]cerebrodb.ListSelectableCerebroSprintsForIssueRow{}
	for _, r := range rows {
		got[util.UUIDToString(r.ID)] = r
	}

	ownID := util.UUIDToString(ownSprint.ID)
	openID := util.UUIDToString(openSprint.ID)
	closedID := util.UUIDToString(closedSprint.ID)

	if _, ok := got[ownID]; !ok {
		t.Fatalf("own project's sprint must be selectable")
	}
	if !got[ownID].IsOwnProject {
		t.Fatalf("own sprint must be flagged is_own_project")
	}
	if _, ok := got[openID]; !ok {
		t.Fatalf("opted-in project's sprint must be selectable")
	}
	if got[openID].IsOwnProject {
		t.Fatalf("opted-in sprint must NOT be flagged is_own_project")
	}
	if got[openID].ProjectTitle != "Open Project" {
		t.Fatalf("opted-in sprint must carry project title, got %q", got[openID].ProjectTitle)
	}
	if _, ok := got[closedID]; ok {
		t.Fatalf("closed project's sprint must NOT be selectable")
	}

	// Own-project sprints sort first.
	if len(rows) == 0 || !rows[0].IsOwnProject {
		t.Fatalf("own-project sprints must be ordered first")
	}

	// Use issueID so the fixture stays realistic and the var is referenced.
	pid, err := q.GetCerebroIssueProjectID(ctx, issueID)
	if err != nil {
		t.Fatalf("get issue project: %v", err)
	}
	if util.UUIDToString(pid) != util.UUIDToString(sweeperTestProjectID) {
		t.Fatalf("issue project mismatch")
	}
}

// TestSelectableSprints_DisabledOptIn confirms an opted-in flag is ignored
// when the project's sprint feature itself is disabled.
func TestSelectableSprints_DisabledOptIn(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	q := cerebrodb.New(sweeperTestPool)
	seedSettingsForProject(t, ctx, sweeperTestProjectID, true, false)

	// Sibling project accepts external issues but is disabled — must not leak.
	disabledProject := insertProject(t, ctx, "Disabled Project")
	seedSettingsForProject(t, ctx, disabledProject, false, true)
	disabledSprint := seedSprintForProject(t, ctx, disabledProject, "Disabled S1", 1)

	rows, err := q.ListSelectableCerebroSprintsForIssue(ctx, cerebrodb.ListSelectableCerebroSprintsForIssueParams{
		WorkspaceID: sweeperTestWorkspaceID,
		ProjectID:   sweeperTestProjectID,
	})
	if err != nil {
		t.Fatalf("list selectable: %v", err)
	}
	for _, r := range rows {
		if util.UUIDToString(r.ID) == util.UUIDToString(disabledSprint.ID) {
			t.Fatalf("disabled project's sprint must not be selectable")
		}
	}
}
