package taskusagebackfill_test

import (
	"context"
	"errors"
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
	"github.com/multica-ai/multica/server/internal/taskusagebackfill"
)

// TestHook_DirectV034Upgrade simulates the path described in
// docs/db-backed-execution-scheduler-rfc.md §12.3 ("从 v0.3.4 直接升级
// 到带 scheduler 的版本") and RFC §14 row "direct v0.3.4 upgrade":
//
//   - apply migrations through 102 (legacy daily rollups + new hourly
//     schema/pipeline are both live);
//   - seed a `task_usage` row dated > 1 hour ago, watermark left at
//     1970-01-01 (the post-101 default);
//   - migration 103's fail-closed guard would normally abort `migrate
//     up` here because watermark < max_event - 1h;
//   - run taskusagebackfill.Hook (the migrator hook from MUL-2957);
//   - the hook performs an idempotent monthly-slice backfill, stamps
//     the watermark, and migration 103 then passes.
//
// The test creates and drops a temporary database so it does not
// disturb the shared development DB. If the postgres server is not
// reachable the test skips (mirrors the rest of the integration suite).
func TestHook_DirectV034Upgrade(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	if !databaseReachable(ctx, adminURL) {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	tmpDB := fmt.Sprintf("multica_v034_upgrade_%d", time.Now().UnixNano())
	if err := createDatabase(ctx, adminURL, tmpDB); err != nil {
		t.Fatalf("create temp database %s: %v", tmpDB, err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), adminURL, tmpDB); err != nil {
			t.Logf("drop temp database %s: %v", tmpDB, err)
		}
	})

	tmpURL := replaceDatabase(adminURL, tmpDB)
	pool, err := pgxpool.New(ctx, tmpURL)
	if err != nil {
		t.Fatalf("connect to temp database: %v", err)
	}
	t.Cleanup(pool.Close)

	// Apply migrations through 102 (the simulated v0.3.4-equivalent
	// state where the new hourly schema and pipeline are present, but
	// 103 has not yet dropped the legacy daily rollups).
	if err := applyMigrationsUpTo(ctx, pool, "102_task_usage_hourly_pipeline"); err != nil {
		t.Fatalf("apply migrations to 102: %v", err)
	}

	// Seed: one task_usage row dated 2 hours ago. Triggers fire on
	// INSERT/UPDATE post-102 only via the watermark window in
	// rollup_task_usage_hourly_window, so we leave the watermark at
	// the post-101 default of 1970-01-01.
	wsID, runtimeID, agentID, taskID := seedTaskUsageFixture(t, ctx, pool)

	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := pool.Exec(ctx, `
		INSERT INTO task_usage (
			task_id, provider, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			created_at
		) VALUES ($1, 'openai', 'gpt-test', 10, 20, 0, 0, $2)
	`, taskID, twoHoursAgo); err != nil {
		t.Fatalf("seed task_usage: %v", err)
	}
	_ = wsID
	_ = runtimeID
	_ = agentID

	// Confirm migration 103's guard would abort if applied now.
	pendingErr := tryApplyMigration(ctx, pool, "103_drop_legacy_daily_rollups")
	if pendingErr == nil {
		t.Fatalf("migration 103 guard did not trip with stale watermark; preconditions invalid")
	}
	if !strings.Contains(pendingErr.Error(), "refusing to drop legacy daily rollups") {
		t.Fatalf("expected fail-closed guard error, got %v", pendingErr)
	}
	// Roll back the failed transaction state (Postgres connections are
	// fine; we just did not commit anything destructive).

	// Run the MUL-2957 migrator hook. The hook should:
	//   * Read MIN/MAX from task_usage.
	//   * Acquire advisory lock 4246 on its own conn.
	//   * Walk monthly slices via rollup_task_usage_hourly_window.
	//   * Stamp the watermark to now() - 5 minutes.
	res, err := taskusagebackfill.Hook(ctx, pool, taskusagebackfill.HookOptions{})
	if err != nil {
		t.Fatalf("Hook returned error: %v", err)
	}
	if res.Skipped != "" {
		t.Fatalf("Hook should have run a backfill; got skipped=%q", res.Skipped)
	}
	if !res.WatermarkStamped {
		t.Fatalf("Hook should have stamped the watermark")
	}
	if res.SlicesProcessed < 1 {
		t.Fatalf("expected at least 1 slice processed, got %d", res.SlicesProcessed)
	}

	// Hourly bucket exists now.
	var hourlyRows int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM task_usage_hourly
	`).Scan(&hourlyRows); err != nil {
		t.Fatalf("count task_usage_hourly: %v", err)
	}
	if hourlyRows == 0 {
		t.Fatalf("backfill did not produce any hourly rows; check seed fixture")
	}

	// Watermark stamped close to "now - 5 min" (loose bound to absorb
	// CI clock skew).
	var watermark time.Time
	if err := pool.QueryRow(ctx, `
		SELECT watermark_at FROM task_usage_hourly_rollup_state WHERE id = 1
	`).Scan(&watermark); err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	expected := time.Now().UTC().Add(-5 * time.Minute)
	if delta := watermark.UTC().Sub(expected); delta > time.Minute || delta < -2*time.Minute {
		t.Fatalf("watermark %s far from expected %s (delta=%s)",
			watermark.Format(time.RFC3339), expected.Format(time.RFC3339), delta)
	}

	// And now migration 103 must apply cleanly.
	if err := tryApplyMigration(ctx, pool, "103_drop_legacy_daily_rollups"); err != nil {
		t.Fatalf("migration 103 still fails after hook: %v", err)
	}
}

// TestHook_FreshDatabaseStampsWatermarkOnly covers the empty-history
// branch: a brand new self-host install runs the hook before migration
// 103, but task_usage is empty. The hook should stamp the watermark
// (so the guard's "fresh DB" branch passes) and not touch any rows.
func TestHook_FreshDatabaseStampsWatermarkOnly(t *testing.T) {
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	if !databaseReachable(ctx, adminURL) {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	tmpDB := fmt.Sprintf("multica_v034_fresh_%d", time.Now().UnixNano())
	if err := createDatabase(ctx, adminURL, tmpDB); err != nil {
		t.Fatalf("create temp database: %v", err)
	}
	t.Cleanup(func() {
		_ = dropDatabase(context.Background(), adminURL, tmpDB)
	})

	pool, err := pgxpool.New(ctx, replaceDatabase(adminURL, tmpDB))
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := applyMigrationsUpTo(ctx, pool, "102_task_usage_hourly_pipeline"); err != nil {
		t.Fatalf("apply migrations to 102: %v", err)
	}

	res, err := taskusagebackfill.Hook(ctx, pool, taskusagebackfill.HookOptions{})
	if err != nil {
		t.Fatalf("Hook on empty DB: %v", err)
	}
	if res.Skipped != "task_usage_empty" {
		t.Fatalf("expected skipped=task_usage_empty, got %q", res.Skipped)
	}
	if !res.WatermarkStamped {
		t.Fatalf("watermark must still be stamped on empty DB so 103 can pass")
	}

	// Migration 103 applies cleanly on the fresh DB — its early-out
	// branch (no task_usage rows) plus the stamped watermark let it
	// through.
	if err := tryApplyMigration(ctx, pool, "103_drop_legacy_daily_rollups"); err != nil {
		t.Fatalf("migration 103 fresh-DB path failed: %v", err)
	}
}

// ---------------------------------------------------------------------
// Test helpers — minimal harness so we can apply migrations to a temp
// database without coupling to cmd/migrate's exit-on-error model.
// ---------------------------------------------------------------------

// resolveMigrationsDir locates server/migrations relative to this test
// file's source path. The shared migrations.ResolveDir() walks parent
// directories looking for any folder literally named "migrations",
// which collides with the Go package server/internal/migrations and
// returns the wrong directory. Anchoring on runtime.Caller's file path
// avoids that ambiguity entirely.
func resolveMigrationsDir() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(0) failed")
	}
	// here = .../server/internal/taskusagebackfill/hook_test.go
	dir := filepath.Join(filepath.Dir(here), "..", "..", "migrations")
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("migrations dir not at %s", dir)
	}
	return dir, nil
}

func databaseReachable(ctx context.Context, url string) bool {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return false
	}
	defer pool.Close()
	return pool.Ping(ctx) == nil
}

func createDatabase(ctx context.Context, adminURL, name string) error {
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

func dropDatabase(ctx context.Context, adminURL, name string) error {
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

// replaceDatabase swaps the database segment of a postgres URL.
// "postgres://u:p@h:5432/old?x=1" with name="new" -> ".../new?x=1".
func replaceDatabase(url, name string) string {
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

func applyMigrationsUpTo(ctx context.Context, pool *pgxpool.Pool, lastVersion string) error {
	dir, err := resolveMigrationsDir()
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

func tryApplyMigration(ctx context.Context, pool *pgxpool.Pool, version string) error {
	dir, err := resolveMigrationsDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, version+".up.sql")
	sql, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`,
		version)
	return err
}

// seedTaskUsageFixture inserts the minimal joined rows
// (workspace, runtime, agent, agent_task_queue) needed for a task_usage
// row to participate in the hourly rollup. Returns the IDs in
// (workspace, runtime, agent, task) order.
func seedTaskUsageFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, string, string) {
	t.Helper()

	var wsID, runtimeID, agentID, taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('upgrade-test', 'upgrade-test')
		RETURNING id
	`).Scan(&wsID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'upgrade-runtime', 'cloud', 'p', 'online',
		        '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id
	`, wsID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks
		)
		VALUES ($1, 'upgrade-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1)
		RETURNING id
	`, wsID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, payload
		)
		VALUES ($1, $2, 'queued', '{}'::jsonb)
		RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		// agent_task_queue schema may differ; fall back to inferring
		// the smallest column set.
		var altErr error
		altErr = pool.QueryRow(ctx, `
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

// ensure errors-helpers import isn't dropped if the compiler decides
// to optimise the empty references away.
var _ = errors.New
