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

// Migration B (132_team_number_cutover) end-to-end invariant test.
//
// The full migration chain cannot be replayed locally (084/101 need pg15+
// UNIQUE NULLS NOT DISTINCT and the dev box only has pg14), and Docker is
// unavailable in most sandboxes, so this test is deliberately CI-shaped: it
// connects to whatever DATABASE_URL points at (default the local dev DSN),
// and if Postgres is unreachable it SKIPS cleanly — the same pattern as
// migrate_concurrent_test.go and every other live-Postgres test in the repo.
//
// Rather than replay all 132 migrations, it builds a *synthetic minimal
// schema* — just the tables and the uq_issue_workspace_number constraint that
// 131 and 132 actually reference — inside a private throwaway schema
// (cutover_test_<ts>_<rand>) with search_path pinned to it. Then it applies the
// real 131 + 132 SQL files through the production runMigrations loop and
// asserts the numbering invariant:
//
//   - re-backfill: a NULL-team straggler written during the deploy window is
//     assigned the default Team by 132;
//   - counter sync: the default Team's issue_counter never regresses and rises
//     to cover both max(number) and the legacy workspace.issue_counter;
//   - cutover: issue.team_id / autopilot.team_id become NOT NULL, the legacy
//     uq_issue_workspace_number is gone, uq_issue_team_number is in place;
//   - per-team numbering: two Teams' number-1 issues coexist in one workspace,
//     while a duplicate (team_id, number) is rejected.
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

// syntheticBaseSchema is the minimal pre-131 schema: only the columns and
// constraints that migrations 131 and 132 read or alter. Names must match
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
    workspace_id UUID NOT NULL
);
CREATE TABLE issue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    number INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'todo',
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    project_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number)
);
CREATE TABLE autopilot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL
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

func cutoverFilePaths(t *testing.T) (up131, up132 string) {
	t.Helper()
	dir, err := migrations.ResolveDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	up131 = filepath.Join(dir, "131_workspace_team.up.sql")
	up132 = filepath.Join(dir, "132_team_number_cutover.up.sql")
	for _, p := range []string{up131, up132} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("migration file missing: %s: %v", p, err)
		}
	}
	return up131, up132
}

// seedPre131 creates one workspace (prefix ENG, counter 3) with an owner and
// three legacy issues (numbers 1..3, NULL team) and returns the workspace id.
func seedPre131(t *testing.T, pool *pgxpool.Pool) string {
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
	for n := 1; n <= 3; n++ {
		execCutover(t, pool,
			`INSERT INTO issue (workspace_id, number, status) VALUES ($1, $2, 'todo')`, wsID, n)
	}
	return wsID
}

func constraintExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conname = $1 AND conrelid = 'issue'::regclass)`,
		name,
	).Scan(&exists); err != nil {
		t.Fatalf("check constraint %s: %v", name, err)
	}
	return exists
}

func TestMigration132CutoverInvariant(t *testing.T) {
	pool, schema := newCutoverSchema(t)
	up131, up132 := cutoverFilePaths(t)

	execCutover(t, pool, syntheticBaseSchema)
	wsID := seedPre131(t, pool)

	if err := applyCutoverMigration(pool, schema, up131); err != nil {
		t.Fatalf("apply 131: %v", err)
	}

	// Simulate the deploy window: an old, team-unaware instance mints issue #4
	// with a NULL team_id and advances the legacy workspace counter to 5.
	execCutover(t, pool, `INSERT INTO issue (workspace_id, number, status) VALUES ($1, 4, 'todo')`, wsID)
	execCutover(t, pool, `UPDATE workspace SET issue_counter = 5 WHERE id = $1`, wsID)

	if err := applyCutoverMigration(pool, schema, up132); err != nil {
		t.Fatalf("apply 132: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Re-backfill: no issue may still carry a NULL team_id.
	var nullTeamCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE team_id IS NULL`).Scan(&nullTeamCount); err != nil {
		t.Fatalf("count null-team issues: %v", err)
	}
	if nullTeamCount != 0 {
		t.Fatalf("re-backfill left %d NULL-team issues, want 0", nullTeamCount)
	}

	// Counter sync: default team counter = GREATEST(seeded 3, max number 4,
	// legacy ws counter 5) = 5. It must never regress.
	var defaultTeamID string
	var defaultCounter int
	var defaultKey string
	if err := pool.QueryRow(ctx,
		`SELECT id, key, issue_counter FROM workspace_team WHERE workspace_id = $1 AND is_default`, wsID,
	).Scan(&defaultTeamID, &defaultKey, &defaultCounter); err != nil {
		t.Fatalf("read default team: %v", err)
	}
	if defaultCounter != 5 {
		t.Fatalf("default team issue_counter = %d, want 5 (GREATEST of counter/max-number/ws-counter)", defaultCounter)
	}
	if defaultKey != "ENG" {
		t.Fatalf("default team key = %q, want ENG", defaultKey)
	}

	// Cutover: legacy uniqueness gone, team-scoped uniqueness present.
	if constraintExists(t, pool, "uq_issue_workspace_number") {
		t.Fatal("uq_issue_workspace_number should have been dropped")
	}
	if !constraintExists(t, pool, "uq_issue_team_number") {
		t.Fatal("uq_issue_team_number should exist after cutover")
	}

	// team_id is NOT NULL now: a NULL insert must fail.
	if _, err := pool.Exec(ctx,
		`INSERT INTO issue (workspace_id, team_id, number, status) VALUES ($1, NULL, 99, 'todo')`, wsID,
	); err == nil {
		t.Fatal("expected NOT NULL violation inserting issue with NULL team_id")
	}

	// Per-team numbering: a second Team's issue #1 coexists with ENG-1 in the
	// same workspace (impossible under the dropped workspace-number unique).
	var designTeamID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace_team (workspace_id, name, key, is_default) VALUES ($1, 'Design', 'DES', false) RETURNING id`, wsID,
	).Scan(&designTeamID); err != nil {
		t.Fatalf("insert design team: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO issue (workspace_id, team_id, number, status) VALUES ($1, $2, 1, 'todo')`, wsID, designTeamID,
	); err != nil {
		t.Fatalf("DES-1 should coexist with ENG-1: %v", err)
	}
	// But a duplicate (team_id, number) within one Team is rejected.
	if _, err := pool.Exec(ctx,
		`INSERT INTO issue (workspace_id, team_id, number, status) VALUES ($1, $2, 1, 'todo')`, wsID, designTeamID,
	); err == nil {
		t.Fatal("expected uq_issue_team_number violation on duplicate DES-1")
	}
}

func TestMigration132PreflightRejectsDuplicateTeamNumber(t *testing.T) {
	pool, schema := newCutoverSchema(t)
	up131, up132 := cutoverFilePaths(t)

	execCutover(t, pool, syntheticBaseSchema)
	wsID := seedPre131(t, pool)
	if err := applyCutoverMigration(pool, schema, up131); err != nil {
		t.Fatalf("apply 131: %v", err)
	}

	// Force a duplicate (team_id, number): the legacy constraint normally makes
	// this impossible, so drop it first, then mint a second default-team issue
	// reusing number 1.
	execCutover(t, pool, `ALTER TABLE issue DROP CONSTRAINT uq_issue_workspace_number`)
	execCutover(t, pool, `
		INSERT INTO issue (workspace_id, team_id, number, status)
		SELECT $1, wt.id, 1, 'todo' FROM workspace_team wt WHERE wt.workspace_id = $1 AND wt.is_default`, wsID)

	err := applyCutoverMigration(pool, schema, up132)
	if err == nil {
		t.Fatal("expected 132 to fail on duplicate (team_id, number)")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error %q does not mention duplicate preflight", err)
	}
	// Preflight RAISEs before the DDL, so the cutover constraint must not exist.
	if constraintExists(t, pool, "uq_issue_team_number") {
		t.Fatal("uq_issue_team_number must not be created when preflight fails")
	}
}

func TestMigration132PreflightRejectsNullTeam(t *testing.T) {
	pool, schema := newCutoverSchema(t)
	up131, up132 := cutoverFilePaths(t)

	execCutover(t, pool, syntheticBaseSchema)
	wsID := seedPre131(t, pool)
	if err := applyCutoverMigration(pool, schema, up131); err != nil {
		t.Fatalf("apply 131: %v", err)
	}

	// Create an un-backfillable straggler: null out an issue's team and remove
	// the default flag so 132's re-backfill cannot repair it.
	execCutover(t, pool, `UPDATE issue SET team_id = NULL WHERE workspace_id = $1 AND number = 1`, wsID)
	execCutover(t, pool, `UPDATE workspace_team SET is_default = false WHERE workspace_id = $1`, wsID)

	err := applyCutoverMigration(pool, schema, up132)
	if err == nil {
		t.Fatal("expected 132 to fail on remaining NULL team_id")
	}
	if !strings.Contains(err.Error(), "NULL team_id") {
		t.Fatalf("error %q does not mention the NULL team_id preflight", err)
	}
	if constraintExists(t, pool, "uq_issue_team_number") {
		t.Fatal("uq_issue_team_number must not be created when preflight fails")
	}
}
