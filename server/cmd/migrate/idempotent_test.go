package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrationsAreIdempotent applies every up migration to a fresh database,
// resets the schema_migrations bookkeeping, and runs every migration again
// against the already-migrated schema. The second pass simulates what happens
// when a half-applied deploy leaves the schema_migrations table out of sync
// with the actual schema (the failure mode described in JEH-356).
//
// Each .up.sql file must be safe to execute against a database that already
// contains the objects it creates. CREATE TABLE/INDEX must use IF NOT EXISTS,
// ALTER TABLE ADD COLUMN must use IF NOT EXISTS, ALTER TABLE ADD CONSTRAINT
// must be wrapped in a guard (DO $$ ... pg_constraint check ... END $$ or
// DROP CONSTRAINT IF EXISTS first), and INSERT must use ON CONFLICT DO NOTHING
// or a WHERE NOT EXISTS clause.
func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()

	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		baseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Skipf("DATABASE_URL is not parseable: %v", err)
	}

	// Quick reachability check before we start creating throwaway DBs.
	probe, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Skipf("Skipping idempotency test: could not open pool: %v", err)
	}
	if err := probe.Ping(ctx); err != nil {
		probe.Close()
		t.Skipf("Skipping idempotency test: database not reachable: %v", err)
	}
	probe.Close()

	// Build an admin URL pointing at the default `postgres` database so we can
	// CREATE/DROP a throwaway test database.
	adminParsed := *parsed
	adminParsed.Path = "/postgres"
	adminURL := adminParsed.String()

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("Skipping idempotency test: could not open admin pool: %v", err)
	}
	defer adminPool.Close()

	if err := adminPool.Ping(ctx); err != nil {
		t.Skipf("Skipping idempotency test: admin DB not reachable: %v", err)
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("rand: %v", err)
	}
	testDBName := "multica_idempotent_" + hex.EncodeToString(suffix)

	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", testDBName)); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		// Use a fresh context so cleanup runs even if the test was cancelled.
		// Drop forcibly so any leaked connections from the test pool are kicked.
		_, _ = adminPool.Exec(context.Background(),
			fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", testDBName))
	})

	testParsed := *parsed
	testParsed.Path = "/" + testDBName
	testURL := testParsed.String()

	testPool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer testPool.Close()

	// Locate the migrations directory whether tests run from server/cmd/migrate
	// or from the repo root.
	migrationsDir := findMigrationsDir(t)

	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no migrations found in %s", migrationsDir)
	}
	sort.Strings(files)

	// First pass: apply every migration on the empty database.
	if err := applyAll(ctx, testPool, files); err != nil {
		t.Fatalf("first pass failed: %v", err)
	}

	// Second pass: re-apply every migration against the already-migrated
	// schema. Any migration that errors here is non-idempotent and would
	// brick the next deploy after a partial-failure recovery.
	if err := applyAll(ctx, testPool, files); err != nil {
		t.Fatalf("second pass (idempotency) failed: %v", err)
	}
}

func applyAll(ctx context.Context, pool *pgxpool.Pool, files []string) error {
	for _, file := range files {
		sql, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("exec %s: %w", filepath.Base(file), err)
		}
	}
	return nil
}

func findMigrationsDir(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../migrations",   // when running `go test ./cmd/migrate/...` from server/
		"server/migrations",  // when running from repo root
		"migrations",         // when running from server/cmd/migrate
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			if strings.HasSuffix(abs, "migrations") {
				return c
			}
		}
	}
	t.Fatalf("could not find migrations directory; tried %v", candidates)
	return ""
}
