package main

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"
)

func TestCommentSuppressedAgentsMigrationUpDownUpInIsolatedSchema(t *testing.T) {
	base := openTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	schema := createScratchSchema(t, ctx, base, "comment_suppressed_agents_")
	pool := openTestPoolWithSearchPath(t, schema)
	if _, err := pool.Exec(ctx, `CREATE TABLE comment (id UUID PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	for _, direction := range []string{"up", "down", "up"} {
		if err := runMigrations(ctx, pool, runOptions{
			Direction:             direction,
			Files:                 realMigrationFiles(t, []string{"550_comment_suppressed_agents"}, direction),
			SchemaMigrationsTable: schema + ".schema_migrations",
			AdvisoryLockKey:       int64(rand.Uint64()&0x7fffffffffffffff) | 1,
			Hooks:                 hooksForDirection(direction),
			Conditions:            conditionsForDirection(direction),
		}); err != nil {
			t.Fatalf("migrate %s: %v", direction, err)
		}
		var columns int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = 'comment' AND column_name = 'suppressed_agent_ids'
		`, schema).Scan(&columns); err != nil {
			t.Fatal(err)
		}
		if want := map[string]int{"up": 1, "down": 0}[direction]; columns != want {
			t.Fatalf("after %s, suppressed_agent_ids columns = %d, want %d", direction, columns, want)
		}
	}
}
