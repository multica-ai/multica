package migrations

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	triageWSBoth  = "00000000-0000-0000-0000-00000000a001" // owns triage and triage_2
	triageWSOnly  = "00000000-0000-0000-0000-00000000a002" // owns triage only
	triageWSClean = "00000000-0000-0000-0000-00000000a003" // owns neither
)

// triageMigrationSandbox holds a pool whose connections all work in one
// isolated schema, holding only the columns migrations 468-469 read or write.
type triageMigrationSandbox struct {
	pool   *pgxpool.Pool
	schema string
}

func newTriageMigrationSandbox(t *testing.T, schema string) *triageMigrationSandbox {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	// Every pooled connection, not just the first, must resolve the
	// migrations' unqualified table names inside the sandbox.
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		pool.Close()
	})
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `
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
	return &triageMigrationSandbox{pool: pool, schema: schema}
}

// reserve applies the two reservation migrations the way the runner does: each
// file on its own, so 468's barrier commits before 469 scans.
func (s *triageMigrationSandbox) reserve(t *testing.T, ctx context.Context) {
	t.Helper()
	applyMigrationFile(t, ctx, s.pool, "468_issue_status_key_not_reserved.up.sql")
	applyMigrationFile(t, ctx, s.pool, "469_reserve_triage_status_key.up.sql")
}

func (s *triageMigrationSandbox) catalog(t *testing.T, ctx context.Context, workspaceID string) []string {
	t.Helper()
	var keys []string
	if err := s.pool.QueryRow(ctx, `
		SELECT array_agg(key ORDER BY key) FROM issue_status WHERE workspace_id = $1
	`, workspaceID).Scan(&keys); err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	return keys
}

func (s *triageMigrationSandbox) constraintValidated(t *testing.T, ctx context.Context) bool {
	t.Helper()
	var validated bool
	if err := s.pool.QueryRow(ctx, `
		SELECT convalidated FROM pg_constraint
		WHERE conname = 'issue_status_key_not_reserved'
		  AND conrelid = (quote_ident($1) || '.issue_status')::regclass
	`, s.schema).Scan(&validated); err != nil {
		t.Fatalf("read reservation constraint: %v", err)
	}
	return validated
}

// TestReserveTriageStatusKeyMigration covers the upgrade conflict of MUL-7212:
// a workspace that already owns a custom `triage` status must come out with a
// replacement key picked like issuestatus.firstFreeKey, and its catalog, issues
// and saved views must all agree on it afterwards.
func TestReserveTriageStatusKeyMigration(t *testing.T) {
	ctx := context.Background()
	s := newTriageMigrationSandbox(t, "reserve_triage_status_key_migration_test")

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
		if _, err := s.pool.Exec(ctx, seed, triageWSBoth, triageWSOnly, triageWSClean); err != nil {
			t.Fatalf("seed conflicting workspaces: %v", err)
		}
	}

	s.reserve(t, ctx)

	if got, want := s.catalog(t, ctx, triageWSBoth), []string{"triage_2", "triage_3"}; !slices.Equal(got, want) {
		t.Errorf("catalog with triage and archived triage_2 = %v, want %v", got, want)
	}
	if got, want := s.catalog(t, ctx, triageWSOnly), []string{"triage_2"}; !slices.Equal(got, want) {
		t.Errorf("catalog with triage only = %v, want %v", got, want)
	}
	if !s.constraintValidated(t, ctx) {
		t.Error("469 left the reservation NOT VALID")
	}

	type issueState struct {
		status   string
		revision int64
	}
	issues := map[string]issueState{}
	rows, err := s.pool.Query(ctx, `SELECT title, status, revision FROM issue`)
	if err != nil {
		t.Fatalf("read issues: %v", err)
	}
	for rows.Next() {
		var title string
		var state issueState
		if err := rows.Scan(&title, &state.status, &state.revision); err != nil {
			t.Fatalf("scan issue: %v", err)
		}
		issues[title] = state
	}
	rows.Close()
	for title, want := range map[string]issueState{
		// Moved issues bump their revision so a stale editor conflicts.
		"on triage A": {"triage_3", 4},
		"on triage B": {"triage_3", 2},
		"on triage_2": {"triage_2", 1},
		"on triage C": {"triage_2", 2},
		"clean todo":  {"todo", 1},
	} {
		if got := issues[title]; got != want {
			t.Errorf("issue %q = %+v, want %+v", title, got, want)
		}
	}

	type viewState struct {
		query    string
		revision int
	}
	views := map[string]viewState{}
	rows, err = s.pool.Query(ctx, `SELECT name, query::text, revision FROM issue_view`)
	if err != nil {
		t.Fatalf("read views: %v", err)
	}
	for rows.Next() {
		var name string
		var state viewState
		if err := rows.Scan(&name, &state.query, &state.revision); err != nil {
			t.Fatalf("scan view: %v", err)
		}
		views[name] = state
	}
	rows.Close()
	for name, want := range map[string]viewState{
		"mixed":            {`{"statusFilters": ["todo", "triage_3", "triage_2"], "priorityFilters": ["high"]}`, 2},
		"no status filter": {`{"priorityFilters": ["high"]}`, 1},
		"only triage":      {`{"statusFilters": ["triage_2"]}`, 2},
		"stale value":      {`{"statusFilters": ["triage"]}`, 1},
	} {
		if got := views[name]; got != want {
			t.Errorf("view %q = %+v, want %+v", name, got, want)
		}
	}

	// The reservation now holds in storage: an old pod that still accepts
	// `triage` as a custom key cannot recreate the conflict.
	assertInsertCheckViolation(t, ctx, s.pool,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'triage', 'todo')`, triageWSClean)

	applyMigrationFile(t, ctx, s.pool, "469_reserve_triage_status_key.down.sql")
	applyMigrationFile(t, ctx, s.pool, "468_issue_status_key_not_reserved.down.sql")
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'triage', 'todo')`, triageWSClean); err != nil {
		t.Fatalf("insert after rollback dropped the reservation: %v", err)
	}
	// Re-applying handles the row the rollback window let in.
	s.reserve(t, ctx)
	if got, want := s.catalog(t, ctx, triageWSClean), []string{"todo_later", "triage_2"}; !slices.Equal(got, want) {
		t.Errorf("catalog after re-apply = %v, want %v", got, want)
	}
}

// TestReserveTriageStatusKeySurvivesAConcurrentCreate is the rollout race: an
// old pod, which still accepts `triage` as a custom key, creates one in a
// workspace that had no conflict while the migrations run. Scanning first and
// adding the CHECK last let that row fail the final validation and abort the
// deploy. With the barrier committed before the scan, the in-flight create is
// either repaired by 469 or refused outright — the deploy never fails on it.
func TestReserveTriageStatusKeySurvivesAConcurrentCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := newTriageMigrationSandbox(t, "reserve_triage_status_key_race_test")

	migrator, err := s.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire migrator connection: %v", err)
	}
	defer migrator.Release()
	oldPod, err := s.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire old-pod connection: %v", err)
	}
	defer oldPod.Release()
	var migratorPID int32
	if err := migrator.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&migratorPID); err != nil {
		t.Fatalf("read migrator pid: %v", err)
	}

	// The old pod is mid-create when the migration starts.
	tx, err := oldPod.Begin(ctx)
	if err != nil {
		t.Fatalf("begin old-pod create: %v", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'triage', 'todo')`, triageWSClean); err != nil {
		t.Fatalf("old-pod create: %v", err)
	}

	barrierSQL := readMigrationFile(t, "468_issue_status_key_not_reserved.up.sql")
	barrier := make(chan error, 1)
	go func() {
		_, err := migrator.Exec(ctx, barrierSQL)
		barrier <- err
	}()

	// The barrier needs the table to itself, so it waits for the create.
	for {
		var waiting bool
		if err := s.pool.QueryRow(ctx,
			`SELECT wait_event_type = 'Lock' FROM pg_stat_activity WHERE pid = $1`, migratorPID).Scan(&waiting); err != nil {
			t.Fatalf("watch migrator: %v", err)
		}
		if waiting {
			break
		}
		select {
		case err := <-barrier:
			t.Fatalf("barrier finished before the in-flight create committed: %v", err)
		case <-ctx.Done():
			t.Fatal("migrator never waited on the in-flight create")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit old-pod create: %v", err)
	}
	if err := <-barrier; err != nil {
		t.Fatalf("apply barrier after a concurrent create: %v", err)
	}

	// From here on the old pod cannot create another one anywhere.
	assertInsertCheckViolation(t, ctx, oldPod,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'triage', 'todo')`, triageWSOnly)

	applyMigrationFile(t, ctx, migrator, "469_reserve_triage_status_key.up.sql")
	if got, want := s.catalog(t, ctx, triageWSClean), []string{"triage_2"}; !slices.Equal(got, want) {
		t.Errorf("catalog after the race = %v, want the in-flight create renamed to [triage_2]", got)
	}
	if !s.constraintValidated(t, ctx) {
		t.Error("469 left the reservation NOT VALID")
	}
}

// TestIssueEffectiveStatusTriageMigration pins the SQL mirror of
// issuestatus.Effective: `triage` resolves to itself on the fast path, and
// custom keys still resolve through the catalog.
func TestIssueEffectiveStatusTriageMigration(t *testing.T) {
	ctx := context.Background()
	s := newTriageMigrationSandbox(t, "issue_effective_status_triage_migration_test")
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'shipped', 'done')`, triageWSBoth); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}

	applyMigrationFile(t, ctx, s.pool, "340_issue_effective_status_fn.up.sql")
	applyMigrationFile(t, ctx, s.pool, "470_issue_effective_status_triage.up.sql")

	cases := map[string]string{"triage": "triage", "todo": "todo", "shipped": "done", "ghost": "ghost"}
	for status, want := range cases {
		var got string
		if err := s.pool.QueryRow(ctx, `SELECT issue_effective_status($1, $2)`, triageWSBoth, status).Scan(&got); err != nil {
			t.Fatalf("issue_effective_status(%q): %v", status, err)
		}
		if got != want {
			t.Errorf("issue_effective_status(%q) = %q, want %q", status, got, want)
		}
	}

	applyMigrationFile(t, ctx, s.pool, "470_issue_effective_status_triage.down.sql")
	var got string
	if err := s.pool.QueryRow(ctx, `SELECT issue_effective_status($1, 'shipped')`, triageWSBoth).Scan(&got); err != nil || got != "done" {
		t.Fatalf("after rollback issue_effective_status(shipped) = %q, %v; want done", got, err)
	}
}
