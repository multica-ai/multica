package runtimetools

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestValidateToolsConfig covers the three states the seeder cares about:
// empty (OK), well-formed (OK), garbage (warn).
func TestValidateToolsConfig(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", false},
		{"valid empty mcpServers", `{"mcpServers": {}}`, false},
		{"valid one server", `{"mcpServers": {"foo": {"command": "x"}}}`, false},
		{"extra top-level keys", `{"mcpServers": {}, "future": 1}`, false},
		{"mcpServers omitted", `{"future": 1}`, false},
		{"not a JSON object", `[1, 2, 3]`, true},
		{"truncated", `{"mcpServers":`, true},
		{"mcpServers is array", `{"mcpServers": [1, 2]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateToolsConfig([]byte(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateToolsConfig(%q): got err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

// TestSeedCloudToolsRejectsNilPool guards a footgun: passing nil pool
// shouldn't crash, it should return an error. The startup wrapper logs and
// continues; a panic would take down the server.
func TestSeedCloudToolsRejectsNilPool(t *testing.T) {
	err := SeedCloudToolsForAllRuntimes(context.Background(), nil, []BackfillToolMeta{{Name: "x"}})
	if err == nil {
		t.Fatal("expected error for nil pool, got nil")
	}
}

// TestSeedCloudToolsEmptyToolListNoOp confirms an empty cloud list is
// handled cleanly (still scans tools_config for malformed warnings, but
// doesn't return an error).
func TestSeedCloudToolsEmptyToolListNoOp(t *testing.T) {
	if runtimeToolsPool == nil {
		t.Skip("DATABASE_URL not set")
	}
	if err := SeedCloudToolsForAllRuntimes(context.Background(), runtimeToolsPool, nil); err != nil {
		t.Errorf("empty cloud list should be a no-op, got %v", err)
	}
}

// TestSeedCloudToolsIsIdempotent is the contract that lets us call the
// seeder on every server startup: a second run must not duplicate rows.
// Requires migration 9034's NULLS NOT DISTINCT unique constraint to be
// applied — without it, ON CONFLICT does not match cloud (NULL
// mcp_server_name) rows and the second insert silently duplicates.
func TestSeedCloudToolsIsIdempotent(t *testing.T) {
	if runtimeToolsPool == nil {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()

	// Make a minimal workspace + runtime to seed against. We tear it down
	// afterwards via ON DELETE CASCADE on cerebro_runtime_tool's
	// runtime_id FK so the test stays self-contained.
	var wsID pgtype.UUID
	if err := runtimeToolsPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('rt-tools-backfill-test', $1) RETURNING id`,
		"rt-tools-backfill-test-"+t.Name(),
	).Scan(&wsID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeToolsPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	var runtimeID pgtype.UUID
	if err := runtimeToolsPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		 VALUES ($1, 'rt-tools-backfill', 'local', 'claude-code', 'online')
		 RETURNING id`,
		wsID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}

	cloud := []BackfillToolMeta{
		{Name: "test_tool_alpha", Description: "alpha"},
		{Name: "test_tool_beta", Description: "beta"},
	}

	for i := 0; i < 3; i++ {
		if err := SeedCloudToolsForAllRuntimes(ctx, runtimeToolsPool, cloud); err != nil {
			t.Fatalf("seed pass %d: %v", i, err)
		}
	}

	var count int
	if err := runtimeToolsPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM cerebro_runtime_tool
		 WHERE runtime_id = $1 AND source = 'cloud'
		   AND tool_name IN ('test_tool_alpha', 'test_tool_beta')`,
		runtimeID,
	).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 2 {
		t.Errorf("after 3 seed passes expected 2 cloud rows, got %d", count)
	}

	// Cloud rows should be enabled=true so admin/owner bypass kicks in.
	var enabledCount int
	if err := runtimeToolsPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM cerebro_runtime_tool
		 WHERE runtime_id = $1 AND enabled = true AND source = 'cloud'
		   AND tool_name IN ('test_tool_alpha', 'test_tool_beta')`,
		runtimeID,
	).Scan(&enabledCount); err != nil {
		t.Fatalf("count enabled: %v", err)
	}
	if enabledCount != 2 {
		t.Errorf("expected both cloud rows enabled, got %d", enabledCount)
	}
}

// TestSeedCloudToolsPreservesAdminToggle is the regression that an admin
// who disabled a tool via the UI does NOT see it re-enabled on the next
// server restart. The UpsertCerebroRuntimeTool query intentionally omits
// `enabled` from the ON CONFLICT update clause so the flag is sticky.
func TestSeedCloudToolsPreservesAdminToggle(t *testing.T) {
	if runtimeToolsPool == nil {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()

	var wsID pgtype.UUID
	if err := runtimeToolsPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('rt-tools-toggle-test', $1) RETURNING id`,
		"rt-tools-toggle-test-"+t.Name(),
	).Scan(&wsID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeToolsPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	var runtimeID pgtype.UUID
	if err := runtimeToolsPool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		 VALUES ($1, 'rt-tools-toggle', 'local', 'claude-code', 'online')
		 RETURNING id`,
		wsID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}

	cloud := []BackfillToolMeta{{Name: "test_tool_toggle", Description: "toggle"}}

	if err := SeedCloudToolsForAllRuntimes(ctx, runtimeToolsPool, cloud); err != nil {
		t.Fatalf("first seed: %v", err)
	}

	// Admin disables the tool through the admin UI (simulated direct UPDATE).
	if _, err := runtimeToolsPool.Exec(ctx,
		`UPDATE cerebro_runtime_tool SET enabled = false
		 WHERE runtime_id = $1 AND tool_name = 'test_tool_toggle'`,
		runtimeID,
	); err != nil {
		t.Fatalf("update enabled=false: %v", err)
	}

	if err := SeedCloudToolsForAllRuntimes(ctx, runtimeToolsPool, cloud); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	var enabled bool
	if err := runtimeToolsPool.QueryRow(ctx,
		`SELECT enabled FROM cerebro_runtime_tool
		 WHERE runtime_id = $1 AND tool_name = 'test_tool_toggle'`,
		runtimeID,
	).Scan(&enabled); err != nil {
		t.Fatalf("read enabled: %v", err)
	}
	if enabled {
		t.Errorf("admin toggle-off was overwritten by re-seed (enabled=%v)", enabled)
	}
}
