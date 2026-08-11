package migrations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
)

// Break caught: reclassifying the built-in Platform Agent CLI runtime must not
// change custom-profile or similarly named runtime rows.
func TestPlatformAgentCloudRuntimeMigration(t *testing.T) {
	pool := pooltestdb.Open(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin migration fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback migration fixture: %v", err)
		}
	})

	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE agent_runtime (
			id text PRIMARY KEY,
			provider text NOT NULL,
			profile_id uuid,
			runtime_mode text NOT NULL,
			updated_at timestamptz NOT NULL DEFAULT now()
		);
		INSERT INTO agent_runtime (id, provider, profile_id, runtime_mode) VALUES
			('builtin-platform', 'platform-agent-cli', NULL, 'local'),
			('custom-platform', 'platform-agent-cli', gen_random_uuid(), 'local'),
			('similar-provider', 'platform-agent-cli-compatible', NULL, 'local'),
			('ordinary-provider', 'codex', NULL, 'local');
	`); err != nil {
		t.Fatalf("seed migration fixture: %v", err)
	}

	up, err := os.ReadFile(filepath.Join(realMigrationsDir(t), "280_platform_agent_cloud_runtime.up.sql"))
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply up migration: %v", err)
	}
	assertRuntimeModes(t, ctx, tx, map[string]string{
		"builtin-platform":  "cloud",
		"custom-platform":   "local",
		"similar-provider":  "local",
		"ordinary-provider": "local",
	})

	down, err := os.ReadFile(filepath.Join(realMigrationsDir(t), "280_platform_agent_cloud_runtime.down.sql"))
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply down migration: %v", err)
	}
	assertRuntimeModes(t, ctx, tx, map[string]string{
		"builtin-platform":  "local",
		"custom-platform":   "local",
		"similar-provider":  "local",
		"ordinary-provider": "local",
	})
}

func assertRuntimeModes(t *testing.T, ctx context.Context, tx pgx.Tx, want map[string]string) {
	t.Helper()
	rows, err := tx.Query(ctx, `SELECT id, runtime_mode FROM agent_runtime`)
	if err != nil {
		t.Fatalf("query runtime modes: %v", err)
	}
	defer rows.Close()
	got := make(map[string]string, len(want))
	for rows.Next() {
		var id, mode string
		if err := rows.Scan(&id, &mode); err != nil {
			t.Fatalf("scan runtime mode: %v", err)
		}
		got[id] = mode
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime modes: %v", err)
	}
	for id, mode := range want {
		if got[id] != mode {
			t.Errorf("runtime %s mode = %q, want %q", id, got[id], mode)
		}
	}
}
