package evals

import (
	"os"
	"strings"
	"testing"
)

func TestEvalScheduleMigrationDefinesTableAndConstraints(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9148_cerebro_eval_schedule.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	// IF NOT EXISTS makes re-applying the up migration idempotent.
	for _, contract := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_eval_schedule",
		"eval_id UUID NOT NULL REFERENCES cerebro_eval(id) ON DELETE CASCADE",
		"schedule_expr TEXT NOT NULL",
		"length(trim(schedule_expr)) > 0",
		"UNIQUE (eval_id)",
		"idx_cerebro_eval_schedule_due",
	} {
		if !strings.Contains(schema, contract) {
			t.Fatalf("migration is missing %q contract", contract)
		}
	}

	down, err := os.ReadFile("../../../migrations/9148_cerebro_eval_schedule.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(down), "DROP TABLE IF EXISTS cerebro_eval_schedule") {
		t.Fatal("down migration does not drop cerebro_eval_schedule")
	}
}
