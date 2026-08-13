package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRedundantIndexMigrationsPreserveCoveringQueryPlansAndRollback(t *testing.T) {
	adminPool := openTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), rand.Uint32())
	schema := "migrate_redundant_idx_" + suffix
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
	createRedundantIndexFixture(t, ctx, pool)

	versions := []string{
		"300_drop_redundant_issue_workspace_number_index",
		"301_drop_redundant_sys_cron_job_plan_index",
		"302_drop_redundant_channel_chat_session_binding_index",
		"303_drop_redundant_lark_chat_session_binding_index",
	}
	indexNames := []string{
		"idx_issue_workspace_number",
		"idx_sys_cron_exec_job_plan",
		"idx_channel_chat_session_binding_session",
		"idx_lark_chat_session_binding_session",
	}
	indexCountBefore := countSchemaIndexes(t, pool, schema)
	redundantBytesBefore := sumIndexSizes(t, pool, schema, indexNames)
	if redundantBytesBefore <= 0 {
		t.Fatalf("redundant index size before drops = %d, want positive", redundantBytesBefore)
	}
	t.Logf("seeded fixture before drops: indexes=%d redundant_bytes=%d", indexCountBefore, redundantBytesBefore)

	if err := runMigrations(ctx, pool, runOptions{
		Direction:             "up",
		Files:                 realMigrationFiles(t, versions, "up"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("up"),
	}); err != nil {
		t.Fatalf("apply redundant-index drops: %v", err)
	}

	for _, indexName := range indexNames {
		assertIndexExists(t, pool, schema, indexName, false)
	}
	indexCountAfter := countSchemaIndexes(t, pool, schema)
	if indexCountAfter != indexCountBefore-len(indexNames) {
		t.Fatalf("schema index count after drops = %d, want %d", indexCountAfter, indexCountBefore-len(indexNames))
	}
	if got := sumIndexSizes(t, pool, schema, indexNames); got != 0 {
		t.Fatalf("redundant index size after drops = %d, want 0", got)
	}
	t.Logf("seeded fixture after drops: indexes=%d redundant_bytes=0", indexCountAfter)
	for _, indexName := range []string{
		"uq_issue_workspace_number",
		"uq_sys_cron_execution",
		"channel_chat_session_binding_chat_session_id_key",
		"lark_chat_session_binding_chat_session_id_key",
	} {
		assertIndexValidity(t, pool, schema, indexName, true)
	}

	assertPlanUsesIndex(t, pool,
		`SELECT id FROM issue
		 WHERE workspace_id = '00000000-0000-0000-0000-000000000001'
		   AND number = 2500`,
		"uq_issue_workspace_number",
	)
	assertPlanUsesIndex(t, pool,
		`SELECT id FROM sys_cron_executions
		 WHERE job_name = 'job'
		   AND scope_kind = 'global'
		   AND scope_id = 'global'
		 ORDER BY plan_time DESC
		 LIMIT 1`,
		"Index Scan Backward using uq_sys_cron_execution",
	)
	assertPlanUsesIndex(t, pool,
		`SELECT id FROM channel_chat_session_binding
		 WHERE chat_session_id = '00000000-0000-0000-0000-000000001234'`,
		"channel_chat_session_binding_chat_session_id_key",
	)
	assertPlanUsesIndex(t, pool,
		`SELECT id FROM lark_chat_session_binding
		 WHERE chat_session_id = '00000000-0000-0000-0000-000000001234'`,
		"lark_chat_session_binding_chat_session_id_key",
	)

	reversedVersions := append([]string(nil), versions...)
	for left, right := 0, len(reversedVersions)-1; left < right; left, right = left+1, right-1 {
		reversedVersions[left], reversedVersions[right] = reversedVersions[right], reversedVersions[left]
	}
	if err := runMigrations(ctx, pool, runOptions{
		Direction:             "down",
		Files:                 realMigrationFiles(t, reversedVersions, "down"),
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
		Hooks:                 hooksForDirection("down"),
	}); err != nil {
		t.Fatalf("roll back redundant-index drops: %v", err)
	}

	for _, indexName := range indexNames {
		assertIndexValidity(t, pool, schema, indexName, true)
	}
	var sysCronIndexDefinition string
	if err := pool.QueryRow(ctx,
		"SELECT pg_get_indexdef($1::regclass)",
		pgx.Identifier{schema, "idx_sys_cron_exec_job_plan"}.Sanitize(),
	).Scan(&sysCronIndexDefinition); err != nil {
		t.Fatalf("read restored sys_cron index definition: %v", err)
	}
	if !strings.Contains(sysCronIndexDefinition, "plan_time DESC") {
		t.Fatalf("restored sys_cron index lost descending order: %s", sysCronIndexDefinition)
	}
}

func openTestPoolWithSearchPath(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(testDatabaseURL())
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open schema-scoped test pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping schema-scoped test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createRedundantIndexFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	statements := []string{
		`CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE issue (
			id BIGSERIAL PRIMARY KEY,
			workspace_id UUID NOT NULL,
			number INTEGER NOT NULL,
			CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number)
		)`,
		`CREATE INDEX idx_issue_workspace_number ON issue (workspace_id, number)`,
		`CREATE TABLE sys_cron_executions (
			id BIGSERIAL PRIMARY KEY,
			job_name TEXT NOT NULL,
			scope_kind TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			plan_time TIMESTAMPTZ NOT NULL,
			CONSTRAINT uq_sys_cron_execution UNIQUE (job_name, scope_kind, scope_id, plan_time)
		)`,
		`CREATE INDEX idx_sys_cron_exec_job_plan
			ON sys_cron_executions (job_name, scope_kind, scope_id, plan_time DESC)`,
		`CREATE TABLE channel_chat_session_binding (
			id BIGSERIAL PRIMARY KEY,
			chat_session_id UUID NOT NULL,
			CONSTRAINT channel_chat_session_binding_chat_session_id_key UNIQUE (chat_session_id)
		)`,
		`CREATE INDEX idx_channel_chat_session_binding_session
			ON channel_chat_session_binding (chat_session_id)`,
		`CREATE TABLE lark_chat_session_binding (
			id BIGSERIAL PRIMARY KEY,
			chat_session_id UUID NOT NULL,
			CONSTRAINT lark_chat_session_binding_chat_session_id_key UNIQUE (chat_session_id)
		)`,
		`CREATE INDEX idx_lark_chat_session_binding_session
			ON lark_chat_session_binding (chat_session_id)`,
		`INSERT INTO issue (workspace_id, number)
		 SELECT '00000000-0000-0000-0000-000000000001', n
		 FROM generate_series(1, 5000) AS n`,
		`INSERT INTO sys_cron_executions (job_name, scope_kind, scope_id, plan_time)
		 SELECT 'job', 'global', 'global', now() - make_interval(secs => n)
		 FROM generate_series(1, 5000) AS n`,
		`INSERT INTO channel_chat_session_binding (chat_session_id)
		 SELECT ('00000000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid
		 FROM generate_series(1, 5000) AS n`,
		`INSERT INTO lark_chat_session_binding (chat_session_id)
		 SELECT ('00000000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid
		 FROM generate_series(1, 5000) AS n`,
		`ANALYZE issue`,
		`ANALYZE sys_cron_executions`,
		`ANALYZE channel_chat_session_binding`,
		`ANALYZE lark_chat_session_binding`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply fixture statement %q: %v", statement, err)
		}
	}
}

func realMigrationFiles(t *testing.T, versions []string, direction string) []string {
	t.Helper()
	files := make([]string, 0, len(versions))
	for _, version := range versions {
		path := filepath.Join("..", "..", "migrations", version+"."+direction+".sql")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat migration %s: %v", path, err)
		}
		files = append(files, path)
	}
	return files
}

func assertIndexExists(
	t *testing.T,
	pool *pgxpool.Pool,
	schema string,
	indexName string,
	want bool,
) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1
			  AND c.relname = $2
			  AND c.relkind = 'i'
		)
	`, schema, indexName).Scan(&exists); err != nil {
		t.Fatalf("read existence for %s.%s: %v", schema, indexName, err)
	}
	if exists != want {
		t.Fatalf("index %s.%s existence = %v, want %v", schema, indexName, exists, want)
	}
}

func countSchemaIndexes(t *testing.T, pool *pgxpool.Pool, schema string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1
		  AND c.relkind = 'i'
	`, schema).Scan(&count); err != nil {
		t.Fatalf("count indexes in schema %s: %v", schema, err)
	}
	return count
}

func sumIndexSizes(
	t *testing.T,
	pool *pgxpool.Pool,
	schema string,
	indexNames []string,
) int64 {
	t.Helper()
	var bytes int64
	if err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(sum(pg_relation_size(c.oid)), 0)::bigint
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1
		  AND c.relname = ANY($2::text[])
		  AND c.relkind = 'i'
	`, schema, indexNames).Scan(&bytes); err != nil {
		t.Fatalf("sum redundant index sizes in schema %s: %v", schema, err)
	}
	return bytes
}

func assertPlanUsesIndex(t *testing.T, pool *pgxpool.Pool, query, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := pool.Query(ctx, "EXPLAIN (COSTS OFF) "+query)
	if err != nil {
		t.Fatalf("explain query: %v", err)
	}
	defer rows.Close()
	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		planLines = append(planLines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read explain rows: %v", err)
	}
	plan := strings.Join(planLines, "\n")
	if !strings.Contains(plan, want) {
		t.Fatalf("query plan does not use %q:\n%s", want, plan)
	}
}
