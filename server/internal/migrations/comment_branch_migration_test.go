package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const commentBranchMigrationTestSchema = "comment_branch_migration_test"

func TestCommentBranchMigrationsUpDownPreserveLegacyAndMixedVersionRows(t *testing.T) {
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

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire Postgres connection: %v", err)
	}
	defer conn.Release()

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+commentBranchMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+commentBranchMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, commentBranchMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE agent_task_queue (
			id UUID PRIMARY KEY,
			issue_id UUID,
			agent_id UUID,
			status TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		INSERT INTO agent_task_queue (id, issue_id, agent_id, status)
		VALUES (
			'39000000-0000-4000-8000-000000000001',
			'39000000-0000-4000-8000-000000000010',
			'39000000-0000-4000-8000-000000000020',
			'completed'
		);
	`); err != nil {
		t.Fatalf("create pre-migration fixture: %v", err)
	}

	for _, name := range []string{
		"390_agent_task_comment_branch.up.sql",
		"391_agent_task_branch_request_unique_index.up.sql",
		"392_agent_task_deferred_branch_index.up.sql",
	} {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}

	assertCommentBranchColumns(t, ctx, conn.Conn(), true)
	assertCommentBranchIndex(t, ctx, conn.Conn(), "idx_agent_task_branch_request_unique", true)
	assertCommentBranchIndex(t, ctx, conn.Conn(), "idx_agent_task_deferred_branch", false)

	var legacyBranchColumnsNull bool
	if err := conn.QueryRow(ctx, `
		SELECT branch_point_comment_id IS NULL
		   AND branch_source_task_id IS NULL
		   AND branch_context IS NULL
		   AND branch_request_id IS NULL
		FROM agent_task_queue
		WHERE id = '39000000-0000-4000-8000-000000000001'
	`).Scan(&legacyBranchColumnsNull); err != nil {
		t.Fatalf("inspect legacy row after up migrations: %v", err)
	}
	if !legacyBranchColumnsNull {
		t.Fatal("legacy row received non-null comment branch metadata")
	}

	// Simulate an old application writer after the additive schema is live.
	if _, err := conn.Exec(ctx, `
		INSERT INTO agent_task_queue (id, issue_id, agent_id, status)
		VALUES (
			'39000000-0000-4000-8000-000000000002',
			'39000000-0000-4000-8000-000000000010',
			'39000000-0000-4000-8000-000000000020',
			'queued'
		)
	`); err != nil {
		t.Fatalf("old writer insert after additive migration: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO agent_task_queue (
			id, issue_id, agent_id, status, branch_point_comment_id,
			branch_context, branch_request_id
		) VALUES (
			'39000000-0000-4000-8000-000000000003',
			'39000000-0000-4000-8000-000000000010',
			'39000000-0000-4000-8000-000000000020',
			'deferred',
			'39000000-0000-4000-8000-000000000030',
			'{"version":1}'::jsonb,
			'39000000-0000-4000-8000-000000000040'
		)
	`); err != nil {
		t.Fatalf("new writer insert after migration: %v", err)
	}

	for _, name := range []string{
		"392_agent_task_deferred_branch_index.down.sql",
		"391_agent_task_branch_request_unique_index.down.sql",
		"390_agent_task_comment_branch.down.sql",
	} {
		applyMigrationFile(t, ctx, conn.Conn(), name)
	}

	assertCommentBranchColumns(t, ctx, conn.Conn(), false)
	var rowCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue`).Scan(&rowCount); err != nil {
		t.Fatalf("count task rows after rollback: %v", err)
	}
	if rowCount != 3 {
		t.Fatalf("task rows after rollback = %d, want 3", rowCount)
	}
}

func assertCommentBranchColumns(t *testing.T, ctx context.Context, conn *pgx.Conn, wantPresent bool) {
	t.Helper()
	var count int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = 'agent_task_queue'
		  AND column_name = ANY($2::text[])
	`, commentBranchMigrationTestSchema, []string{
		"branch_point_comment_id",
		"branch_source_task_id",
		"branch_context",
		"branch_request_id",
	}).Scan(&count); err != nil {
		t.Fatalf("inspect comment branch columns: %v", err)
	}
	want := 0
	if wantPresent {
		want = 4
	}
	if count != want {
		t.Fatalf("comment branch column count = %d, want %d", count, want)
	}
}

func assertCommentBranchIndex(t *testing.T, ctx context.Context, conn *pgx.Conn, name string, wantUnique bool) {
	t.Helper()
	var unique, valid bool
	if err := conn.QueryRow(ctx, `
		SELECT i.indisunique, i.indisvalid
		FROM pg_index i
		JOIN pg_class idx ON idx.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = idx.relnamespace
		WHERE n.nspname = $1 AND idx.relname = $2
	`, commentBranchMigrationTestSchema, name).Scan(&unique, &valid); err != nil {
		t.Fatalf("inspect index %s: %v", name, err)
	}
	if unique != wantUnique || !valid {
		t.Fatalf("index %s metadata = (unique %t, valid %t), want (unique %t, valid true)", name, unique, valid, wantUnique)
	}
}
