package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestRuntimePoolTriggerUserMigrationDoesNotInferHistoricalActors(t *testing.T) {
	up, err := os.ReadFile("../../migrations/281_runtime_pool_trigger_user.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/281_runtime_pool_trigger_user.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(up), "ADD COLUMN runtime_trigger_user_id UUID") {
		t.Fatal("up migration must add nullable runtime_trigger_user_id")
	}
	if strings.Contains(string(up), "originator_user_id") || strings.Contains(string(up), "runtime_requester_user_id") {
		t.Fatal("migration must not infer a trigger user from attribution or authorization fields")
	}
	if !strings.Contains(string(down), "DROP COLUMN runtime_trigger_user_id") {
		t.Fatal("down migration must remove runtime_trigger_user_id")
	}
}
