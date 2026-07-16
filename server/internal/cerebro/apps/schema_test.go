package apps

import (
	"os"
	"strings"
	"testing"
)

func TestMiniAppsMigrationDefinesVersionedCatalogAndWorkflowTables(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9135_cerebro_mini_apps.up.sql")
	if err != nil {
		t.Fatalf("read mini apps migration: %v", err)
	}
	sql := string(raw)
	for _, table := range []string{
		"cerebro_app",
		"cerebro_app_version",
		"cerebro_app_change_request",
		"cerebro_app_grant",
		"cerebro_app_kv",
		"cerebro_app_workflow_def",
		"cerebro_app_workflow_run",
		"cerebro_app_audit_log",
	} {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Errorf("migration does not define %s", table)
		}
	}
	if !strings.Contains(sql, "CHECK (length(btrim(release_notes)) > 0)") {
		t.Error("published version snapshots must require release notes")
	}
	if !strings.Contains(sql, "identity_envelope JSONB NOT NULL") {
		t.Error("workflow runs must persist their identity envelope")
	}
}

func TestMiniAppSchemaDefinesImmutableBundlesAndDeployments(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9141_cerebro_app_bundles.up.sql")
	if err != nil {
		t.Fatalf("read app bundles migration: %v", err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_app_version_file",
		"PRIMARY KEY (app_id, version, path)",
		"REFERENCES cerebro_app_version(app_id, version) ON DELETE CASCADE",
		"CREATE TABLE IF NOT EXISTS cerebro_app_deployment",
		"CREATE INDEX IF NOT EXISTS idx_cerebro_app_deployment_status",
		"CHECK (provider IN ('docker', 'sliplane'))",
		"CHECK (status IN ('pending', 'provisioning', 'ready', 'failed', 'paused', 'deleting'))",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("app bundles migration is missing %q", fragment)
		}
	}
}
