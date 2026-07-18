package evals

import (
	"os"
	"strings"
	"testing"
)

func TestEvalAssetsMigrationAddsVersionFamily(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9146_cerebro_eval_assets.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	for _, contract := range []string{
		"ADD COLUMN IF NOT EXISTS eval_family_id UUID",
		"idx_cerebro_eval_family",
		"SET DEFAULT gen_random_uuid()",
	} {
		if !strings.Contains(schema, contract) {
			t.Fatalf("assets migration is missing %q", contract)
		}
	}

	down, err := os.ReadFile("../../../migrations/9146_cerebro_eval_assets.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(down), "DROP COLUMN IF EXISTS eval_family_id") {
		t.Fatal("down migration must drop eval_family_id")
	}
}
