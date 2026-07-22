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
