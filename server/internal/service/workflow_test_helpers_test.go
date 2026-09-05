package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type workflowTestFixture struct {
	pool            *pgxpool.Pool
	service         *WorkflowService
	workspaceID     pgtype.UUID
	userID          pgtype.UUID
	nextIssueNumber int32
}

func newWorkflowTestFixture(t *testing.T) *workflowTestFixture {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	suffix := time.Now().UnixNano()
	var userID, workspaceID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Workflow Test", fmt.Sprintf("workflow-%d@multica.test", suffix)).Scan(&userID); err != nil {
		pool.Close()
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id`,
		"Workflow Test", fmt.Sprintf("workflow-%d", suffix)).Scan(&workspaceID); err != nil {
		pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
		pool.Close()
		t.Fatalf("seed workspace: %v", err)
	}

	fx := &workflowTestFixture{
		pool: pool, service: NewWorkflowService(db.New(pool), pool),
		workspaceID: workspaceID, userID: userID, nextIssueNumber: 1,
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
		pool.Close()
	})
	return fx
}

func (f *workflowTestFixture) createDefinition(t *testing.T, name, raw string) db.WorkflowDefinition {
	t.Helper()
	row, err := f.service.CreateDefinition(context.Background(), CreateWorkflowDefinitionParams{
		WorkspaceID: f.workspaceID,
		Name:        name,
		Definition:  []byte(raw),
		CreatedBy:   f.userID,
	})
	if err != nil {
		t.Fatalf("CreateDefinition(%q): %v", name, err)
	}
	return row
}

func (f *workflowTestFixture) createParent(t *testing.T, status string) db.Issue {
	t.Helper()
	return f.createIssue(t, pgtype.UUID{}, pgtype.Int4{}, status, "workflow parent")
}

func (f *workflowTestFixture) createChild(t *testing.T, parentID pgtype.UUID, stage int32, status string) db.Issue {
	t.Helper()
	return f.createIssue(t, parentID, pgtype.Int4{Int32: stage, Valid: true}, status, fmt.Sprintf("workflow child %d", f.nextIssueNumber))
}

func (f *workflowTestFixture) createIssue(t *testing.T, parentID pgtype.UUID, stage pgtype.Int4, status, title string) db.Issue {
	t.Helper()
	ctx := context.Background()
	number := f.nextIssueNumber
	f.nextIssueNumber++
	var issue db.Issue
	err := f.pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, parent_issue_id, stage, position, number)
		VALUES ($1, $2, $3, 'none', 'member', $4, $5, $6, 0, $7)
		RETURNING id, workspace_id, title, description, status, priority, assignee_type, assignee_id, creator_type, creator_id,
		          parent_issue_id, acceptance_criteria, context_refs, position, due_date, created_at, updated_at, number, project_id,
		          origin_type, origin_id, first_executed_at, start_date, metadata, stage, properties, revision, last_activity_at`,
		f.workspaceID, title, status, f.userID, parentID, stage, number).Scan(
		&issue.ID, &issue.WorkspaceID, &issue.Title, &issue.Description, &issue.Status, &issue.Priority,
		&issue.AssigneeType, &issue.AssigneeID, &issue.CreatorType, &issue.CreatorID, &issue.ParentIssueID,
		&issue.AcceptanceCriteria, &issue.ContextRefs, &issue.Position, &issue.DueDate, &issue.CreatedAt, &issue.UpdatedAt,
		&issue.Number, &issue.ProjectID, &issue.OriginType, &issue.OriginID, &issue.FirstExecutedAt, &issue.StartDate,
		&issue.Metadata, &issue.Stage, &issue.Properties, &issue.Revision, &issue.LastActivityAt,
	)
	if err != nil {
		t.Fatalf("create workflow issue: %v", err)
	}
	return issue
}

func assertWorkflowIssueStatus(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	if got != want {
		t.Fatalf("issue status = %q, want %q", got, want)
	}
}
