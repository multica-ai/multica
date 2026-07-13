package operatingsystem

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationDefinesOperatingSystemTables(t *testing.T) {
	up := readMigration(t, "9135_cerebro_operating_system.up.sql")

	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_operating_system_settings",
		"CREATE TABLE IF NOT EXISTS cerebro_strategy_item",
		"CREATE TABLE IF NOT EXISTS cerebro_rock",
		"CREATE TABLE IF NOT EXISTS cerebro_object_connection",
		"UNIQUE (workspace_id, source_type, source_id, target_type, target_id, relationship_type)",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}
}

func TestMigrationPreservesCanonicalConstraints(t *testing.T) {
	up := readMigration(t, "9135_cerebro_operating_system.up.sql")

	for _, fragment := range []string{
		"kind IN ('core_value', 'core_focus', 'horizon_goal')",
		"horizon_unit IN ('day', 'week', 'month', 'year')",
		"confidence BETWEEN 0 AND 100",
		"reported_health IN ('on_track', 'at_risk', 'off_track', 'unset')",
		"period_end >= period_start",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("migration missing constraint %q", fragment)
		}
	}
}

func TestDownMigrationDropsTablesInReverseOrder(t *testing.T) {
	down := readMigration(t, "9135_cerebro_operating_system.down.sql")
	want := []string{
		"DROP TABLE IF EXISTS cerebro_object_connection;",
		"DROP TABLE IF EXISTS cerebro_rock;",
		"DROP TABLE IF EXISTS cerebro_strategy_item;",
		"DROP TABLE IF EXISTS cerebro_operating_system_settings;",
	}
	position := -1
	for _, fragment := range want {
		next := strings.Index(down, fragment)
		if next <= position {
			t.Fatalf("down migration must drop in reverse order; missing or misplaced %q", fragment)
		}
		position = next
	}
}

func readMigration(t *testing.T, name string) string {
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
