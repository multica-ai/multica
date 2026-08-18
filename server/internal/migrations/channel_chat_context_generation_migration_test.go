package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const channelContextMigrationTestSchema = "channel_context_migration_test"

func TestChannelChatContextGenerationMigrationsUpDownAndLegacyRows(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+channelContextMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+channelContextMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, channelContextMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE channel_chat_session_binding (
			chat_session_id UUID NOT NULL,
			pending_fresh BOOLEAN NOT NULL DEFAULT FALSE
		);
		CREATE TABLE chat_message (
			id UUID NOT NULL,
			chat_session_id UUID NOT NULL,
			role TEXT NOT NULL,
			channel_ingested BOOLEAN NOT NULL DEFAULT FALSE,
			task_id UUID
		);
		CREATE TABLE agent_task_queue (
			id UUID NOT NULL,
			chat_session_id UUID,
			parent_task_id UUID,
			chat_input_task_id UUID,
			retry_of_task_id UUID,
			rerun_of_task_id UUID
		);
	`); err != nil {
		t.Fatalf("create pre-migration tables: %v", err)
	}

	const sessionID = "c3440000-0000-4000-8000-000000000001"
	if _, err := conn.Exec(ctx, `INSERT INTO channel_chat_session_binding (chat_session_id) VALUES ($1)`, sessionID); err != nil {
		t.Fatalf("seed pre-migration binding: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO chat_message (id, chat_session_id, role) VALUES
			('c3440000-0000-4000-8000-000000000002', $1, 'user'),
			('c3440000-0000-4000-8000-000000000003', $1, 'assistant')
	`, sessionID); err != nil {
		t.Fatalf("seed pre-migration messages: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO agent_task_queue (id, chat_session_id)
		VALUES ('c3440000-0000-4000-8000-000000000004', $1)
	`, sessionID); err != nil {
		t.Fatalf("seed pre-migration task: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "377_channel_chat_context_generation.up.sql")
	// The migration runner records each version after executing its SQL. If the
	// ledger write fails, the next startup executes the same file again.
	applyMigrationFile(t, ctx, conn.Conn(), "377_channel_chat_context_generation.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "378_channel_chat_context_generation_key.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "378_channel_chat_context_generation_key.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "379_channel_context_mixed_version_guard.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "379_channel_context_mixed_version_guard.up.sql")

	var bindingRevision, generationRevision int64
	if err := conn.QueryRow(ctx, `
		SELECT binding.context_revision, generation.revision
		FROM channel_chat_session_binding AS binding
		JOIN channel_chat_context_generation AS generation
		  ON generation.chat_session_id = binding.chat_session_id
		WHERE binding.chat_session_id = $1
	`, sessionID).Scan(&bindingRevision, &generationRevision); err != nil {
		t.Fatalf("read backfilled generation: %v", err)
	}
	if bindingRevision != 1 || generationRevision != 1 {
		t.Fatalf("backfilled revisions = binding:%d generation:%d, want 1/1", bindingRevision, generationRevision)
	}

	var userRevision, assistantRevision *int64
	if err := conn.QueryRow(ctx, `
		SELECT
			MAX(channel_context_revision) FILTER (WHERE role = 'user'),
			MAX(channel_context_revision) FILTER (WHERE role = 'assistant')
		FROM chat_message
		WHERE chat_session_id = $1
	`, sessionID).Scan(&userRevision, &assistantRevision); err != nil {
		t.Fatalf("read message backfill: %v", err)
	}
	if userRevision != nil {
		t.Fatalf("legacy user message revision = %v, want NULL (readers treat it as revision 1)", *userRevision)
	}
	if assistantRevision != nil {
		t.Fatalf("assistant message revision = %v, want NULL (derived from its task)", *assistantRevision)
	}

	var taskRevision *int64
	if err := conn.QueryRow(ctx, `SELECT channel_context_revision FROM agent_task_queue WHERE chat_session_id = $1`, sessionID).Scan(&taskRevision); err != nil {
		t.Fatalf("read task backfill: %v", err)
	}
	if taskRevision != nil {
		t.Fatalf("legacy task revision = %v, want NULL (readers treat it as revision 1)", *taskRevision)
	}

	assertChannelContextMixedVersionGuards(t, ctx, conn.Conn(), sessionID)

	assertChannelContextIndex(t, ctx, conn.Conn())
	if _, err := conn.Exec(ctx, `
		INSERT INTO channel_chat_context_generation (chat_session_id, revision)
		VALUES ($1, 1)
	`, sessionID); !isUniqueViolationMigration(err) {
		t.Fatalf("duplicate generation error = %v, want unique violation", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "379_channel_context_mixed_version_guard.down.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "378_channel_chat_context_generation_key.down.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "377_channel_chat_context_generation.down.sql")

	var generationExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, channelContextMigrationTestSchema+".channel_chat_context_generation").Scan(&generationExists); err != nil {
		t.Fatalf("inspect rolled-back generation table: %v", err)
	}
	if generationExists {
		t.Fatal("channel_chat_context_generation still exists after down migrations")
	}
	for _, column := range []string{
		"channel_chat_session_binding.context_revision",
		"chat_message.channel_context_revision",
		"agent_task_queue.channel_context_revision",
	} {
		var exists bool
		if err := conn.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_schema = $1
				  AND table_name = split_part($2, '.', 1)
				  AND column_name = split_part($2, '.', 2)
			)
		`, channelContextMigrationTestSchema, column).Scan(&exists); err != nil {
			t.Fatalf("inspect rolled-back column %s: %v", column, err)
		}
		if exists {
			t.Fatalf("column %s still exists after down migrations", column)
		}
	}
}

func assertChannelContextMixedVersionGuards(t *testing.T, ctx context.Context, conn *pgx.Conn, sessionID string) {
	t.Helper()
	const newMessageID = "c3440000-0000-4000-8000-000000000006"
	const taskID = "c3440000-0000-4000-8000-000000000007"
	const retryTaskID = "c3440000-0000-4000-8000-000000000008"

	statements := []struct {
		sql  string
		args []any
	}{
		{`UPDATE channel_chat_session_binding SET context_revision = 2, pending_fresh = TRUE WHERE chat_session_id = $1`, []any{sessionID}},
		{`INSERT INTO channel_chat_context_generation (chat_session_id, revision, pending_fresh) VALUES ($1, 2, TRUE)`, []any{sessionID}},
		{`INSERT INTO chat_message (id, chat_session_id, role) VALUES ($2, $1, 'user')`, []any{sessionID, newMessageID}},
		{`INSERT INTO agent_task_queue (id, chat_session_id) VALUES ($2, $1)`, []any{sessionID, taskID}},
		{`INSERT INTO agent_task_queue (id, chat_session_id, parent_task_id) VALUES ($2, $1, $3)`, []any{sessionID, retryTaskID, "c3440000-0000-4000-8000-000000000004"}},
	}
	for _, statement := range statements {
		if _, err := conn.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("simulate old writers at revision 2: %v", err)
		}
	}

	var messageRevision, taskRevision int64
	if err := conn.QueryRow(ctx, `
		SELECT message.channel_context_revision, task.channel_context_revision
		FROM chat_message AS message
		JOIN agent_task_queue AS task ON task.id = $2
		WHERE message.id = $1
	`, newMessageID, taskID).Scan(&messageRevision, &taskRevision); err != nil {
		t.Fatalf("read old-writer revision stamps: %v", err)
	}
	if messageRevision != 2 || taskRevision != 2 {
		t.Fatalf("old-writer revisions = message:%d task:%d, want 2/2", messageRevision, taskRevision)
	}
	var retryRevision int64
	if err := conn.QueryRow(ctx, `SELECT channel_context_revision FROM agent_task_queue WHERE id = $1`, retryTaskID).Scan(&retryRevision); err != nil {
		t.Fatalf("read old-writer retry revision: %v", err)
	}
	if retryRevision != 1 {
		t.Fatalf("old-writer retry revision = %d, want inherited legacy revision 1", retryRevision)
	}

	if _, err := conn.Exec(ctx, `
		UPDATE chat_message SET task_id = $1
		WHERE chat_session_id = $2 AND role = 'user' AND task_id IS NULL
	`, taskID, sessionID); err != nil {
		t.Fatalf("simulate old unscoped batch seal: %v", err)
	}

	var newOwner *string
	if err := conn.QueryRow(ctx, `
		SELECT MAX(task_id::text) FILTER (WHERE id = $1)
		FROM chat_message
	`, newMessageID).Scan(&newOwner); err != nil {
		t.Fatalf("read mixed-version batch ownership: %v", err)
	}
	var legacyOwner *string
	if err := conn.QueryRow(ctx, `SELECT task_id::text FROM chat_message WHERE id = $1`, "c3440000-0000-4000-8000-000000000002").Scan(&legacyOwner); err != nil {
		t.Fatalf("read legacy message owner: %v", err)
	}
	if legacyOwner != nil {
		t.Fatalf("revision-1 legacy message crossed into revision-2 task: owner=%s", *legacyOwner)
	}
	if newOwner == nil || *newOwner != taskID {
		t.Fatalf("revision-2 message owner = %v, want %s", newOwner, taskID)
	}

	if _, err := conn.Exec(ctx, `
		UPDATE channel_chat_session_binding
		SET pending_fresh = FALSE
		WHERE chat_session_id = $1
	`, sessionID); err != nil {
		t.Fatalf("simulate old pending-fresh clear: %v", err)
	}
	var generationPending bool
	if err := conn.QueryRow(ctx, `
		SELECT pending_fresh
		FROM channel_chat_context_generation
		WHERE chat_session_id = $1 AND revision = 2
	`, sessionID).Scan(&generationPending); err != nil {
		t.Fatalf("read synchronized generation pending-fresh: %v", err)
	}
	if generationPending {
		t.Fatal("old binding clear did not clear generation pending_fresh")
	}
}

func assertChannelContextIndex(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var unique, valid, ready bool
	err := conn.QueryRow(ctx, `
		SELECT index.indisunique, index.indisvalid, index.indisready
		FROM pg_index AS index
		JOIN pg_class AS relation ON relation.oid = index.indexrelid
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = $1
		  AND relation.relname = 'channel_chat_context_generation_session_revision_idx'
	`, channelContextMigrationTestSchema).Scan(&unique, &valid, &ready)
	if err != nil {
		t.Fatalf("inspect generation index: %v", err)
	}
	if !unique || !valid || !ready {
		t.Fatalf("generation index flags = unique:%t valid:%t ready:%t, want all true", unique, valid, ready)
	}
}
