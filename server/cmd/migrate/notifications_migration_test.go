package main

// CEREBRO-PATCH(notifications-migration-test): firtal notification schema verification

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNotificationsMigrationCreatesSchemaAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	pool := migratedTestPool(t, ctx)
	defer pool.Close()

	assertIndexExists(t, ctx, pool, "idx_notifications_recipient_created_at")
	assertIndexExists(t, ctx, pool, "idx_notifications_unread_count")

	var unreadPredicate string
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_expr(indpred, indrelid)
		FROM pg_index
		WHERE indexrelid = 'idx_notifications_unread_count'::regclass
	`).Scan(&unreadPredicate); err != nil {
		t.Fatalf("read unread index predicate: %v", err)
	}
	if unreadPredicate != "(read_at IS NULL)" {
		t.Fatalf("unread index predicate = %q, want read_at IS NULL", unreadPredicate)
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('Notifications Test', 'notifications-test')
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO notifications (
			workspace_id, recipient_id, recipient_type, type,
			reference_id, reference_type, metadata
		)
		VALUES (
			$1, gen_random_uuid(), 'member', 'issue_mentioned',
			gen_random_uuid(), 'issue', '{"source":"test"}'::jsonb
		)
	`, workspaceID); err != nil {
		t.Fatalf("insert first notification: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO notifications (
			workspace_id, recipient_id, recipient_type, type,
			reference_id, reference_type, metadata
		)
		SELECT workspace_id, recipient_id, recipient_type, type,
		       reference_id, reference_type, metadata
		FROM notifications
		WHERE workspace_id = $1
	`, workspaceID); err != nil {
		t.Fatalf("insert duplicate notification: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM notifications WHERE workspace_id = $1
	`, workspaceID).Scan(&count); err != nil {
		t.Fatalf("count duplicate notifications: %v", err)
	}
	if count != 1 {
		t.Fatalf("notification count after duplicate insert = %d, want 1", count)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO notifications (
			workspace_id, recipient_id, recipient_type, type,
			reference_id, reference_type, metadata, created_at
		)
		SELECT workspace_id, recipient_id, recipient_type, type,
		       reference_id, reference_type, metadata, created_at + interval '6 minutes'
		FROM notifications
		WHERE workspace_id = $1
	`, workspaceID); err != nil {
		t.Fatalf("insert later notification: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM notifications WHERE workspace_id = $1
	`, workspaceID).Scan(&count); err != nil {
		t.Fatalf("count later notifications: %v", err)
	}
	if count != 2 {
		t.Fatalf("notification count after later insert = %d, want 2", count)
	}
}

func assertIndexExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename = 'notifications'
			  AND indexname = $1
		)
	`, name).Scan(&exists); err != nil {
		t.Fatalf("check index %s: %v", name, err)
	}
	if !exists {
		t.Fatalf("missing index %s", name)
	}
}

func migratedTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		baseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Skipf("DATABASE_URL is not parseable: %v", err)
	}

	probe, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Skipf("Skipping notifications migration test: could not open pool: %v", err)
	}
	if err := probe.Ping(ctx); err != nil {
		probe.Close()
		t.Skipf("Skipping notifications migration test: database not reachable: %v", err)
	}
	probe.Close()

	adminParsed := *parsed
	adminParsed.Path = "/postgres"
	adminURL := adminParsed.String()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("Skipping notifications migration test: could not open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	if err := adminPool.Ping(ctx); err != nil {
		t.Skipf("Skipping notifications migration test: admin DB not reachable: %v", err)
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("rand: %v", err)
	}
	testDBName := "multica_notifications_" + hex.EncodeToString(suffix)

	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", testDBName)); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(),
			fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", testDBName))
	})

	testParsed := *parsed
	testParsed.Path = "/" + testDBName
	testURL := testParsed.String()

	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	migrationsDir := findMigrationsDir(t)
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no migrations found in %s", migrationsDir)
	}
	sort.Strings(files)

	if err := applyAll(ctx, pool, files); err != nil {
		pool.Close()
		t.Fatalf("apply migrations: %v", err)
	}

	return pool
}
