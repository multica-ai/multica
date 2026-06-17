package migrations

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeProfileProviderMigrationKeepsLegacyConflictArbiter(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "migrations")

	body, err := os.ReadFile(filepath.Join(dir, "121_agent_runtime_provider_partial_unique.up.sql"))
	if err != nil {
		t.Fatalf("read migration 121 up: %v", err)
	}
	sql := strings.ToLower(string(body))
	normalized := strings.Join(strings.Fields(sql), " ")

	if strings.Contains(normalized, "drop constraint agent_runtime_workspace_id_daemon_id_provider_key") {
		t.Fatalf("migration 121 must not drop the legacy provider unique constraint; old runtime registration SQL still needs it during rolling deploys and rollbacks")
	}
	if !strings.Contains(normalized, "create unique index if not exists agent_runtime_workspace_daemon_provider_key") {
		t.Fatalf("migration 121 should still prepare the built-in runtime partial unique index for profile-aware registration")
	}
	if !strings.Contains(normalized, "where profile_id is null") {
		t.Fatalf("migration 121 provider index must be scoped to built-in runtime rows")
	}
}
