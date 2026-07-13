package analytics

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAnalyticsProjectionMigrationDefinesRunFactsAndChildren(t *testing.T) {
	up := readAnalyticsMigration(t, "9130_cerebro_analytics_projection.up.sql")

	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_analytics_run",
		"UNIQUE (workspace_id, run_id)",
		"CREATE TABLE IF NOT EXISTS cerebro_analytics_reference",
		"CREATE TABLE IF NOT EXISTS cerebro_analytics_run_skill",
		"CREATE TABLE IF NOT EXISTS cerebro_analytics_run_saving",
		"CREATE TABLE IF NOT EXISTS cerebro_analytics_quality_measurement",
		"CREATE TABLE IF NOT EXISTS cerebro_analytics_visual",
		"ON DELETE CASCADE",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}
}

func TestAnalyticsProjectionMigrationIndexesWorkspaceQueries(t *testing.T) {
	up := readAnalyticsMigration(t, "9130_cerebro_analytics_projection.up.sql")

	for _, fragment := range []string{
		"(workspace_id, started_at DESC)",
		"(workspace_id, source_type, started_at DESC)",
		"(workspace_id, person_id, started_at DESC)",
		"(workspace_id, project_id, started_at DESC)",
		"(workspace_id, provider, model, started_at DESC)",
		"(workspace_id, skill_name, last_used_at DESC)",
		"(workspace_id, measurement_type, category, measured_at DESC)",
		"(workspace_id, reference_kind, reference_id)",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("migration missing index keys %q", fragment)
		}
	}
}

func TestAnalyticsProjectionDownMigrationDropsEveryTable(t *testing.T) {
	down := readAnalyticsMigration(t, "9130_cerebro_analytics_projection.down.sql")

	for _, table := range []string{
		"cerebro_analytics_visual",
		"cerebro_analytics_quality_measurement",
		"cerebro_analytics_run_saving",
		"cerebro_analytics_run_skill",
		"cerebro_analytics_reference",
		"cerebro_analytics_run",
	} {
		if !strings.Contains(down, "DROP TABLE IF EXISTS "+table) {
			t.Errorf("down migration does not drop %s", table)
		}
	}
}

func readAnalyticsMigration(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations", name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}
