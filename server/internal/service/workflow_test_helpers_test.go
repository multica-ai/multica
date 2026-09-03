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
	pool        *pgxpool.Pool
	service     *WorkflowService
	workspaceID pgtype.UUID
	userID      pgtype.UUID
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
		workspaceID: workspaceID, userID: userID,
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
