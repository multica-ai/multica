package feature_flags

// Integration tests for the workspace-level feature-flag override queries
// added in FIR-2505. They prove the round trip the handler relies on: an
// owner-set workspace override is stored under the all-zero sentinel user_id,
// is returned by the workspace List with its locked flag, is invisible to the
// per-user List, and is removed by Delete. Tests skip cleanly when no test
// database is reachable.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

const (
	ffTestEmail         = "featureflags-test@multica.ai"
	ffTestName          = "Feature Flags Test"
	ffTestWorkspaceSlug = "featureflags-tests"
)

var (
	ffTestPool        *pgxpool.Pool
	ffTestWorkspaceID pgtype.UUID
	ffTestUserID      pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping feature_flags integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping feature_flags integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}

	if err := cleanupFFTestFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to clean feature_flags test fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := setupFFTestFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to set up feature_flags test fixture: %v\n", err)
		_ = cleanupFFTestFixture(ctx, pool)
		pool.Close()
		os.Exit(1)
	}

	ffTestPool = pool
	code := m.Run()

	if err := cleanupFFTestFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean feature_flags test fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupFFTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, ffTestName, ffTestEmail).Scan(&ffTestUserID); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Feature Flags Tests", ffTestWorkspaceSlug, "Temporary workspace", "FFT").Scan(&ffTestWorkspaceID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	return nil
}

func cleanupFFTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	// cerebro_feature_flags has no workspace FK cascade, so clear rows first.
	if _, err := pool.Exec(ctx, `
		DELETE FROM cerebro_feature_flags
		WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)
	`, ffTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, ffTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, ffTestEmail); err != nil {
		return err
	}
	return nil
}

// TestWorkspaceFlag_RoundTrip proves the workspace override is stored, listed
// with its locked flag, hidden from the per-user list, and deletable.
func TestWorkspaceFlag_RoundTrip(t *testing.T) {
	if ffTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	q := cerebrodb.New(ffTestPool)
	const flag = "cerebro_tool_policy"

	// Owner forces the flag on and locks it.
	if err := q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: ffTestWorkspaceID,
		FlagKey:     flag,
		Enabled:     true,
		Locked:      true,
	}); err != nil {
		t.Fatalf("upsert workspace flag: %v", err)
	}

	wsRows, err := q.ListCerebroWorkspaceFeatureFlags(ctx, ffTestWorkspaceID)
	if err != nil {
		t.Fatalf("list workspace flags: %v", err)
	}
	if len(wsRows) != 1 || wsRows[0].FlagKey != flag || !wsRows[0].Enabled || !wsRows[0].Locked {
		t.Fatalf("workspace list = %+v, want one enabled+locked %q row", wsRows, flag)
	}

	// The sentinel row must NOT leak into a real member's per-user list.
	userRows, err := q.ListCerebroFeatureFlags(ctx, cerebrodb.ListCerebroFeatureFlagsParams{
		WorkspaceID: ffTestWorkspaceID,
		UserID:      ffTestUserID,
	})
	if err != nil {
		t.Fatalf("list per-user flags: %v", err)
	}
	if len(userRows) != 0 {
		t.Fatalf("per-user list = %+v, want empty (sentinel row must not leak)", userRows)
	}

	// Owner clears the workspace override.
	if err := q.DeleteCerebroWorkspaceFeatureFlag(ctx, cerebrodb.DeleteCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: ffTestWorkspaceID,
		FlagKey:     flag,
	}); err != nil {
		t.Fatalf("delete workspace flag: %v", err)
	}
	wsRows, err = q.ListCerebroWorkspaceFeatureFlags(ctx, ffTestWorkspaceID)
	if err != nil {
		t.Fatalf("list workspace flags after delete: %v", err)
	}
	if len(wsRows) != 0 {
		t.Fatalf("workspace list after delete = %+v, want empty", wsRows)
	}
}

// TestWorkspaceFlag_RelockUpdates proves a second upsert overwrites enabled +
// locked rather than inserting a duplicate.
func TestWorkspaceFlag_RelockUpdates(t *testing.T) {
	if ffTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	q := cerebrodb.New(ffTestPool)
	const flag = "cerebro_simple_tool_policy"

	// First a soft (unlocked) workspace default.
	if err := q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: ffTestWorkspaceID, FlagKey: flag, Enabled: true, Locked: false,
	}); err != nil {
		t.Fatalf("upsert soft default: %v", err)
	}
	// Then lock it.
	if err := q.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: ffTestWorkspaceID, FlagKey: flag, Enabled: true, Locked: true,
	}); err != nil {
		t.Fatalf("upsert lock: %v", err)
	}

	rows, err := q.ListCerebroWorkspaceFeatureFlags(ctx, ffTestWorkspaceID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found int
	for _, r := range rows {
		if r.FlagKey == flag {
			found++
			if !r.Locked {
				t.Fatalf("flag %q locked = false after relock, want true", flag)
			}
		}
	}
	if found != 1 {
		t.Fatalf("found %d rows for %q, want exactly 1 (no duplicate)", found, flag)
	}

	// Cleanup this flag so the round-trip test's count assertions stay isolated.
	if err := q.DeleteCerebroWorkspaceFeatureFlag(ctx, cerebrodb.DeleteCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: ffTestWorkspaceID, FlagKey: flag,
	}); err != nil {
		t.Fatalf("cleanup delete: %v", err)
	}
}
