package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const legacyPlatformSkillNamesMigrationTestSchema = "legacy_platform_skill_names_migration_test"

// TestLegacyPlatformSkillNamesMigrationRewritesOnlyRetiredNames pins the two
// halves of migration 451 that are easy to get wrong in opposite directions: a
// rewrite that misses an occurrence leaves an agent pointing at a skill the
// server no longer ships, and a rewrite that is too eager corrupts a name that
// still resolves. The survivors below are the real ones, not hypotheticals --
// `multica-working-on-issues` is still shipped as a redirect stub, and a
// workspace is free to own a skill whose slug merely starts with a retired
// name.
func TestLegacyPlatformSkillNamesMigrationRewritesOnlyRetiredNames(t *testing.T) {
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
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+legacyPlatformSkillNamesMigrationTestSchema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+legacyPlatformSkillNamesMigrationTestSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, legacyPlatformSkillNamesMigrationTestSchema); err != nil {
		t.Fatalf("set isolated migration search path: %v", err)
	}

	// Only the columns the migration reads and writes. The real tables carry
	// far more, none of which the rewrite can observe.
	if _, err := conn.Exec(ctx, `
		CREATE TABLE agent (
			id UUID PRIMARY KEY,
			instructions TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE squad (
			id UUID PRIMARY KEY,
			instructions TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		t.Fatalf("create agent and squad fixtures: %v", err)
	}

	cases := []struct {
		name  string
		id    string
		given string
		want  string
	}{
		{
			name:  "retired name in backticks",
			id:    "00000000-0000-0000-0000-000000000001",
			given: "Load the `multica-squads` skill before delegating.",
			want:  "Load the `multica-platform` skill before delegating.",
		},
		{
			name:  "several retired names in one body",
			id:    "00000000-0000-0000-0000-000000000002",
			given: "See multica-autopilots and multica-mentioning.",
			want:  "See multica-platform and multica-platform.",
		},
		{
			name:  "longest retired slug",
			id:    "00000000-0000-0000-0000-000000000003",
			given: "multica-projects-and-resources at line start",
			want:  "multica-platform at line start",
		},
		{
			name:  "remaining retired names",
			id:    "00000000-0000-0000-0000-000000000004",
			given: "multica-creating-agents multica-runtimes-and-repos multica-skill-importing",
			want:  "multica-platform multica-platform multica-platform",
		},
		{
			name:  "already migrated",
			id:    "00000000-0000-0000-0000-000000000005",
			given: "Load the multica-platform skill.",
			want:  "Load the multica-platform skill.",
		},
		{
			name:  "workspace skill extending a retired slug",
			id:    "00000000-0000-0000-0000-000000000006",
			given: "Our own multica-squads-qa skill owns this.",
			want:  "Our own multica-squads-qa skill owns this.",
		},
		{
			name:  "retired slug as a suffix of another token",
			id:    "00000000-0000-0000-0000-000000000007",
			given: "Our own acme-multica-squads skill owns this.",
			want:  "Our own acme-multica-squads skill owns this.",
		},
		{
			name:  "still-shipped redirect stub",
			id:    "00000000-0000-0000-0000-000000000008",
			given: "Read multica-working-on-issues for the issue contracts.",
			want:  "Read multica-working-on-issues for the issue contracts.",
		},
		{
			name:  "unrelated built-in",
			id:    "00000000-0000-0000-0000-000000000009",
			given: "Mika owns multica-onboarding.",
			want:  "Mika owns multica-onboarding.",
		},
		{
			name:  "no skill reference at all",
			id:    "00000000-0000-0000-0000-00000000000a",
			given: "Answer in Chinese and keep replies short.",
			want:  "Answer in Chinese and keep replies short.",
		},
	}

	for _, tc := range cases {
		for _, table := range []string{"agent", "squad"} {
			if _, err := conn.Exec(ctx,
				"INSERT INTO "+table+" (id, instructions) VALUES ($1, $2)",
				tc.id, tc.given,
			); err != nil {
				t.Fatalf("seed %s %s: %v", table, tc.name, err)
			}
		}
	}

	// An untouched row must keep its original updated_at, so the timestamps
	// have to be distinguishable from the migration's now().
	if _, err := conn.Exec(ctx, `
		UPDATE agent SET updated_at = TIMESTAMPTZ '2020-01-01 00:00:00+00';
		UPDATE squad SET updated_at = TIMESTAMPTZ '2020-01-01 00:00:00+00';
	`); err != nil {
		t.Fatalf("age fixture rows: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "451_rewrite_legacy_platform_skill_names.up.sql")

	for _, tc := range cases {
		for _, table := range []string{"agent", "squad"} {
			var got string
			var touched bool
			if err := conn.QueryRow(ctx,
				"SELECT instructions, updated_at > TIMESTAMPTZ '2020-01-01 00:00:00+00' FROM "+table+" WHERE id = $1",
				tc.id,
			).Scan(&got, &touched); err != nil {
				t.Fatalf("read %s %s: %v", table, tc.name, err)
			}
			if got != tc.want {
				t.Errorf("%s %s: instructions = %q, want %q", table, tc.name, got, tc.want)
			}
			// updated_at moves exactly with the text, so an unchanged row is
			// not reported to the rest of the system as edited.
			if wantTouched := tc.given != tc.want; touched != wantTouched {
				t.Errorf("%s %s: updated_at bumped = %v, want %v", table, tc.name, touched, wantTouched)
			}
		}
	}

	// Re-running must be a no-op. Migrations are applied once, but a rewrite
	// that is not idempotent is a rewrite that can still match its own output.
	applyMigrationFile(t, ctx, conn.Conn(), "451_rewrite_legacy_platform_skill_names.up.sql")

	var remaining int
	if err := conn.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT instructions FROM agent
			UNION ALL
			SELECT instructions FROM squad
		) rows
		WHERE instructions ~ '(?<![A-Za-z0-9-])multica-(autopilots|creating-agents|mentioning|projects-and-resources|runtimes-and-repos|skill-importing|squads)(?![A-Za-z0-9-])'
	`).Scan(&remaining); err != nil {
		t.Fatalf("recount retired names: %v", err)
	}
	if remaining != 0 {
		t.Errorf("retired skill names still present in %d rows, want 0", remaining)
	}

	for _, tc := range cases {
		var got string
		if err := conn.QueryRow(ctx, "SELECT instructions FROM agent WHERE id = $1", tc.id).Scan(&got); err != nil {
			t.Fatalf("re-read agent %s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("agent %s after second apply: instructions = %q, want %q", tc.name, got, tc.want)
		}
	}
}
