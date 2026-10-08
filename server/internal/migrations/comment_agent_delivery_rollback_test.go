package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const commentAgentDeliveryRollbackTestSchema = "comment_agent_delivery_rollback_test"

func TestCommentAgentDeliveryRollbackMigrationRemovesRevertedSchema(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+commentAgentDeliveryRollbackTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+commentAgentDeliveryRollbackTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, commentAgentDeliveryRollbackTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		CREATE TABLE comment_agent_delivery (comment_id UUID PRIMARY KEY);
		INSERT INTO comment_agent_delivery (comment_id) VALUES (gen_random_uuid());
	`); err != nil {
		t.Fatalf("create reverted feature schema: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "534_drop_comment_agent_delivery.up.sql")
	assertCommentAgentDeliveryTableMissing(t, ctx, conn)

	// Both fresh installs and a retried migration see no table. The cleanup is
	// deliberately idempotent and its down direction must not recreate data.
	applyMigrationFile(t, ctx, conn.Conn(), "534_drop_comment_agent_delivery.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "534_drop_comment_agent_delivery.down.sql")
	assertCommentAgentDeliveryTableMissing(t, ctx, conn)
}

func assertCommentAgentDeliveryTableMissing(t *testing.T, ctx context.Context, conn *pgxpool.Conn) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, `
		SELECT to_regclass($1) IS NOT NULL
	`, commentAgentDeliveryRollbackTestSchema+".comment_agent_delivery").Scan(&exists); err != nil {
		t.Fatalf("inspect comment_agent_delivery table: %v", err)
	}
	if exists {
		t.Fatal("comment_agent_delivery table still exists after rollback migration")
	}
}
