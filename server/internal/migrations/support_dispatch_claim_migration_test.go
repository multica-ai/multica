package migrations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const supportClaimMigrationSchema = "support_claim_migration_test"

func TestSupportDispatchClaimMigrationPreservesUniqueFence(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires disposable Postgres at DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+supportClaimMigrationSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+supportClaimMigrationSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, supportClaimMigrationSchema); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "564_support_dispatch_claim.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "565_support_dispatch_claim_unique.up.sql")
	const row = `INSERT INTO support_dispatch_claim (workspace_id, issue_id, issue_revision,
		staff_comment_id, staff_comment_revision, attestation_digest, claimed_by_user_id)
		VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002',
		2, '00000000-0000-0000-0000-000000000003', 1,
		'0000000000000000000000000000000000000000000000000000000000000000',
		'00000000-0000-0000-0000-000000000004')`
	if _, err := conn.Exec(ctx, row); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, row); err == nil {
		t.Fatal("same issue claimed twice")
	}
	applyMigrationFile(t, ctx, conn.Conn(), "565_support_dispatch_claim_unique.down.sql")
	if _, err := conn.Exec(ctx, row); err == nil {
		t.Fatal("partial rollback removed unique fence")
	}
	down, err := os.ReadFile(filepath.Join(realMigrationsDir(t), "564_support_dispatch_claim.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(down)); err == nil {
		t.Fatal("nonempty claim table was removed")
	}
	// Reapply is harmless when the safety index survived the partial rollback.
	applyMigrationFile(t, ctx, conn.Conn(), "565_support_dispatch_claim_unique.up.sql")
	if _, err := conn.Exec(ctx, "DELETE FROM support_dispatch_claim"); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, ctx, conn.Conn(), "564_support_dispatch_claim.down.sql")
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('support_dispatch_claim') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("empty table did not roll back: exists %v, error %v", exists, err)
	}
}
