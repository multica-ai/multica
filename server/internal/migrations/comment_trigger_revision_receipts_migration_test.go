package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const commentTriggerRevisionMigrationTestSchema = "comment_trigger_revision_migration_test"

func TestCommentTriggerRevisionReceiptMigrationUpDown(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+commentTriggerRevisionMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+commentTriggerRevisionMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, commentTriggerRevisionMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	if _, err := conn.Exec(ctx, `
		CREATE TABLE comment (
			id UUID PRIMARY KEY,
			revision BIGINT NOT NULL DEFAULT 1,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE agent (id UUID PRIMARY KEY);
		CREATE TABLE comment_trigger_outbox (comment_id UUID PRIMARY KEY REFERENCES comment(id));
		CREATE TABLE agent_task_queue (
			id UUID PRIMARY KEY,
			agent_id UUID NOT NULL REFERENCES agent(id),
			delivered_comment_ids UUID[] NOT NULL DEFAULT '{}',
			dispatched_at TIMESTAMPTZ
		);
		INSERT INTO comment (id, revision, updated_at)
		VALUES ('00000000-0000-4000-8000-000000000001', 5, now()-interval '1 minute');
		INSERT INTO agent (id) VALUES ('00000000-0000-4000-8000-000000000002');
		INSERT INTO comment_trigger_outbox (comment_id)
		VALUES ('00000000-0000-4000-8000-000000000001');
		INSERT INTO agent_task_queue (id, agent_id, delivered_comment_ids, dispatched_at)
		VALUES (
			'00000000-0000-4000-8000-000000000003',
			'00000000-0000-4000-8000-000000000002',
			ARRAY['00000000-0000-4000-8000-000000000001'::uuid],
			now()
		);
	`); err != nil {
		t.Fatalf("create pre-migration fixtures: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "513_comment_trigger_revision_receipts.up.sql")
	var commentRevision, outboxRevision, receiptRevision int64
	if err := conn.QueryRow(ctx, `
		SELECT source.trigger_revision, outbox.comment_trigger_revision, receipt.comment_trigger_revision
		FROM comment source
		JOIN comment_trigger_outbox outbox ON outbox.comment_id=source.id
		JOIN comment_trigger_delivery_receipt receipt ON receipt.comment_id=source.id
	`).Scan(&commentRevision, &outboxRevision, &receiptRevision); err != nil {
		t.Fatalf("read migrated trigger revisions: %v", err)
	}
	if commentRevision != 5 || outboxRevision != 5 || receiptRevision != 5 {
		t.Fatalf("migrated trigger revisions = %d/%d/%d, want 5/5/5", commentRevision, outboxRevision, receiptRevision)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "513_comment_trigger_revision_receipts.down.sql")
	var remainingColumns, remainingReceiptTables int
	if err := conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM information_schema.columns
			 WHERE table_schema=$1 AND column_name IN ('trigger_revision', 'comment_trigger_revision')),
			(SELECT count(*) FROM information_schema.tables
			 WHERE table_schema=$1 AND table_name='comment_trigger_delivery_receipt')
	`, commentTriggerRevisionMigrationTestSchema).Scan(&remainingColumns, &remainingReceiptTables); err != nil {
		t.Fatalf("inspect rollback: %v", err)
	}
	if remainingColumns != 0 || remainingReceiptTables != 0 {
		t.Fatalf("rollback left trigger columns=%d receipt tables=%d", remainingColumns, remainingReceiptTables)
	}
}
