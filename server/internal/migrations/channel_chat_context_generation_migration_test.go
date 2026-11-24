package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
			status TEXT NOT NULL DEFAULT 'queued',
			parent_task_id UUID,
			chat_input_task_id UUID,
			retry_of_task_id UUID,
			rerun_of_task_id UUID
		);
	`); err != nil {
		t.Fatalf("create pre-migration tables: %v", err)
	}

	const sessionID = "c3440000-0000-4000-8000-000000000001"
	const directSessionID = "c3440000-0000-4000-8000-000000000009"
	const directTaskID = "c3440000-0000-4000-8000-000000000010"
	const terminalTaskID = "c3440000-0000-4000-8000-000000000011"
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
		INSERT INTO agent_task_queue (id, chat_session_id, status)
		VALUES ('c3440000-0000-4000-8000-000000000004', $1, 'queued'),
		       ($2, $3, 'queued'),
		       ($4, $1, 'completed'),
		       ('c3440000-0000-4000-8000-000000000020', $1, 'dispatched'),
		       ('c3440000-0000-4000-8000-000000000021', $1, 'running'),
		       ('c3440000-0000-4000-8000-000000000022', $1, 'waiting_local_directory'),
		       ('c3440000-0000-4000-8000-000000000023', $1, 'deferred')
	`, sessionID, directTaskID, directSessionID, terminalTaskID); err != nil {
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

	var backfilled int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id = $1 AND channel_context_revision = 1`, sessionID).Scan(&backfilled); err != nil {
		t.Fatalf("read task backfill: %v", err)
	}
	if backfilled != 5 {
		t.Fatalf("backfilled in-flight channel tasks = %d, want 5", backfilled)
	}
	var terminalTaskRevision *int64
	if err := conn.QueryRow(ctx, `SELECT channel_context_revision FROM agent_task_queue WHERE id = $1`, terminalTaskID).Scan(&terminalTaskRevision); err != nil {
		t.Fatalf("read terminal task backfill: %v", err)
	}
	if terminalTaskRevision != nil {
		t.Fatalf("terminal channel task revision = %v, want NULL", *terminalTaskRevision)
	}
	var directTaskRevision *int64
	if err := conn.QueryRow(ctx, `SELECT channel_context_revision FROM agent_task_queue WHERE id = $1`, directTaskID).Scan(&directTaskRevision); err != nil {
		t.Fatalf("read direct task backfill: %v", err)
	}
	if directTaskRevision != nil {
		t.Fatalf("legacy direct task revision = %v, want NULL", *directTaskRevision)
	}

	assertChannelContextOwnershipGuard(t, ctx, conn.Conn(), sessionID, directSessionID, directTaskID)

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
		"chat_message.channel_outbound_type",
		"chat_message.channel_outbound_installation_id",
		"chat_message.channel_outbound_chat_id",
		"chat_message.channel_outbound_message_ids",
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

func assertChannelContextOwnershipGuard(t *testing.T, ctx context.Context, conn *pgx.Conn, sessionID, directSessionID, directTaskID string) {
	t.Helper()
	const newMessageID = "c3440000-0000-4000-8000-000000000006"
	const taskID = "c3440000-0000-4000-8000-000000000007"
	const retryTaskID = "c3440000-0000-4000-8000-000000000008"
	const rerunTaskID = "c3440000-0000-4000-8000-000000000009"

	statements := []struct {
		sql  string
		args []any
	}{
		{`UPDATE channel_chat_session_binding SET context_revision = 2, pending_fresh = TRUE WHERE chat_session_id = $1`, []any{sessionID}},
		{`INSERT INTO channel_chat_context_generation (chat_session_id, revision, pending_fresh) VALUES ($1, 2, TRUE)`, []any{sessionID}},
		{`INSERT INTO chat_message (id, chat_session_id, role, channel_context_revision) VALUES ($2, $1, 'user', 2)`, []any{sessionID, newMessageID}},
		{`INSERT INTO agent_task_queue (id, chat_session_id, channel_context_revision) VALUES ($2, $1, 2)`, []any{sessionID, taskID}},
		{`INSERT INTO agent_task_queue (id, chat_session_id, retry_of_task_id) VALUES ($2, $1, $3)`, []any{directSessionID, retryTaskID, directTaskID}},
		{`INSERT INTO agent_task_queue (id, chat_session_id, rerun_of_task_id) VALUES ($2, $1, $3)`, []any{directSessionID, rerunTaskID, directTaskID}},
	}
	for _, statement := range statements {
		if _, err := conn.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed ownership guard fixture: %v", err)
		}
	}

	for _, id := range []string{retryTaskID, rerunTaskID} {
		var revision *int64
		if err := conn.QueryRow(ctx, `SELECT channel_context_revision FROM agent_task_queue WHERE id = $1`, id).Scan(&revision); err != nil {
			t.Fatalf("read direct retry/rerun revision: %v", err)
		}
		if revision != nil {
			t.Fatalf("direct retry/rerun %s revision = %v, want NULL", id, *revision)
		}
	}

	if _, err := conn.Exec(ctx, `
		UPDATE chat_message SET task_id = $1
		WHERE chat_session_id = $2 AND role = 'user' AND task_id IS NULL
	`, taskID, sessionID); err != nil {
		t.Fatalf("seal generation-2 batch: %v", err)
	}

	var newOwner *string
	if err := conn.QueryRow(ctx, `
		SELECT task_id::text
		FROM chat_message
		WHERE id = $1
	`, newMessageID).Scan(&newOwner); err != nil {
		t.Fatalf("read generation-2 batch ownership: %v", err)
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
		INSERT INTO chat_message (id, chat_session_id, role, task_id)
		VALUES ('c3440000-0000-4000-8000-000000000030', $1, 'assistant', $2)
	`, sessionID, taskID); err != nil {
		t.Fatalf("seed assistant outbound row: %v", err)
	}
	q := db.New(conn)
	rows, err := q.SetChatMessageChannelOutboundProvenanceByTask(ctx, db.SetChatMessageChannelOutboundProvenanceByTaskParams{
		ChannelType:    pgtype.Text{String: "slack", Valid: true},
		InstallationID: util.MustParseUUID("c3440000-0000-4000-8000-000000000040"),
		ChannelChatID:  pgtype.Text{String: "C123", Valid: true},
		MessageIds:     []string{"104.000000", "105.000000"},
		TaskID:         util.MustParseUUID(taskID),
	})
	if err != nil || rows != 1 {
		t.Fatalf("record outbound provenance: rows=%d err=%v", rows, err)
	}
	ids, err := q.ListChannelOutboundMessageIDsForContext(ctx, db.ListChannelOutboundMessageIDsForContextParams{
		ChatSessionID:          util.MustParseUUID(sessionID),
		ChannelType:            pgtype.Text{String: "slack", Valid: true},
		InstallationID:         util.MustParseUUID("c3440000-0000-4000-8000-000000000040"),
		ChannelChatID:          pgtype.Text{String: "C123", Valid: true},
		ChannelContextRevision: pgtype.Int8{Int64: 2, Valid: true},
		CandidateMessageIds:    []string{"103.000000", "104.000000", "105.000000"},
	})
	if err != nil {
		t.Fatalf("list outbound provenance: %v", err)
	}
	if len(ids) != 2 || ids[0] != "104.000000" || ids[1] != "105.000000" {
		t.Fatalf("outbound provenance ids = %v", ids)
	}
	otherTargetIDs, err := q.ListChannelOutboundMessageIDsForContext(ctx, db.ListChannelOutboundMessageIDsForContextParams{
		ChatSessionID:          util.MustParseUUID(sessionID),
		ChannelType:            pgtype.Text{String: "slack", Valid: true},
		InstallationID:         util.MustParseUUID("c3440000-0000-4000-8000-000000000041"),
		ChannelChatID:          pgtype.Text{String: "C123", Valid: true},
		ChannelContextRevision: pgtype.Int8{Int64: 2, Valid: true},
		CandidateMessageIds:    []string{"104.000000"},
	})
	if err != nil {
		t.Fatalf("list outbound provenance for other installation: %v", err)
	}
	if len(otherTargetIDs) != 0 {
		t.Fatalf("other installation reused outbound ids = %v", otherTargetIDs)
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
