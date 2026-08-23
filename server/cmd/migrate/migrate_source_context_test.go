package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestSourceContextMigrationsRoundTripWithDocumentedDataLoss(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_source_context_" + suffix
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schemaIdent); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaIdent+" CASCADE"); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})

	pool := openTestPoolWithSearchPath(t, schema)
	if _, err := pool.Exec(ctx, `CREATE TABLE attachment (id UUID NOT NULL)`); err != nil {
		t.Fatalf("create attachment fixture: %v", err)
	}

	versions := []string{
		"407_issue_source_context",
		"408_issue_source_context_id_index",
		"409_issue_source_context_issue_index",
		"410_issue_source_context_origin_task_index",
		"411_attachment_source_context_index",
		"412_issue_source_context_object_intent_key_index",
		"413_issue_source_context_object_intent_due_index",
		"414_issue_source_context_object_intent_context_index",
	}
	options := runOptions{
		Direction:             "up",
		Files:                 realMigrationFiles(t, versions, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("apply source-context migrations: %v", err)
	}

	assertSourceContextSchema(t, pool, schema, true)
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue_source_context (
			id, workspace_id, issue_id, origin_task_id, source_issue_id,
			anchor_comment_id, captured_by_user_id, snapshot_version,
			snapshot, capture_digest, state, attached_at
		) VALUES (
			'00000000-0000-0000-0000-000000000001',
			'00000000-0000-0000-0000-000000000002',
			'00000000-0000-0000-0000-000000000003',
			NULL,
			'00000000-0000-0000-0000-000000000004',
			'00000000-0000-0000-0000-000000000005',
			'00000000-0000-0000-0000-000000000006',
			1,
			'{}'::jsonb,
			'digest',
			'attached',
			now()
		)
	`); err != nil {
		t.Fatalf("insert source-context fixture: %v", err)
	}

	reversed := append([]string(nil), versions...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	options.Direction = "down"
	options.Files = realMigrationFiles(t, reversed, "down")
	options.Hooks = hooksForDirection("down")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("roll back source-context migrations: %v", err)
	}

	assertSourceContextSchema(t, pool, schema, false)
	var attachmentExists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+".attachment").Scan(&attachmentExists); err != nil {
		t.Fatalf("read attachment table existence after rollback: %v", err)
	}
	if !attachmentExists {
		t.Fatal("attachment table was removed by source-context rollback")
	}

	options.Direction = "up"
	options.Files = realMigrationFiles(t, versions, "up")
	options.Hooks = hooksForDirection("up")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("reapply source-context migrations: %v", err)
	}
	assertSourceContextSchema(t, pool, schema, true)

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue_source_context`).Scan(&count); err != nil {
		t.Fatalf("count source contexts after reapply: %v", err)
	}
	if count != 0 {
		t.Fatalf("source contexts after destructive rollback and reapply = %d, want 0", count)
	}
}

func assertSourceContextSchema(t *testing.T, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, schema string, want bool) {
	t.Helper()
	ctx := context.Background()
	for _, relation := range []string{"issue_source_context", "issue_source_context_object_intent"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+relation).Scan(&exists); err != nil {
			t.Fatalf("read %s existence: %v", relation, err)
		}
		if exists != want {
			t.Fatalf("%s existence = %v, want %v", relation, exists, want)
		}
	}

	var columnExists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = $1
			  AND table_name = 'attachment'
			  AND column_name = 'source_context_id'
		)
	`, schema).Scan(&columnExists); err != nil {
		t.Fatalf("read attachment.source_context_id existence: %v", err)
	}
	if columnExists != want {
		t.Fatalf("attachment.source_context_id existence = %v, want %v", columnExists, want)
	}
}
