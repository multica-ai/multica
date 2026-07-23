package evals

import (
	"os"
	"strings"
	"testing"
)

func TestBlockingGateCapabilityMigration(t *testing.T) {
	up, err := os.ReadFile("../../../migrations/9149_cerebro_set_blocking_gate_capability.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{"cerebro_group_capability_known", "set_blocking_gate", "create_memory"} {
		if !strings.Contains(string(up), contract) {
			t.Fatalf("up migration missing %q", contract)
		}
	}
	down, err := os.ReadFile("../../../migrations/9149_cerebro_set_blocking_gate_capability.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(down), "DELETE FROM cerebro_group_capability WHERE capability = 'set_blocking_gate'") {
		t.Fatal("down migration must remove blocking-gate grants before restoring the constraint")
	}
}
