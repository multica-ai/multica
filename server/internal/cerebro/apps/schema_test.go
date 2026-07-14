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
