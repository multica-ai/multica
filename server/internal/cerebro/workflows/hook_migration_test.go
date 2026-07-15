package workflows

import (
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
