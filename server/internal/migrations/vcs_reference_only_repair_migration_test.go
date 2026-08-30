package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const vcsReferenceOnlyRepairMigrationTestSchema = "vcs_reference_only_repair_migration_test"

func TestVCSReferenceOnlyRepairMigrationRestoresPartialSchema(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+vcsReferenceOnlyRepairMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+vcsReferenceOnlyRepairMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, vcsReferenceOnlyRepairMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE issue_vcs_pull_request (
			issue_id UUID NOT NULL,
			pull_request_id UUID NOT NULL,
			close_intent BOOLEAN NOT NULL DEFAULT FALSE,
			PRIMARY KEY (issue_id, pull_request_id)
		);
		INSERT INTO issue_vcs_pull_request (issue_id, pull_request_id)
		VALUES (
			'00000000-0000-0000-0000-000000000001',
			'00000000-0000-0000-0000-000000000002'
		);
	`); err != nil {
		t.Fatalf("create partial VCS link schema: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "441_vcs_reference_only_repair.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "441_vcs_reference_only_repair.up.sql")

	var referenceOnly bool
	if err := conn.QueryRow(ctx, `SELECT reference_only FROM issue_vcs_pull_request`).Scan(&referenceOnly); err != nil {
		t.Fatalf("read repaired reference_only value: %v", err)
	}
	if referenceOnly {
		t.Fatal("existing VCS link reference_only = true, want false")
	}

	var nullable, defaultValue string
	if err := conn.QueryRow(ctx, `
		SELECT is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = 'issue_vcs_pull_request'
		  AND column_name = 'reference_only'
	`, vcsReferenceOnlyRepairMigrationTestSchema).Scan(&nullable, &defaultValue); err != nil {
		t.Fatalf("inspect repaired reference_only column: %v", err)
	}
	if nullable != "NO" || defaultValue != "false" {
		t.Fatalf("reference_only metadata = (nullable %s, default %s), want (NO, false)", nullable, defaultValue)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "441_vcs_reference_only_repair.down.sql")
	if err := conn.QueryRow(ctx, `SELECT reference_only FROM issue_vcs_pull_request`).Scan(&referenceOnly); err != nil {
		t.Fatalf("read reference_only after rollback: %v", err)
	}
}
