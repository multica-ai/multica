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
	"github.com/multica-ai/multica/server/internal/migrations"
)

// Migration B (162_space_number_cutover) end-to-end invariant test.
//
// The full migration chain cannot be replayed locally (084/101 need pg15+
// UNIQUE NULLS NOT DISTINCT and the dev box only has pg14), and Docker is
// unavailable in most sandboxes, so this test is deliberately CI-shaped: it
// connects to whatever DATABASE_URL points at (default the local dev DSN),
// and if Postgres is unreachable it SKIPS cleanly — the same pattern as
// migrate_concurrent_test.go and every other live-Postgres test in the repo.
//
// Rather than replay all 162 migrations, it builds a *synthetic minimal
// schema* — just the tables and the uq_issue_workspace_number constraint that
// 161 and 162 actually reference — inside a private throwaway schema
// (cutover_test_<ts>_<rand>) with search_path pinned to it. Then it applies the
// real 161 + 162 SQL files through the production runMigrations loop and
// asserts the numbering invariant:
//
//   - re-backfill: a NULL-space straggler written during the deploy window is
//     assigned the default Space by 162;
//   - counter sync: the default Space's issue_counter never regresses and rises
//     to cover both max(number) and the legacy workspace.issue_counter;
//   - cutover: issue.space_id / autopilot.space_id become NOT NULL, the legacy
//     uq_issue_workspace_number is gone, uq_issue_space_number is in place;
//   - per-space numbering: two Spaces' number-1 issues coexist in one workspace,
//     while a duplicate (space_id, number) is rejected.
//
// Two negative tests prove the safe-by-construction preflight RAISEs (and rolls
// back before any destructive DDL) when its invariants are violated.

func cutoverDSN() string {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	return dsn
}

// newCutoverSchema opens a pool whose connections are pinned to a fresh private
// schema via search_path, so the migrations' unqualified table names resolve to
// synthetic fixtures instead of the real public schema. Skips when the DB is
// unreachable; drops the schema on cleanup.
func newCutoverSchema(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	schema := fmt.Sprintf("cutover_test_%d_%d", time.Now().UnixNano(), rand.Uint32())

	cfg, err := pgxpool.ParseConfig(cutoverDSN())
	if err != nil {
		t.Skipf("parse DATABASE_URL: %v", err)
	}
	// Pinning search_path to a not-yet-existing schema is allowed by Postgres;
	// the schema is created below before any unqualified name is used.
	cfg.ConnConfig.RuntimeParams["search_path"] = schema

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("could not open pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable at %s: %v", cutoverDSN(), err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgx.Identifier{schema}.Sanitize())); err != nil {
		pool.Close()
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgx.Identifier{schema}.Sanitize())); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
		pool.Close()
	})
	return pool, schema
}

// syntheticBaseSchema is the minimal pre-161 schema: only the columns and
// constraints that migrations 161 and 162 read or alter. Names must match
// exactly (uq_issue_workspace_number, member/project/user FK targets).
const syntheticBaseSchema = `
CREATE TABLE "user" (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);
CREATE TABLE workspace (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL DEFAULT '',
    issue_prefix TEXT NOT NULL DEFAULT '',
    issue_counter INT NOT NULL DEFAULT 0
);
CREATE TABLE member (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role TEXT NOT NULL DEFAULT 'member',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);
CREATE TABLE project (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE issue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    number INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'todo',
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    project_id UUID,
    parent_issue_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number),
    CONSTRAINT issue_project_id_fkey FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE SET NULL,
    CONSTRAINT issue_parent_issue_id_fkey FOREIGN KEY (parent_issue_id) REFERENCES issue(id) ON DELETE SET NULL
);
CREATE TABLE autopilot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_id UUID,
    CONSTRAINT autopilot_project_id_fkey FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE SET NULL
);
`

func execCutover(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// applyCutoverMigration runs a single real migration file through the
// production runMigrations loop against the pinned pool + private
// schema_migrations table. A unique advisory-lock key per call keeps it from
// blocking on a real migration runner sharing the same database.
func applyCutoverMigration(pool *pgxpool.Pool, schema, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), raceTestTimeout)
	defer cancel()
	return runMigrations(ctx, pool, runOptions{
		Direction:             "up",
		Files:                 []string{path},
		SchemaMigrationsTable: schema + ".schema_migrations",
		AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
	})
}

func cutoverFilePaths(t *testing.T) (up161, up162 string) {
	t.Helper()
	dir, err := migrations.ResolveDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	up161 = filepath.Join(dir, "161_workspace_space.up.sql")
	up162 = filepath.Join(dir, "162_space_number_cutover.up.sql")
	for _, p := range []string{up161, up162} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("migration file missing: %s: %v", p, err)
		}
	}
	return up161, up162
}

// seedPre161 creates one workspace (prefix ENG, counter 3) with an owner and
// three legacy issues (numbers 1..3, NULL space) and returns the workspace id.
func seedPre161(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var userID, wsID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" DEFAULT VALUES RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, issue_prefix, issue_counter) VALUES ('Acme', 'ENG', 3) RETURNING id`,
	).Scan(&wsID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	execCutover(t, pool, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsID, userID)
	var projectID string
	if err := pool.QueryRow(ctx, `INSERT INTO project (workspace_id) VALUES ($1) RETURNING id`, wsID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	for n := 1; n <= 3; n++ {
		if n == 1 {
			execCutover(t, pool,
				`INSERT INTO issue (workspace_id, number, status, project_id) VALUES ($1, $2, 'todo', $3)`, wsID, n, projectID)
			continue
		}
		execCutover(t, pool, `INSERT INTO issue (workspace_id, number, status) VALUES ($1, $2, 'todo')`, wsID, n)
	}
	return wsID
}

func constraintExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	return constraintExistsOn(t, pool, "issue", name)
}

func constraintExistsOn(t *testing.T, pool *pgxpool.Pool, table, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conname = $1 AND conrelid = to_regclass($2))`,
		name, table,
	).Scan(&exists); err != nil {
		t.Fatalf("check constraint %s on %s: %v", name, table, err)
	}
	return exists
}

func TestMigration162CutoverInvariant(t *testing.T) {
	pool, schema := newCutoverSchema(t)
	up161, up162 := cutoverFilePaths(t)

	execCutover(t, pool, syntheticBaseSchema)
	wsID := seedPre161(t, pool)

	if err := applyCutoverMigration(pool, schema, up161); err != nil {
		t.Fatalf("apply 161: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Simulate the deploy window: an old, Space-unaware instance creates a
	// Project and issue #4 with NULL space_id, then advances the legacy counter.
	var windowProjectID string
	if err := pool.QueryRow(ctx, `INSERT INTO project (workspace_id) VALUES ($1) RETURNING id`, wsID).Scan(&windowProjectID); err != nil {
		t.Fatalf("insert deploy-window project: %v", err)
	}
	execCutover(t, pool, `INSERT INTO issue (workspace_id, number, status, project_id) VALUES ($1, 4, 'todo', $2)`, wsID, windowProjectID)
	execCutover(t, pool, `UPDATE workspace SET issue_counter = 5 WHERE id = $1`, wsID)

	if err := applyCutoverMigration(pool, schema, up162); err != nil {
		t.Fatalf("apply 162: %v", err)
	}

	// Re-backfill: no Project or Issue may still carry a NULL space_id.
	var nullSpaceCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM project WHERE space_id IS NULL`).Scan(&nullSpaceCount); err != nil {
		t.Fatalf("count null-space projects: %v", err)
	}
	if nullSpaceCount != 0 {
		t.Fatalf("re-backfill left %d NULL-space projects, want 0", nullSpaceCount)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE space_id IS NULL`).Scan(&nullSpaceCount); err != nil {
		t.Fatalf("count null-space issues: %v", err)
	}
	if nullSpaceCount != 0 {
		t.Fatalf("re-backfill left %d NULL-space issues, want 0", nullSpaceCount)
	}

	// Counter sync: default space counter = GREATEST(seeded 3, max number 4,
	// legacy ws counter 5) = 5. It must never regress. No is_default flag — the
	// default space is identified the same way GetDefaultWorkspaceSpace and
	// migration 162 itself do: the workspace's earliest-created Space.
	var defaultSpaceID string
	var defaultCounter int
	var defaultKey string
	if err := pool.QueryRow(ctx,
		`SELECT id, key, issue_counter FROM workspace_space WHERE workspace_id = $1 ORDER BY created_at ASC, id ASC LIMIT 1`, wsID,
	).Scan(&defaultSpaceID, &defaultKey, &defaultCounter); err != nil {
		t.Fatalf("read default space: %v", err)
	}
	if defaultCounter != 5 {
		t.Fatalf("default space issue_counter = %d, want 5 (GREATEST of counter/max-number/ws-counter)", defaultCounter)
	}
	if defaultKey != "ENG" {
		t.Fatalf("default space key = %q, want ENG", defaultKey)
	}

	var projectIssueMismatchCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM issue i
		JOIN project p ON p.id = i.project_id
		WHERE i.space_id <> p.space_id OR i.workspace_id <> p.workspace_id
	`).Scan(&projectIssueMismatchCount); err != nil {
		t.Fatalf("count Project Issue Space mismatches: %v", err)
	}
	if projectIssueMismatchCount != 0 {
		t.Fatalf("found %d Project Issue Space mismatches, want 0", projectIssueMismatchCount)
	}

	// Cutover: legacy uniqueness gone, space-scoped uniqueness present.
	if constraintExists(t, pool, "uq_issue_workspace_number") {
		t.Fatal("uq_issue_workspace_number should have been dropped")
	}
	if !constraintExists(t, pool, "uq_issue_space_number") {
		t.Fatal("uq_issue_space_number should exist after cutover")
	}
	if !constraintExists(t, pool, "fk_issue_workspace_project_space") {
		t.Fatal("Project Issue same-Space constraint should exist after cutover")
	}
	if !constraintExistsOn(t, pool, "autopilot", "fk_autopilot_workspace_project_space") {
		t.Fatal("Project Autopilot same-Space constraint should exist after cutover")
	}
	if !constraintExists(t, pool, "fk_issue_workspace_parent_space") {
		t.Fatal("parent/child same-Space constraint should exist after cutover")
	}

	// space_id is NOT NULL now: a NULL insert must fail.
	if _, err := pool.Exec(ctx,
		`INSERT INTO issue (workspace_id, space_id, number, status) VALUES ($1, NULL, 99, 'todo')`, wsID,
	); err == nil {
		t.Fatal("expected NOT NULL violation inserting issue with NULL space_id")
	}

	// Per-space numbering: a second Space's issue #1 coexists with ENG-1 in the
	// same workspace (impossible under the dropped workspace-number unique).
	var designSpaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace_space (workspace_id, name, key) VALUES ($1, 'Design', 'DES') RETURNING id`, wsID,
	).Scan(&designSpaceID); err != nil {
		t.Fatalf("insert design space: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO issue (workspace_id, space_id, number, status) VALUES ($1, $2, 1, 'todo')`, wsID, designSpaceID,
	); err != nil {
		t.Fatalf("DES-1 should coexist with ENG-1: %v", err)
	}

	// Ownership constraints reject attaching a Design issue to an Engineering
	// Project or parent, even if a future write path forgets handler validation.
	var engProjectID, engParentID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM project WHERE workspace_id = $1 ORDER BY created_at, id LIMIT 1`, wsID,
	).Scan(&engProjectID); err != nil {
		t.Fatalf("read Engineering project: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT id FROM issue WHERE workspace_id = $1 AND space_id = $2 ORDER BY number LIMIT 1`, wsID, defaultSpaceID,
	).Scan(&engParentID); err != nil {
		t.Fatalf("read Engineering parent: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO issue (workspace_id, space_id, number, status, project_id) VALUES ($1, $2, 2, 'todo', $3)`, wsID, designSpaceID, engProjectID,
	); err == nil {
		t.Fatal("expected Project Issue same-Space constraint violation")
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO autopilot (workspace_id, space_id, project_id) VALUES ($1, $2, $3)`, wsID, designSpaceID, engProjectID,
	); err == nil {
		t.Fatal("expected Project Autopilot same-Space constraint violation")
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO issue (workspace_id, space_id, number, status, parent_issue_id) VALUES ($1, $2, 2, 'todo', $3)`, wsID, designSpaceID, engParentID,
	); err == nil {
		t.Fatal("expected parent/child same-Space constraint violation")
	}
	// But a duplicate (space_id, number) within one Space is rejected.
	if _, err := pool.Exec(ctx,
		`INSERT INTO issue (workspace_id, space_id, number, status) VALUES ($1, $2, 1, 'todo')`, wsID, designSpaceID,
	); err == nil {
		t.Fatal("expected uq_issue_space_number violation on duplicate DES-1")
	}
}

func TestMigration162PreflightRejectsDuplicateSpaceNumber(t *testing.T) {
	pool, schema := newCutoverSchema(t)
	up161, up162 := cutoverFilePaths(t)

	execCutover(t, pool, syntheticBaseSchema)
	wsID := seedPre161(t, pool)
	if err := applyCutoverMigration(pool, schema, up161); err != nil {
		t.Fatalf("apply 161: %v", err)
	}

	// Force a duplicate (space_id, number): the legacy constraint normally makes
	// this impossible, so drop it first, then mint a second default-space issue
	// reusing number 1.
	execCutover(t, pool, `ALTER TABLE issue DROP CONSTRAINT uq_issue_workspace_number`)
	execCutover(t, pool, `
		INSERT INTO issue (workspace_id, space_id, number, status)
		SELECT $1, wt.id, 1, 'todo' FROM workspace_space wt WHERE wt.workspace_id = $1`, wsID)

	err := applyCutoverMigration(pool, schema, up162)
	if err == nil {
		t.Fatal("expected 162 to fail on duplicate (space_id, number)")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error %q does not mention duplicate preflight", err)
	}
	// Preflight RAISEs before the DDL, so the cutover constraint must not exist.
	if constraintExists(t, pool, "uq_issue_space_number") {
		t.Fatal("uq_issue_space_number must not be created when preflight fails")
	}
}

func TestMigration162PreflightRejectsNullSpace(t *testing.T) {
	pool, schema := newCutoverSchema(t)
	up161, up162 := cutoverFilePaths(t)

	execCutover(t, pool, syntheticBaseSchema)
	wsID := seedPre161(t, pool)
	if err := applyCutoverMigration(pool, schema, up161); err != nil {
		t.Fatalf("apply 161: %v", err)
	}

	// Create an un-backfillable straggler: null out an issue's space and archive
	// the workspace's only Space, so it's no longer a valid re-backfill target
	// (162's re-backfill only considers archived_at IS NULL Spaces — there is no
	// is_default flag to strip instead).
	execCutover(t, pool, `UPDATE issue SET space_id = NULL WHERE workspace_id = $1 AND number = 1`, wsID)
	execCutover(t, pool, `UPDATE workspace_space SET archived_at = now() WHERE workspace_id = $1`, wsID)

	err := applyCutoverMigration(pool, schema, up162)
	if err == nil {
		t.Fatal("expected 162 to fail on remaining NULL space_id")
	}
	if !strings.Contains(err.Error(), "NULL space_id") {
		t.Fatalf("error %q does not mention the NULL space_id preflight", err)
	}
	if constraintExists(t, pool, "uq_issue_space_number") {
		t.Fatal("uq_issue_space_number must not be created when preflight fails")
	}
}
