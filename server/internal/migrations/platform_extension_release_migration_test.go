package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformExtensionReleaseMigrationsPreserveReleaseAndConcurrentIndexContract(t *testing.T) {
	dir := realMigrationsDir(t)

	tableUp, err := os.ReadFile(filepath.Join(dir, "265_platform_extension_release.up.sql"))
	if err != nil {
		t.Fatalf("read release table migration: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE platform_extension_release",
		"id UUID PRIMARY KEY DEFAULT gen_random_uuid()",
		"workspace_id UUID NOT NULL",
		"extension_key TEXT NOT NULL",
		"name TEXT NOT NULL",
		"version TEXT NOT NULL",
		"digest TEXT NOT NULL",
		"manifest JSONB NOT NULL",
		"runtime_id UUID NULL",
		"squad_id UUID NULL",
		"CHECK ((runtime_id IS NULL AND squad_id IS NULL) OR\n        (runtime_id IS NOT NULL AND squad_id IS NOT NULL))",
		"resources JSONB NOT NULL DEFAULT '{}'::jsonb",
		"created_by UUID NOT NULL",
		"created_at TIMESTAMPTZ NOT NULL DEFAULT now()",
	} {
		if !strings.Contains(string(tableUp), want) {
			t.Errorf("release migration is missing %q", want)
		}
	}
	if strings.Contains(strings.ToUpper(string(tableUp)), "REFERENCES") || strings.Contains(strings.ToUpper(string(tableUp)), "CASCADE") {
		t.Errorf("release migration must not add foreign keys or cascade actions: %s", tableUp)
	}

	tableDown, err := os.ReadFile(filepath.Join(dir, "265_platform_extension_release.down.sql"))
	if err != nil {
		t.Fatalf("read release table rollback: %v", err)
	}
	if got := strings.TrimSpace(string(tableDown)); got != "DROP TABLE platform_extension_release;" {
		t.Errorf("release rollback = %q, want only a table drop", got)
	}

	indexUp, err := os.ReadFile(filepath.Join(dir, "266_platform_extension_release_identity_index.up.sql"))
	if err != nil {
		t.Fatalf("read release index migration: %v", err)
	}
	if got := strings.TrimSpace(string(indexUp)); got != "CREATE UNIQUE INDEX CONCURRENTLY idx_platform_extension_release_workspace_key_version\n    ON platform_extension_release (workspace_id, extension_key, version);" {
		t.Errorf("release identity index migration = %q", got)
	}

	indexDown, err := os.ReadFile(filepath.Join(dir, "266_platform_extension_release_identity_index.down.sql"))
	if err != nil {
		t.Fatalf("read release index rollback: %v", err)
	}
	if got := strings.TrimSpace(string(indexDown)); got != "DROP INDEX CONCURRENTLY idx_platform_extension_release_workspace_key_version;" {
		t.Errorf("release identity index rollback = %q", got)
	}
}

func TestRuntimePoolContractMigrationPreflightsTerminalTimestamps(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(realMigrationsDir(t), "267_runtime_pool_contract.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	preflight := strings.Index(sql, "runtime_pool_terminal_timestamp_preflight")
	addCheck := strings.Index(sql, "ADD CONSTRAINT agent_task_queue_terminal_completed_at_check")
	validate := strings.Index(sql, "VALIDATE CONSTRAINT agent_task_queue_terminal_completed_at_check")
	if preflight < 0 || addCheck <= preflight || validate <= addCheck {
		t.Fatalf("preflight/add/validate order is invalid: %d/%d/%d", preflight, addCheck, validate)
	}
	if !strings.Contains(sql[addCheck:validate], "NOT VALID") {
		t.Fatal("terminal timestamp CHECK must be added NOT VALID before validation")
	}
}
