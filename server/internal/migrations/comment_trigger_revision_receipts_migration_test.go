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
			status TEXT NOT NULL,
			dispatched_at TIMESTAMPTZ,
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ
		);
		INSERT INTO comment (id, revision, updated_at) VALUES
			('00000000-0000-4000-8000-000000000001', 5, now()-interval '1 minute'),
			('00000000-0000-4000-8000-000000000004', 6, now()-interval '1 minute'),
			('00000000-0000-4000-8000-000000000006', 7, now()-interval '20 seconds');
		INSERT INTO agent (id) VALUES ('00000000-0000-4000-8000-000000000002');
		INSERT INTO comment_trigger_outbox (comment_id)
		VALUES ('00000000-0000-4000-8000-000000000001');
		INSERT INTO agent_task_queue (
			id, agent_id, delivered_comment_ids, status,
			dispatched_at, started_at, completed_at
		)
		VALUES
			(
				'00000000-0000-4000-8000-000000000003',
				'00000000-0000-4000-8000-000000000002',
				ARRAY['00000000-0000-4000-8000-000000000001'::uuid],
				'completed',
				now()-interval '30 seconds',
				now()-interval '20 seconds',
				now()-interval '10 seconds'
			),
			(
				'00000000-0000-4000-8000-000000000005',
				'00000000-0000-4000-8000-000000000002',
				ARRAY['00000000-0000-4000-8000-000000000004'::uuid],
				'running',
				now()-interval '30 seconds',
				now()-interval '20 seconds',
				NULL
			),
			(
				'00000000-0000-4000-8000-000000000007',
				'00000000-0000-4000-8000-000000000002',
				ARRAY['00000000-0000-4000-8000-000000000006'::uuid],
				'completed',
				now()-interval '30 seconds',
				now()-interval '15 seconds',
				now()-interval '10 seconds'
			);
	`); err != nil {
		t.Fatalf("create pre-migration fixtures: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "513_comment_trigger_revision_receipts.up.sql")
	var commentRevision, outboxRevision, snapshotRevision, receiptRevision int64
	if err := conn.QueryRow(ctx, `
		SELECT source.trigger_revision, outbox.comment_trigger_revision,
		       snapshot.comment_trigger_revision, receipt.comment_trigger_revision
		FROM comment source
		JOIN comment_trigger_outbox outbox ON outbox.comment_id=source.id
		JOIN agent_task_comment_delivery_snapshot snapshot ON snapshot.comment_id=source.id
		JOIN comment_trigger_delivery_receipt receipt ON receipt.comment_id=source.id
	`).Scan(&commentRevision, &outboxRevision, &snapshotRevision, &receiptRevision); err != nil {
		t.Fatalf("read migrated trigger revisions: %v", err)
	}
	if commentRevision != 5 || outboxRevision != 5 || snapshotRevision != 5 || receiptRevision != 5 {
		t.Fatalf("migrated trigger revisions = %d/%d/%d/%d, want 5/5/5/5", commentRevision, outboxRevision, snapshotRevision, receiptRevision)
	}
	// Model an installation that already ran the old claim-stage migration:
	// the running task has an immutable receipt even though its claim can still
	// be replaced. Migration 514 must discard that false evidence.
	if _, err := conn.Exec(ctx, `
		INSERT INTO comment_trigger_delivery_receipt (
			comment_id, comment_trigger_revision, agent_id, task_id
		) VALUES (
			'00000000-0000-4000-8000-000000000004', 6,
			'00000000-0000-4000-8000-000000000002',
			'00000000-0000-4000-8000-000000000005'
		)
	`); err != nil {
		t.Fatalf("create old claim-stage receipt: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "514_comment_delivery_snapshot_lifecycle.up.sql")
	var snapshots, receipts int
	if err := conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM agent_task_comment_delivery_snapshot),
			(SELECT count(*) FROM comment_trigger_delivery_receipt)
	`).Scan(&snapshots, &receipts); err != nil {
		t.Fatalf("read post-514 evidence: %v", err)
	}
	if snapshots != 2 || receipts != 1 {
		t.Fatalf("post-514 evidence = snapshots:%d receipts:%d, want 2/1", snapshots, receipts)
	}
	var runningReceipts, postDispatchSnapshots int
	if err := conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM comment_trigger_delivery_receipt
			 WHERE comment_id='00000000-0000-4000-8000-000000000004'),
			(SELECT count(*) FROM agent_task_comment_delivery_snapshot
			 WHERE comment_id='00000000-0000-4000-8000-000000000006')
	`).Scan(&runningReceipts, &postDispatchSnapshots); err != nil {
		t.Fatalf("read conservative migration evidence: %v", err)
	}
	if runningReceipts != 0 || postDispatchSnapshots != 0 {
		t.Fatalf("unsafe migrated evidence = running receipts:%d post-dispatch snapshots:%d, want 0/0", runningReceipts, postDispatchSnapshots)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "514_comment_delivery_snapshot_lifecycle.down.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "513_comment_trigger_revision_receipts.down.sql")
	var remainingColumns, remainingReceiptTables int
	if err := conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM information_schema.columns
			 WHERE table_schema=$1 AND column_name IN ('trigger_revision', 'comment_trigger_revision')),
			(SELECT count(*) FROM information_schema.tables
			 WHERE table_schema=$1 AND table_name IN (
			     'comment_trigger_delivery_receipt',
			     'agent_task_comment_delivery_snapshot'
			 ))
	`, commentTriggerRevisionMigrationTestSchema).Scan(&remainingColumns, &remainingReceiptTables); err != nil {
		t.Fatalf("inspect rollback: %v", err)
	}
	if remainingColumns != 0 || remainingReceiptTables != 0 {
		t.Fatalf("rollback left trigger columns=%d receipt tables=%d", remainingColumns, remainingReceiptTables)
	}
}
