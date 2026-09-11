package migrations

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const reserveTriageMigrationTestSchema = "reserve_triage_status_key_migration_test"

const (
	triageWSBoth  = "00000000-0000-0000-0000-00000000a001" // owns triage and triage_2
	triageWSOnly  = "00000000-0000-0000-0000-00000000a002" // owns triage only
	triageWSClean = "00000000-0000-0000-0000-00000000a003" // owns neither
)

// TestReserveTriageStatusKeyMigration covers the upgrade conflict of MUL-7212:
// a workspace that already owns a custom `triage` status must come out with a
// replacement key picked like issuestatus.firstFreeKey, and its catalog, issues
// and saved views must all agree on it afterwards.
func TestReserveTriageStatusKeyMigration(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+reserveTriageMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+reserveTriageMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, reserveTriageMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	// Only the columns the migration reads or writes.
	if _, err := conn.Exec(ctx, `
		CREATE TABLE issue_status (
			id UUID NOT NULL DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			key TEXT NOT NULL,
			category TEXT NOT NULL,
			archived_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (workspace_id, key)
		);
		CREATE TABLE issue (
			id UUID NOT NULL DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL,
			revision BIGINT NOT NULL DEFAULT 1
		);
		CREATE TABLE issue_view (
			id UUID NOT NULL DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL,
			name TEXT NOT NULL,
			query JSONB NOT NULL,
			revision INTEGER NOT NULL DEFAULT 1,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		t.Fatalf("create status tables: %v", err)
	}

	for _, seed := range []string{`
		INSERT INTO issue_status (workspace_id, key, category, archived_at) VALUES
			($1, 'triage', 'backlog', NULL),
			-- Archived rows still own their key, so triage_2 is not free.
			($1, 'triage_2', 'todo', now()),
			($2, 'triage', 'in_review', NULL),
			($3, 'todo_later', 'todo', NULL)
	`, `
		INSERT INTO issue (workspace_id, title, status, revision) VALUES
			($1, 'on triage A', 'triage', 3),
			($1, 'on triage B', 'triage', 1),
			($1, 'on triage_2', 'triage_2', 1),
			($2, 'on triage C', 'triage', 1),
			($3, 'clean todo', 'todo', 1)
	`, `
		INSERT INTO issue_view (workspace_id, name, query) VALUES
			($1, 'mixed', '{"statusFilters": ["todo", "triage", "triage_2"], "priorityFilters": ["high"]}'),
			($1, 'no status filter', '{"priorityFilters": ["high"]}'),
			($2, 'only triage', '{"statusFilters": ["triage"]}'),
			-- A workspace without a custom triage never had a status by that
			-- key, so its views are not the migration's to reinterpret.
			($3, 'stale value', '{"statusFilters": ["triage"]}')
	`} {
		if _, err := conn.Exec(ctx, seed, triageWSBoth, triageWSOnly, triageWSClean); err != nil {
			t.Fatalf("seed conflicting workspaces: %v", err)
		}
	}

	applyMigrationFile(t, ctx, conn.Conn(), "467_reserve_triage_status_key.up.sql")

	catalog := func(workspaceID string) []string {
		t.Helper()
		var keys []string
		if err := conn.QueryRow(ctx, `
			SELECT array_agg(key ORDER BY key) FROM issue_status WHERE workspace_id = $1
		`, workspaceID).Scan(&keys); err != nil {
			t.Fatalf("read catalog: %v", err)
		}
		return keys
	}
	if got, want := catalog(triageWSBoth), []string{"triage_2", "triage_3"}; !slices.Equal(got, want) {
		t.Errorf("catalog with triage and archived triage_2 = %v, want %v", got, want)
	}
	if got, want := catalog(triageWSOnly), []string{"triage_2"}; !slices.Equal(got, want) {
		t.Errorf("catalog with triage only = %v, want %v", got, want)
	}

	issues := map[string]struct {
		status   string
		revision int64
	}{}
	rows, err := conn.Query(ctx, `SELECT title, status, revision FROM issue`)
	if err != nil {
		t.Fatalf("read issues: %v", err)
	}
	for rows.Next() {
		var title, status string
		var revision int64
		if err := rows.Scan(&title, &status, &revision); err != nil {
			t.Fatalf("scan issue: %v", err)
		}
		issues[title] = struct {
			status   string
			revision int64
		}{status, revision}
	}
	rows.Close()
	wantIssues := map[string]struct {
		status   string
		revision int64
	}{
		// Moved issues bump their revision so a stale editor conflicts.
		"on triage A": {"triage_3", 4},
		"on triage B": {"triage_3", 2},
		"on triage_2": {"triage_2", 1},
		"on triage C": {"triage_2", 2},
		"clean todo":  {"todo", 1},
	}
	for title, want := range wantIssues {
		if got := issues[title]; got != want {
			t.Errorf("issue %q = %+v, want %+v", title, got, want)
		}
	}

	views := map[string]string{}
	viewRevisions := map[string]int{}
	rows, err = conn.Query(ctx, `SELECT name, query::text, revision FROM issue_view`)
	if err != nil {
		t.Fatalf("read views: %v", err)
	}
	for rows.Next() {
		var name, query string
		var revision int
		if err := rows.Scan(&name, &query, &revision); err != nil {
			t.Fatalf("scan view: %v", err)
		}
		views[name], viewRevisions[name] = query, revision
	}
	rows.Close()
	wantViews := map[string]struct {
		query    string
		revision int
	}{
		"mixed":            {`{"statusFilters": ["todo", "triage_3", "triage_2"], "priorityFilters": ["high"]}`, 2},
		"no status filter": {`{"priorityFilters": ["high"]}`, 1},
		"only triage":      {`{"statusFilters": ["triage_2"]}`, 2},
		"stale value":      {`{"statusFilters": ["triage"]}`, 1},
	}
	for name, want := range wantViews {
		if views[name] != want.query || viewRevisions[name] != want.revision {
			t.Errorf("view %q = %s (revision %d), want %s (revision %d)",
				name, views[name], viewRevisions[name], want.query, want.revision)
		}
	}

	// The reservation now holds in storage: an old pod that still accepts
	// `triage` as a custom key cannot recreate the conflict.
	assertInsertCheckViolation(t, ctx, conn,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'triage', 'todo')`, triageWSClean)

	applyMigrationFile(t, ctx, conn.Conn(), "467_reserve_triage_status_key.down.sql")
	if _, err := conn.Exec(ctx,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'triage', 'todo')`, triageWSClean); err != nil {
		t.Fatalf("insert after rollback dropped the reservation: %v", err)
	}
	// Re-applying handles the row the rollback window let in.
	applyMigrationFile(t, ctx, conn.Conn(), "467_reserve_triage_status_key.up.sql")
	if got, want := catalog(triageWSClean), []string{"todo_later", "triage_2"}; !slices.Equal(got, want) {
		t.Errorf("catalog after re-apply = %v, want %v", got, want)
	}
}

// TestIssueEffectiveStatusTriageMigration pins the SQL mirror of
// issuestatus.Effective: `triage` resolves to itself on the fast path, and
// custom keys still resolve through the catalog.
func TestIssueEffectiveStatusTriageMigration(t *testing.T) {
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

	const schema = "issue_effective_status_triage_migration_test"
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, schema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		CREATE TABLE issue_status (workspace_id UUID NOT NULL, key TEXT NOT NULL, category TEXT NOT NULL);
		INSERT INTO issue_status VALUES ('`+triageWSBoth+`', 'shipped', 'done');
	`); err != nil {
		t.Fatalf("create catalog: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "340_issue_effective_status_fn.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "468_issue_effective_status_triage.up.sql")

	cases := map[string]string{"triage": "triage", "todo": "todo", "shipped": "done", "ghost": "ghost"}
	for status, want := range cases {
		var got string
		if err := conn.QueryRow(ctx, `SELECT issue_effective_status($1, $2)`, triageWSBoth, status).Scan(&got); err != nil {
			t.Fatalf("issue_effective_status(%q): %v", status, err)
		}
		if got != want {
			t.Errorf("issue_effective_status(%q) = %q, want %q", status, got, want)
		}
	}

	applyMigrationFile(t, ctx, conn.Conn(), "468_issue_effective_status_triage.down.sql")
	var got string
	if err := conn.QueryRow(ctx, `SELECT issue_effective_status($1, 'shipped')`, triageWSBoth).Scan(&got); err != nil || got != "done" {
		t.Fatalf("after rollback issue_effective_status(shipped) = %q, %v; want done", got, err)
	}
}
