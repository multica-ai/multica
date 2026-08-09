package workflows

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestHookMigrationDefinesCompletePersistenceModel(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9139_cerebro_workflow_hooks.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	for _, table := range []string{
		"cerebro_workflow_hook_policy",
		"cerebro_workflow_hook_binding",
		"cerebro_workflow_hook_handler",
		"cerebro_workflow_hook_run",
		"cerebro_workflow_hook_action_run",
	} {
		if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("migration does not create %s", table)
		}
	}
	for _, contract := range []string{"dry_run", "managed", "fail_mode", "policy_version", "idempotency_key"} {
		if !strings.Contains(schema, contract) {
			t.Fatalf("migration is missing %s contract", contract)
		}
	}
}

func TestWorkflowHookContractMigrationBackfillsPublishedAndDraftPolicies(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9176_cerebro_workflow_hook_contracts.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, contract := range []string{
		"contract_rule TEXT NOT NULL",
		"contract_satisfy TEXT NOT NULL",
		"UPDATE cerebro_workflow_hook_policy",
		"UPDATE cerebro_workflow_hook_draft_revision",
	} {
		if !strings.Contains(sql, contract) {
			t.Errorf("contract migration missing %q", contract)
		}
	}
}

func TestHookMigrationsAddConditionModeWithAllDefault(t *testing.T) {
	entries, err := os.ReadDir("../../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	var combined strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		raw, err := os.ReadFile("../../../migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		combined.Write(raw)
	}
	migrations := combined.String()
	for _, contract := range []string{
		"ADD COLUMN IF NOT EXISTS condition_mode TEXT NOT NULL DEFAULT 'all'",
		"CHECK (condition_mode IN ('all', 'any'))",
	} {
		if !strings.Contains(migrations, contract) {
			t.Fatalf("Hook migrations are missing %q", contract)
		}
	}
}

func TestHookLifecycleMigrationSeparatesLiveAndDraftIdentity(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9164_cerebro_workflow_hook_lifecycle.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, contract := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_family",
		"active_policy_id UUID",
		"current_draft_revision_id UUID",
		"CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_draft_series",
		"CREATE UNIQUE INDEX IF NOT EXISTS hook_one_active_draft_series_per_family",
		"WHERE status = 'active'",
		"CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_draft_revision",
		"UNIQUE (workspace_id, draft_series_id, revision)",
		"WHERE mode <> 'dry_run'",
		"REFERENCES cerebro_workflow_hook_policy (workspace_id, family_id, id)",
		"REFERENCES cerebro_workflow_hook_draft_revision (workspace_id, family_id, id)",
	} {
		if !strings.Contains(migration, contract) {
			t.Fatalf("Hook lifecycle migration is missing %q", contract)
		}
	}
}

func TestHookEventJournalMigrationProtectsSevenDayReplayAndExactRevisionEvidence(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9165_cerebro_workflow_hook_event_journal.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_event_journal",
		"schema_version INTEGER NOT NULL DEFAULT 1",
		"event_hash TEXT NOT NULL",
		"expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '7 days')",
		"UNIQUE (workspace_id, event_hash)",
		"CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_test_evidence",
		"draft_revision_id UUID NOT NULL",
		"event_journal_id UUID NOT NULL",
		"UNIQUE (workspace_id, draft_revision_id)",
		"ADD COLUMN IF NOT EXISTS draft_revision_id UUID",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("hook event journal migration missing %q", fragment)
		}
	}
}

func TestGeneratedHookQueriesPersistConditionMode(t *testing.T) {
	raw, err := os.ReadFile("../queries/workflow_hooks.sql")
	if err != nil {
		t.Fatal(err)
	}
	query := string(raw)
	if strings.Count(query, "condition_mode") < 2 {
		t.Fatalf("workflow hook create queries must persist condition_mode")
	}
	if strings.Count(query, "COALESCE(NULLIF(") < 2 {
		t.Fatalf("workflow hook create queries must normalize an empty condition_mode to all")
	}
}

func TestHookFailModeCleanupRetiresOpenPolicies(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9154_cerebro_workflow_hook_fail_mode_cleanup.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration := string(raw)
	for _, contract := range []string{
		"UPDATE cerebro_workflow_hook_policy",
		"WHERE fail_mode = 'open'",
		"ALTER COLUMN fail_mode SET DEFAULT 'warn'",
		"CHECK (fail_mode IN ('closed', 'warn'))",
	} {
		if !strings.Contains(migration, contract) {
			t.Fatalf("cleanup migration is missing %q", contract)
		}
	}
}

func TestHookFailModeCleanupMigratesExistingRowsAndRejectsOpen(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		ALTER TABLE cerebro_workflow_hook_policy
			DROP CONSTRAINT cerebro_workflow_hook_policy_fail_mode_check;
		ALTER TABLE cerebro_workflow_hook_policy
			ALTER COLUMN fail_mode SET DEFAULT 'open';
		ALTER TABLE cerebro_workflow_hook_policy
			ADD CONSTRAINT cerebro_workflow_hook_policy_fail_mode_check
			CHECK (fail_mode IN ('open', 'closed', 'warn'));
	`); err != nil {
		t.Fatal(err)
	}

	var workspaceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Hook migration', 'hook-migration-' || gen_random_uuid(), '', 'HMG')
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	var policyID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO cerebro_workflow_hook_policy
			(workspace_id, name, fail_mode, created_by_id, created_by_type)
		VALUES ($1, 'Legacy open policy', 'open', gen_random_uuid(), 'member')
		RETURNING id
	`, workspaceID).Scan(&policyID); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile("../../../migrations/9154_cerebro_workflow_hook_fail_mode_cleanup.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}

	var failMode, columnDefault string
	if err := tx.QueryRow(ctx, `SELECT fail_mode FROM cerebro_workflow_hook_policy WHERE id = $1`, policyID).Scan(&failMode); err != nil {
		t.Fatal(err)
	}
	if failMode != "warn" {
		t.Fatalf("migrated fail mode = %q, want warn", failMode)
	}
	if err := tx.QueryRow(ctx, `
		SELECT column_default
		FROM information_schema.columns
		WHERE table_name = 'cerebro_workflow_hook_policy' AND column_name = 'fail_mode'
	`).Scan(&columnDefault); err != nil {
		t.Fatal(err)
	}
	if columnDefault != "'warn'::text" {
		t.Fatalf("fail mode default = %q, want warn", columnDefault)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_workflow_hook_policy
			(workspace_id, name, fail_mode, created_by_id, created_by_type)
		VALUES ($1, 'Rejected open policy', 'open', gen_random_uuid(), 'member')
	`, workspaceID); err == nil {
		t.Fatal("open fail mode unexpectedly passed the migrated constraint")
	}
}
