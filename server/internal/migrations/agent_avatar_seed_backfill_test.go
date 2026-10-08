package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAgentAvatarSeedBackfill covers migration 565 (MAKE-291):
//   - every pre-existing agent row gains a non-null unique seed,
//   - avatar_url is byte-for-byte untouched (legacy emoji stays persisted),
//   - an already-present seed is never overwritten,
//   - the whole migration is idempotent (re-running is a no-op),
//   - newly inserted rows get a seed from the column DEFAULT.
func TestAgentAvatarSeedBackfill(t *testing.T) {
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

	schema := "agent_avatar_seed_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}

	// Minimal legacy `agent` shape: only the columns migration 565 touches
	// have to exist.
	if _, err := conn.Exec(ctx, `
		CREATE TABLE agent (
			id UUID PRIMARY KEY,
			name TEXT NOT NULL,
			avatar_url TEXT
		);
		INSERT INTO agent (id, name, avatar_url) VALUES
			('11111111-1111-1111-1111-111111111111', 'legacy-emoji', 'emoji:👔'),
			('22222222-2222-2222-2222-222222222222', 'legacy-image', 'https://cdn.example.com/a.png'),
			('33333333-3333-3333-3333-333333333333', 'legacy-null', NULL);
	`); err != nil {
		t.Fatalf("create legacy fixture: %v", err)
	}

	apply := func(attempt string) {
		t.Helper()
		if _, err := conn.Exec(ctx, readMigrationFile(t, "565_agent_avatar_seed.up.sql")); err != nil {
			t.Fatalf("%s: apply 565 up: %v", attempt, err)
		}
	}

	apply("first run")

	rows, err := conn.Query(ctx, `SELECT id, name, avatar_url, avatar_seed FROM agent ORDER BY name`)
	if err != nil {
		t.Fatalf("query backfilled agents: %v", err)
	}
	type row struct {
		id   uuid.UUID
		name string
		url  *string
		seed string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name, &r.url, &r.seed); err != nil {
			t.Fatalf("scan agent: %v", err)
		}
		got = append(got, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate agents: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("agent rows = %d, want 3", len(got))
	}

	seeds := map[string]bool{}
	for _, r := range got {
		if r.seed == "" {
			t.Errorf("agent %s (%s): avatar_seed is empty after backfill", r.name, r.id)
		}
		if seeds[r.seed] {
			t.Errorf("agent %s: avatar_seed %q collides with another agent's seed", r.name, r.seed)
		}
		seeds[r.seed] = true
	}
	// avatar_url must be byte-for-byte what it was before the migration.
	wantURL := map[string]*string{
		"legacy-emoji": ptr("emoji:👔"),
		"legacy-image": ptr("https://cdn.example.com/a.png"),
		"legacy-null":  nil,
	}
	for _, r := range got {
		want := wantURL[r.name]
		switch {
		case want == nil && r.url != nil:
			t.Errorf("agent %s: avatar_url = %q, want NULL (backfill must not write avatar_url)", r.name, *r.url)
		case want != nil && r.url == nil:
			t.Errorf("agent %s: avatar_url = NULL, want %q", r.name, *want)
		case want != nil && *r.url != *want:
			t.Errorf("agent %s: avatar_url = %q, want %q (backfill must not rewrite avatar_url)", r.name, *r.url, *want)
		}
	}

	// Snapshot seeds, then re-run: idempotent — no seed may change.
	snapshot := map[uuid.UUID]string{}
	for _, r := range got {
		snapshot[r.id] = r.seed
	}
	apply("idempotent re-run")

	rows2, err := conn.Query(ctx, `SELECT id, name, avatar_url, avatar_seed FROM agent ORDER BY name`)
	if err != nil {
		t.Fatalf("query after re-run: %v", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var (
			id   uuid.UUID
			name string
			url  *string
			seed string
		)
		if err := rows2.Scan(&id, &name, &url, &seed); err != nil {
			t.Fatalf("scan agent after re-run: %v", err)
		}
		if seed != snapshot[id] {
			t.Errorf("agent %s: seed changed across re-run: %q -> %q (backfill must never overwrite a seed)", name, snapshot[id], seed)
		}
	}
	if err := rows2.Err(); err != nil {
		t.Fatalf("iterate agents after re-run: %v", err)
	}

	// A row inserted after the migration picks up a seed from the DEFAULT —
	// the path every new agent creation INSERT takes.
	if _, err := conn.Exec(ctx, `INSERT INTO agent (id, name) VALUES ('44444444-4444-4444-4444-444444444444', 'new-agent')`); err != nil {
		t.Fatalf("insert post-migration agent: %v", err)
	}
	var newSeed string
	var newURL *string
	if err := conn.QueryRow(ctx, `SELECT avatar_seed, avatar_url FROM agent WHERE name = 'new-agent'`).Scan(&newSeed, &newURL); err != nil {
		t.Fatalf("read post-migration agent: %v", err)
	}
	if newSeed == "" {
		t.Error("post-migration insert: avatar_seed DEFAULT did not mint a seed")
	}
	if newURL != nil {
		t.Errorf("post-migration insert: avatar_url = %q, want NULL", *newURL)
	}
	if seeds[newSeed] {
		t.Errorf("post-migration insert: DEFAULT seed %q collides with a backfilled seed", newSeed)
	}
}

func ptr(s string) *string { return &s }
