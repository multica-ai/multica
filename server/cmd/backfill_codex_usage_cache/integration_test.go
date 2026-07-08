package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/migrations"
)

func TestExecuteBackfillUpdatesOnlyEligibleCodexRowsAndRebuildsRollup(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	if !testDatabaseReachable(ctx, adminURL) {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	tmpDB := fmt.Sprintf("multica_codex_usage_backfill_%d", time.Now().UnixNano())
	if err := testCreateDatabase(ctx, adminURL, tmpDB); err != nil {
		t.Fatalf("create temp database %s: %v", tmpDB, err)
	}
	t.Cleanup(func() {
		if err := testDropDatabase(context.Background(), adminURL, tmpDB); err != nil {
			t.Logf("drop temp database %s: %v", tmpDB, err)
		}
	})

	pool, err := pgxpool.New(ctx, testReplaceDatabase(adminURL, tmpDB))
	if err != nil {
		t.Fatalf("connect to temp database: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := testApplyMigrationsUpTo(ctx, pool, "102_task_usage_hourly_pipeline"); err != nil {
		t.Fatalf("apply migrations to 102: %v", err)
	}

	cutoff := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	createdAt := cutoff.Add(-48 * time.Hour)
	oldUpdatedAt := cutoff.Add(-24 * time.Hour)
	newUpdatedAt := cutoff.Add(24 * time.Hour)

	workspaceID, _, _, targetTaskID := seedBackfillTaskUsageFixture(t, ctx, pool, "target")
	_, _, _, otherWorkspaceTaskID := seedBackfillTaskUsageFixture(t, ctx, pool, "other")

	targetID := seedTaskUsageRow(t, ctx, pool, targetTaskID, "codex", "gpt-5-codex", 1000, 50, 300, createdAt, oldUpdatedAt)
	postCutoffID := seedTaskUsageRow(t, ctx, pool, targetTaskID, "codex", "gpt-5-codex-new", 900, 50, 200, createdAt, newUpdatedAt)
	nonCodexID := seedTaskUsageRow(t, ctx, pool, targetTaskID, "claude", "claude-test", 800, 50, 200, createdAt, oldUpdatedAt)
	zeroCacheID := seedTaskUsageRow(t, ctx, pool, targetTaskID, "codex", "gpt-5-codex-no-cache", 700, 50, 0, createdAt, oldUpdatedAt)
	otherWorkspaceID := seedTaskUsageRow(t, ctx, pool, otherWorkspaceTaskID, "codex", "gpt-5-codex-other-workspace", 600, 50, 100, createdAt, oldUpdatedAt)

	cfg := config{
		cutoff:      cutoff,
		workspaceID: workspaceID,
		batchSize:   10,
	}

	summary, total, err := loadDryRunSummary(ctx, pool, cfg)
	if err != nil {
		t.Fatalf("load dry-run summary: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("summary rows = %d, want 1: %#v", len(summary), summary)
	}
	if total.Rows != 1 || total.InputBefore != 1000 || total.InputAfter != 700 || total.Overcount != 300 {
		t.Fatalf("unexpected dry-run total: %+v", total)
	}
	assertTaskUsageInput(t, ctx, pool, targetID, 1000)

	updateStartedAt, err := databaseClock(ctx, pool)
	if err != nil {
		t.Fatalf("read update start clock: %v", err)
	}
	updatedRows, removedTokens, err := executeBackfill(ctx, pool, cfg)
	if err != nil {
		t.Fatalf("execute backfill: %v", err)
	}
	if updatedRows != 1 || removedTokens != 300 {
		t.Fatalf("updatedRows=%d removedTokens=%d, want 1/300", updatedRows, removedTokens)
	}
	updateFinishedAt, err := databaseClock(ctx, pool)
	if err != nil {
		t.Fatalf("read update finish clock: %v", err)
	}
	rollupFrom, rollupTo := rollupWindow(updateStartedAt, updateFinishedAt)
	var rollupRows int64
	if err := pool.QueryRow(ctx,
		`SELECT rollup_task_usage_hourly_window($1::timestamptz, $2::timestamptz)`,
		rollupFrom, rollupTo,
	).Scan(&rollupRows); err != nil {
		t.Fatalf("rebuild hourly rollup: %v", err)
	}
	if rollupRows == 0 {
		t.Fatalf("expected hourly rollup rows to be touched")
	}

	assertTaskUsageInput(t, ctx, pool, targetID, 700)
	assertTaskUsageInput(t, ctx, pool, postCutoffID, 900)
	assertTaskUsageInput(t, ctx, pool, nonCodexID, 800)
	assertTaskUsageInput(t, ctx, pool, zeroCacheID, 700)
	assertTaskUsageInput(t, ctx, pool, otherWorkspaceID, 600)

	var targetUpdatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM task_usage WHERE id = $1`, targetID).Scan(&targetUpdatedAt); err != nil {
		t.Fatalf("read target updated_at: %v", err)
	}
	if !targetUpdatedAt.After(cutoff) {
		t.Fatalf("target updated_at = %s, want after cutoff %s", targetUpdatedAt, cutoff)
	}

	updatedRows, removedTokens, err = executeBackfill(ctx, pool, cfg)
	if err != nil {
		t.Fatalf("execute backfill second time: %v", err)
	}
	if updatedRows != 0 || removedTokens != 0 {
		t.Fatalf("second execution updatedRows=%d removedTokens=%d, want 0/0", updatedRows, removedTokens)
	}

	var hourlyInput, hourlyCache int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0)::bigint,
		       COALESCE(SUM(cache_read_tokens), 0)::bigint
		  FROM task_usage_hourly
		 WHERE workspace_id = $1
		   AND provider = 'codex'
		   AND model = 'gpt-5-codex'
	`, workspaceID).Scan(&hourlyInput, &hourlyCache); err != nil {
		t.Fatalf("read hourly rollup: %v", err)
	}
	if hourlyInput != 700 || hourlyCache != 300 {
		t.Fatalf("hourly rollup input/cache = %d/%d, want 700/300", hourlyInput, hourlyCache)
	}
}

func seedTaskUsageRow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	taskID, provider, model string,
	input, output, cacheRead int64,
	createdAt, updatedAt time.Time,
) string {
	t.Helper()

	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO task_usage (
			task_id, provider, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, 0, $7, $8)
		RETURNING id
	`, taskID, provider, model, input, output, cacheRead, createdAt, updatedAt).Scan(&id); err != nil {
		t.Fatalf("seed task_usage row %s/%s: %v", provider, model, err)
	}
	return id
}

func assertTaskUsageInput(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, want int64) {
	t.Helper()
	var got int64
	if err := pool.QueryRow(ctx, `SELECT input_tokens FROM task_usage WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatalf("read task_usage %s: %v", id, err)
	}
	if got != want {
		t.Fatalf("task_usage %s input_tokens = %d, want %d", id, got, want)
	}
}

func seedBackfillTaskUsageFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) (string, string, string, string) {
	t.Helper()

	var wsID, runtimeID, agentID, taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ($1, $2)
		RETURNING id
	`, "codex backfill "+suffix, "codex-backfill-"+suffix).Scan(&wsID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'codex', 'online',
		        '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id
	`, wsID, "codex backfill runtime "+suffix).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1)
		RETURNING id
	`, wsID, "codex backfill agent "+suffix, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, payload
		)
		VALUES ($1, $2, 'queued', '{}'::jsonb)
		RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		altErr := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id)
			VALUES ($1, $2)
			RETURNING id
		`, agentID, runtimeID).Scan(&taskID)
		if altErr != nil {
			t.Fatalf("seed agent_task_queue: %v / %v", err, altErr)
		}
	}
	return wsID, runtimeID, agentID, taskID
}

func testResolveMigrationsDir() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(0) failed")
	}
	dir := filepath.Join(filepath.Dir(here), "..", "..", "migrations")
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("migrations dir not at %s", dir)
	}
	return dir, nil
}

func testDatabaseReachable(ctx context.Context, url string) bool {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return false
	}
	defer pool.Close()
	return pool.Ping(ctx) == nil
}

func testCreateDatabase(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name)); err != nil {
		return err
	}
	return nil
}

func testDropDatabase(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name)); err != nil {
		return err
	}
	return nil
}

func testReplaceDatabase(url, name string) string {
	idx := strings.LastIndex(url, "/")
	if idx < 0 {
		return url
	}
	rest := url[idx+1:]
	q := strings.Index(rest, "?")
	if q < 0 {
		return url[:idx+1] + name
	}
	return url[:idx+1] + name + rest[q:]
}

func testApplyMigrationsUpTo(ctx context.Context, pool *pgxpool.Pool, lastVersion string) error {
	dir, err := testResolveMigrationsDir()
	if err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}

	for _, f := range files {
		v := migrations.ExtractVersion(f)
		sql, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply %s: %w", v, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`,
			v); err != nil {
			return err
		}
		if v == lastVersion {
			return nil
		}
	}
	return fmt.Errorf("migration %q not found", lastVersion)
}
