package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
)

// createServiceTestSpace gives direct-SQL service fixtures the same minimum
// Space graph that the workspace HTTP creation flow creates in production.
func createServiceTestSpace(t *testing.T, pool *pgxpool.Pool, workspaceID, ownerID string) string {
	t.Helper()
	ctx := context.Background()

	var spaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, is_default, created_by)
		VALUES ($1, 'Test Space', 'TEST', true, $2)
		RETURNING id
	`, workspaceID, ownerID).Scan(&spaceID); err != nil {
		t.Fatalf("create test Space: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace_space_member (workspace_id, space_id, user_id, role)
		VALUES ($1, $2, $3, 'lead')
	`, workspaceID, spaceID, ownerID); err != nil {
		t.Fatalf("join test Space: %v", err)
	}
	return spaceID
}

func serviceTestSpaceUUID(t *testing.T, pool *pgxpool.Pool, workspaceID string) pgtype.UUID {
	t.Helper()
	var spaceID string
	if err := pool.QueryRow(context.Background(), `
		SELECT id FROM workspace_space
		WHERE workspace_id = $1 AND is_default
		LIMIT 1
	`, workspaceID).Scan(&spaceID); err != nil {
		t.Fatalf("load test Space: %v", err)
	}
	return util.MustParseUUID(spaceID)
}
