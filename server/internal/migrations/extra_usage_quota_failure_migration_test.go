package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const extraUsageQuotaFailureMigrationTestSchema = "extra_usage_quota_failure_migration_test"

func TestExtraUsageQuotaFailureBackfillIsSelectiveAndIdempotent(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+extraUsageQuotaFailureMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+extraUsageQuotaFailureMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, extraUsageQuotaFailureMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE agent_task_queue (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			error TEXT,
			failure_reason TEXT
		);
		CREATE TABLE chat_message (
			id TEXT PRIMARY KEY,
			content TEXT,
			failure_reason TEXT
		);

		INSERT INTO agent_task_queue (id, status, error, failure_reason) VALUES
			('task-unknown', 'failed', 'You''re out of extra usage · resets 4am', 'agent_error.unknown'),
			('task-process', 'failed', 'claude exited: OUT OF EXTRA USAGE', 'agent_error.process_failure'),
			('task-coarse', 'failed', 'out of extra usage', 'agent_error'),
			('task-refined', 'failed', 'out of extra usage', 'agent_error.provider_auth_or_access'),
			('task-completed', 'completed', 'out of extra usage', 'agent_error.unknown'),
			('task-unrelated', 'failed', 'connection reset', 'agent_error.unknown');

		INSERT INTO chat_message (id, content, failure_reason) VALUES
			('chat-unknown', 'You''re out of extra usage · resets 4am', 'agent_error.unknown'),
			('chat-process', 'claude exited: OUT OF EXTRA USAGE', 'agent_error.process_failure'),
			('chat-coarse', 'out of extra usage', 'agent_error'),
			('chat-refined', 'out of extra usage', 'agent_error.provider_auth_or_access'),
			('chat-unrelated', 'connection reset', 'agent_error.unknown');
	`); err != nil {
		t.Fatalf("create pre-migration fixtures: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "445_backfill_extra_usage_quota_failure.up.sql")
	assertFailureReason(t, ctx, conn.Conn(), "agent_task_queue", "task-unknown", "agent_error.provider_quota_limit")
	assertFailureReason(t, ctx, conn.Conn(), "agent_task_queue", "task-process", "agent_error.provider_quota_limit")
	assertFailureReason(t, ctx, conn.Conn(), "agent_task_queue", "task-coarse", "agent_error.provider_quota_limit")
	assertFailureReason(t, ctx, conn.Conn(), "agent_task_queue", "task-refined", "agent_error.provider_auth_or_access")
	assertFailureReason(t, ctx, conn.Conn(), "agent_task_queue", "task-completed", "agent_error.unknown")
	assertFailureReason(t, ctx, conn.Conn(), "agent_task_queue", "task-unrelated", "agent_error.unknown")
	assertFailureReason(t, ctx, conn.Conn(), "chat_message", "chat-unknown", "agent_error.provider_quota_limit")
	assertFailureReason(t, ctx, conn.Conn(), "chat_message", "chat-process", "agent_error.provider_quota_limit")
	assertFailureReason(t, ctx, conn.Conn(), "chat_message", "chat-coarse", "agent_error.provider_quota_limit")
	assertFailureReason(t, ctx, conn.Conn(), "chat_message", "chat-refined", "agent_error.provider_auth_or_access")
	assertFailureReason(t, ctx, conn.Conn(), "chat_message", "chat-unrelated", "agent_error.unknown")

	// The backfill is a data-quality ratchet. Down is intentionally a no-op so
	// it cannot misclassify quota rows written organically by the new version.
	applyMigrationFile(t, ctx, conn.Conn(), "445_backfill_extra_usage_quota_failure.down.sql")
	assertFailureReason(t, ctx, conn.Conn(), "agent_task_queue", "task-unknown", "agent_error.provider_quota_limit")
	assertFailureReason(t, ctx, conn.Conn(), "chat_message", "chat-unknown", "agent_error.provider_quota_limit")

	// Reapplying up must leave the repaired data unchanged.
	applyMigrationFile(t, ctx, conn.Conn(), "445_backfill_extra_usage_quota_failure.up.sql")
	assertFailureReason(t, ctx, conn.Conn(), "agent_task_queue", "task-unknown", "agent_error.provider_quota_limit")
	assertFailureReason(t, ctx, conn.Conn(), "chat_message", "chat-unknown", "agent_error.provider_quota_limit")
}

func assertFailureReason(t *testing.T, ctx context.Context, conn *pgx.Conn, table, id, want string) {
	t.Helper()
	var got string
	if err := conn.QueryRow(ctx, "SELECT failure_reason FROM "+table+" WHERE id = $1", id).Scan(&got); err != nil {
		t.Fatalf("read %s %s failure_reason: %v", table, id, err)
	}
	if got != want {
		t.Fatalf("%s %s failure_reason = %q, want %q", table, id, got, want)
	}
}
