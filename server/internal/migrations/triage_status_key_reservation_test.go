package migrations

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	triageWSOwner = "00000000-0000-0000-0000-00000000a001" // owns a custom `triage` status
	triageWSClean = "00000000-0000-0000-0000-00000000a003" // owns neither
)

// triageMigrationSandbox holds a pool whose connections all work in one
// isolated schema, holding only the columns these migrations read or write.
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

func (s *triageMigrationSandbox) reservationExists(t *testing.T, ctx context.Context) bool {
	t.Helper()
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'issue_status_key_not_reserved'
			  AND conrelid = (quote_ident($1) || '.issue_status')::regclass
		)
	`, s.schema).Scan(&exists); err != nil {
		t.Fatalf("read reservation constraint: %v", err)
	}
	return exists
}

// The reservation of the `triage` status key is withdrawn (MUL-7400). What it
// protected — `status = 'triage'` meaning "in Triage" — ended when migration
// 483 gave Triage a column of its own, so 475-477 are emptied rather than
// shipped: no CHECK, and above all no rewrite of a workspace's status keys,
// issues and saved views in the deploy path.
func TestTriageStatusKeyReservationIsNotApplied(t *testing.T) {
	ctx := context.Background()
	s := newTriageMigrationSandbox(t, "triage_status_key_reservation_test")

	for _, seed := range []string{`
		INSERT INTO issue_status (workspace_id, key, category) VALUES
			($1, 'triage', 'backlog'),
			($2, 'todo_later', 'todo')
	`, `
		INSERT INTO issue (workspace_id, title, status, revision) VALUES
			($1, 'on triage', 'triage', 3),
			($2, 'clean todo', 'todo', 1)
	`, `
		INSERT INTO issue_view (workspace_id, name, query) VALUES
			($1, 'only triage', '{"statusFilters": ["triage"]}'),
			($2, 'stale value', '{"statusFilters": ["triage"]}')
	`} {
		if _, err := s.pool.Exec(ctx, seed, triageWSOwner, triageWSClean); err != nil {
			t.Fatalf("seed a workspace that owns a custom triage status: %v", err)
		}
	}

	for _, file := range []string{
		"475_issue_status_key_not_reserved.up.sql",
		"476_reserve_triage_status_key.up.sql",
		"477_issue_effective_status_triage.up.sql",
		"490_drop_triage_status_key_reservation.up.sql",
	} {
		applyMigrationFile(t, ctx, s.pool, file)
	}

	if got, want := s.catalog(t, ctx, triageWSOwner), []string{"triage"}; !slices.Equal(got, want) {
		t.Errorf("catalog = %v, want %v — the custom status must keep its key", got, want)
	}
	var status string
	var revision int64
	if err := s.pool.QueryRow(ctx,
		`SELECT status, revision FROM issue WHERE title = 'on triage'`).Scan(&status, &revision); err != nil {
		t.Fatalf("read issue: %v", err)
	}
	if status != "triage" || revision != 3 {
		t.Errorf("issue = {status:%q revision:%d}, want {triage 3} — untouched", status, revision)
	}
	var query string
	var viewRevision int
	if err := s.pool.QueryRow(ctx,
		`SELECT query::text, revision FROM issue_view WHERE name = 'only triage'`).Scan(&query, &viewRevision); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if query != `{"statusFilters": ["triage"]}` || viewRevision != 1 {
		t.Errorf("view = {query:%s revision:%d}, want the seeded filter at revision 1", query, viewRevision)
	}
	if n := countRows(t, ctx, s.pool, `SELECT count(*) FROM issue_view WHERE query -> 'statusFilters' @> '["triage"]'::jsonb`); n != 2 {
		t.Errorf("%d saved view(s) still filter on triage, want 2", n)
	}

	if s.reservationExists(t, ctx) {
		t.Error("the barrier CHECK was installed; nothing may reserve the key")
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'triage', 'todo')`, triageWSClean); err != nil {
		t.Fatalf("another workspace could not take the key: %v", err)
	}
}

// Emptying 475-477 covers every database that has not run them, production
// included. A database that did migrate off main already has the CHECK, and
// would refuse a custom status keyed `triage` that the server now accepts —
// so migration 490 drops it there.
func TestDropTriageStatusKeyReservationRepairsAMigratedDatabase(t *testing.T) {
	ctx := context.Background()
	s := newTriageMigrationSandbox(t, "triage_status_key_reservation_repair_test")

	// The state the original 475 + 476 left behind: a validated barrier.
	if _, err := s.pool.Exec(ctx, `
		ALTER TABLE issue_status
			ADD CONSTRAINT issue_status_key_not_reserved CHECK (key <> 'triage')
	`); err != nil {
		t.Fatalf("recreate the shipped barrier: %v", err)
	}
	assertInsertCheckViolation(t, ctx, s.pool,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'triage', 'todo')`, triageWSOwner)

	applyMigrationFile(t, ctx, s.pool, "490_drop_triage_status_key_reservation.up.sql")

	if s.reservationExists(t, ctx) {
		t.Fatal("490 left the barrier in place")
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO issue_status (workspace_id, key, category) VALUES ($1, 'triage', 'todo')`, triageWSOwner); err != nil {
		t.Fatalf("insert after 490: %v", err)
	}

	// Rolling 490 back must not hand the barrier back: a row it forbids now
	// exists, and the server no longer reads the key as anything special.
	applyMigrationFile(t, ctx, s.pool, "490_drop_triage_status_key_reservation.down.sql")
	if s.reservationExists(t, ctx) {
		t.Error("490 down restored a barrier the catalog would violate")
	}
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}
