package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestProjectLifecycleCustomizationPinsNewIssuesAndStatusNodeTransitions(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	defer pool.Close()
	q := db.New(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var workspaceID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ('Project lifecycle test', $1, 'PLT')
		RETURNING id
	`, "project-lifecycle-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	var projectID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Project A')
		RETURNING id
	`, workspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM automation_execution WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_transition WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM project WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_lifecycle_status WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_lifecycle WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_status WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	if err := q.SeedIssueStatusEntries(ctx, workspaceID); err != nil {
		t.Fatalf("seed status catalog: %v", err)
	}
	bootstrapTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin bootstrap: %v", err)
	}
	workspaceLifecycle, err := issuelifecycle.EnsureDefault(ctx, q.WithTx(bootstrapTx), workspaceID)
	if err != nil {
		_ = bootstrapTx.Rollback(ctx)
		t.Fatalf("ensure workspace lifecycle: %v", err)
	}
	if err := bootstrapTx.Commit(ctx); err != nil {
		t.Fatalf("commit bootstrap: %v", err)
	}

	createIssue := func(title string) db.Issue {
		t.Helper()
		created, err := NewIssueService(q, pool, nil, nil, nil).Create(ctx, IssueCreateParams{
			WorkspaceID: workspaceID,
			ProjectID:   projectID,
			Title:       title,
			Status:      "todo",
			Priority:    "none",
			CreatorType: "member",
			CreatorID:   dbid.NewV7(),
		}, IssueCreateOpts{})
		if err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return created.Issue
	}

	inherited := createIssue("inherits workspace lifecycle")
	if inherited.LifecycleID != workspaceLifecycle.ID {
		t.Fatalf("inherited lifecycle = %v, want workspace default %v", inherited.LifecycleID, workspaceLifecycle.ID)
	}

	customizeTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin customize: %v", err)
	}
	customLifecycle, err := issuelifecycle.CustomizeProject(ctx, q.WithTx(customizeTx), workspaceID, projectID)
	if err != nil {
		_ = customizeTx.Rollback(ctx)
		t.Fatalf("customize project lifecycle: %v", err)
	}
	if err := customizeTx.Commit(ctx); err != nil {
		t.Fatalf("commit customization: %v", err)
	}
	if customLifecycle.ID == workspaceLifecycle.ID || customLifecycle.ScopeType != "project" || customLifecycle.ScopeID != projectID {
		t.Fatalf("custom lifecycle = %#v", customLifecycle)
	}
	effective, err := issuelifecycle.Effective(ctx, q, workspaceID, projectID)
	if err != nil {
		t.Fatalf("resolve customized effective lifecycle: %v", err)
	}
	if effective.ID != customLifecycle.ID {
		t.Fatalf("effective lifecycle after customization = %v, want %v", effective.ID, customLifecycle.ID)
	}
	customStatuses, err := q.ListIssueLifecycleStatuses(ctx, db.ListIssueLifecycleStatusesParams{
		WorkspaceID: workspaceID, LifecycleID: customLifecycle.ID, IncludeArchived: true,
	})
	if err != nil {
		t.Fatalf("list cloned statuses: %v", err)
	}
	if len(customStatuses) != 7 {
		t.Fatalf("cloned status count = %d, want 7", len(customStatuses))
	}
	wantOrder := []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}
	for i, status := range customStatuses {
		if status.Position != float64(i) || !status.LegacyStatusKey.Valid || status.LegacyStatusKey.String != wantOrder[i] {
			t.Fatalf("cloned status[%d] = key %q position %v, want key %q position %d", i, status.LegacyStatusKey.String, status.Position, wantOrder[i], i)
		}
	}

	customized := createIssue("pins project lifecycle")
	if customized.LifecycleID != customLifecycle.ID {
		t.Fatalf("customized issue lifecycle = %v, want %v", customized.LifecycleID, customLifecycle.ID)
	}
	if inherited.LifecycleID == customized.LifecycleID {
		t.Fatal("project customization rewrote or failed to separate existing issue binding")
	}

	var inProgress db.IssueLifecycleStatus
	for _, status := range customStatuses {
		if status.LegacyStatusKey.Valid && status.LegacyStatusKey.String == "in_progress" {
			inProgress = status
			break
		}
	}
	if !inProgress.ID.Valid {
		t.Fatal("custom lifecycle has no in_progress node")
	}
	transitioned, err := TransitionIssueToStatusNode(ctx, q, pool, IssueStatusNodeTransitionParams{
		IssueID:              customized.ID,
		WorkspaceID:          workspaceID,
		LifecycleStatusID:    inProgress.ID,
		Actor:                issuelifecycle.TransitionActor{Type: "member", ID: customized.CreatorID},
		Cause:                "test_status_node",
		ExpectedRevision:     pgtype.Int8{Int64: customized.Revision, Valid: true},
		ExpectedTransitionID: customized.LastTransitionID,
	})
	if err != nil {
		t.Fatalf("transition by status node: %v", err)
	}
	if !transitioned.Changed || transitioned.Issue.Status != "in_progress" || transitioned.Issue.LifecycleStatusID != inProgress.ID {
		t.Fatalf("status-node transition = %#v", transitioned)
	}
	if _, err := TransitionIssueToStatusNode(ctx, q, pool, IssueStatusNodeTransitionParams{
		IssueID: customized.ID, WorkspaceID: workspaceID, LifecycleStatusID: inProgress.ID,
		ExpectedRevision: pgtype.Int8{Int64: customized.Revision, Valid: true},
	}); !errors.Is(err, ErrIssueTransitionConflict) {
		t.Fatalf("stale lifecycle transition error = %v, want conflict", err)
	}

	defaultTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin use-default: %v", err)
	}
	if err := issuelifecycle.UseWorkspaceDefault(ctx, q.WithTx(defaultTx), workspaceID, projectID); err != nil {
		_ = defaultTx.Rollback(ctx)
		t.Fatalf("use workspace default: %v", err)
	}
	if err := defaultTx.Commit(ctx); err != nil {
		t.Fatalf("commit use-default: %v", err)
	}
	afterReset := createIssue("new issue inherits after reset")
	if afterReset.LifecycleID != workspaceLifecycle.ID {
		t.Fatalf("post-reset lifecycle = %v, want workspace default %v", afterReset.LifecycleID, workspaceLifecycle.ID)
	}
	reloadedCustom, err := q.GetIssue(ctx, customized.ID)
	if err != nil {
		t.Fatalf("reload customized issue: %v", err)
	}
	if reloadedCustom.LifecycleID != customLifecycle.ID {
		t.Fatalf("use-default drifted existing issue lifecycle to %v", reloadedCustom.LifecycleID)
	}
}
