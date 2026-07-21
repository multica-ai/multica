package evals

import (
	"os"
	"strings"
	"testing"
)

func TestEvalMigrationDefinesCatalogRunsAndWorkflowBindings(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9140_cerebro_evals.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	for _, table := range []string{"cerebro_eval", "cerebro_eval_run", "cerebro_workflow_eval_binding"} {
		if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("migration does not create %s", table)
		}
	}
	for _, contract := range []string{"UNIQUE (workspace_id, eval_key, version)", "evidence_artifact_id", "UNIQUE (workflow_id, eval_id, phase)", "idx_cerebro_eval_run_gate_lookup"} {
		if !strings.Contains(schema, contract) {
			t.Fatalf("migration is missing %s contract", contract)
		}
	}
}

func TestEvalFollowupMigrationsProtectDelivery(t *testing.T) {
	for file, contracts := range map[string][]string{
		"9150_cerebro_eval_advisory_dedupe.up.sql": {"UNIQUE INDEX", "notification_key"},
		"9151_cerebro_eval_schedule_lease.up.sql": {"claimed_until", "schedule_claim_due_idx"},
	} {
		raw, err := os.ReadFile("../../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, contract := range contracts {
			if !strings.Contains(string(raw), contract) {
				t.Fatalf("%s is missing %s", file, contract)
			}
		}
	}
}
