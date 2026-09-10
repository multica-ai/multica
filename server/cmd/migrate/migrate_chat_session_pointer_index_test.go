package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestChatSessionPointerIndexBoundsNewerTaskGuard(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_chat_pointer_idx_" + suffix
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
	for _, statement := range []string{
		`CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE chat_session (
			id UUID PRIMARY KEY,
			session_id TEXT,
			runtime_id UUID,
			work_dir TEXT,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE agent_task_queue (
			id UUID PRIMARY KEY,
			chat_session_id UUID,
			status TEXT NOT NULL,
			session_id TEXT,
			runtime_id UUID,
			work_dir TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`INSERT INTO chat_session (
			id, session_id, runtime_id, work_dir
		) VALUES (
			'00000000-0000-0000-0000-000000000001',
			'previous-session',
			'00000000-0000-0000-0000-000000000003',
			'/previous-work'
		)`,
		`INSERT INTO agent_task_queue (
			id, chat_session_id, status, session_id, runtime_id, created_at
		)
		SELECT
			gen_random_uuid(),
			NULL,
			'completed',
			'issue-session-' || n,
			'00000000-0000-0000-0000-000000000003',
			now() - make_interval(secs => n)
		FROM generate_series(1, 5000) AS n`,
		`INSERT INTO agent_task_queue (
			id, chat_session_id, status, session_id, runtime_id, created_at
		)
		SELECT
			gen_random_uuid(),
			('10000000-0000-0000-0000-' || lpad(((n % 500) + 1)::text, 12, '0'))::uuid,
			CASE WHEN n % 2 = 0 THEN 'running' ELSE 'completed' END,
			'chat-session-' || n,
			'00000000-0000-0000-0000-000000000003',
			now() - make_interval(secs => n)
		FROM generate_series(1, 5000) AS n`,
		`INSERT INTO agent_task_queue (
			id, chat_session_id, status, session_id, runtime_id, work_dir, created_at
		) VALUES (
			'00000000-0000-0000-0000-000000000002',
			'00000000-0000-0000-0000-000000000001',
			'cancelled',
			'cancelled-session',
			'00000000-0000-0000-0000-000000000003',
			'/cancelled-work',
			now() - interval '1 day'
		)`,
		`ANALYZE chat_session`,
		`ANALYZE agent_task_queue`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply fixture statement: %v", err)
		}
	}

	const (
		version       = "461_agent_task_queue_chat_with_session_index"
		indexName     = "idx_agent_task_queue_chat_with_session_created_at"
		guardedUpdate = `UPDATE chat_session cs
			SET session_id = t.session_id,
			    runtime_id = t.runtime_id,
			    work_dir = COALESCE(t.work_dir, cs.work_dir),
			    updated_at = now()
			FROM agent_task_queue t
			WHERE t.id = '00000000-0000-0000-0000-000000000002'
			  AND t.chat_session_id = cs.id
			  AND t.status = 'cancelled'
			  AND t.session_id IS NOT NULL
			  AND t.runtime_id IS NOT NULL
			  AND NOT EXISTS (
			      SELECT 1
			      FROM agent_task_queue newer
			      WHERE newer.chat_session_id = t.chat_session_id
			        AND newer.id <> t.id
			        AND newer.session_id IS NOT NULL
			        AND newer.created_at > t.created_at
			  )`
	)

	options := runOptions{
		Direction:             "up",
		Files:                 realMigrationFiles(t, []string{version}, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
	}
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("apply chat pointer index migration: %v", err)
	}
	assertIndexValidity(t, pool, schema, indexName, true)
	assertMigrationVersionRecorded(t, ctx, pool, schema, version, true)
	assertPlanUsesIndex(t, pool, guardedUpdate, indexName)

	options.Direction = "down"
	options.Files = realMigrationFiles(t, []string{version}, "down")
	options.Hooks = hooksForDirection("down")
	if err := runMigrations(ctx, pool, options); err != nil {
		t.Fatalf("roll back chat pointer index migration: %v", err)
	}
	assertIndexExists(t, pool, schema, indexName, false)
	assertMigrationVersionRecorded(t, ctx, pool, schema, version, false)
}
