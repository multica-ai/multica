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

func TestAlignmentMigrationDefinesFirstClassRocks(t *testing.T) {
	up := readMigration(t, "9143_cerebro_operating_system_alignment.up.sql")

	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_operating_period",
		"ADD COLUMN IF NOT EXISTS id UUID",
		"ADD COLUMN IF NOT EXISTS title TEXT",
		"ADD COLUMN IF NOT EXISTS owner_type TEXT",
		"ADD COLUMN IF NOT EXISTS period_id UUID",
		"CREATE TABLE IF NOT EXISTS cerebro_rock_check_in",
		"CREATE TABLE IF NOT EXISTS cerebro_strategy_item_history",
		"ADD COLUMN IF NOT EXISTS horizon_label TEXT",
		"'10-Year Target'",
		"INSERT INTO cerebro_object_connection",
		"SET target_id = r.id",
		"'system'",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("alignment migration missing %q", fragment)
		}
	}
}

func TestAlignmentMigrationDropsLegacyPrimaryKeyBeforeMakingProjectOptional(t *testing.T) {
	up := readMigration(t, "9143_cerebro_operating_system_alignment.up.sql")

	dropPrimaryKey := strings.Index(up, "DROP CONSTRAINT cerebro_rock_pkey")
	dropProjectNotNull := strings.Index(up, "ALTER COLUMN project_id DROP NOT NULL")
	if dropPrimaryKey == -1 || dropProjectNotNull == -1 {
		t.Fatal("alignment migration must replace the legacy project primary key")
	}
	if dropPrimaryKey > dropProjectNotNull {
		t.Fatal("alignment migration must drop the project primary key before making project_id nullable")
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

func TestConfigMigrationDefinesConfigurationFoundation(t *testing.T) {
	up := readMigration(t, "9144_cerebro_operating_system_config.up.sql")

	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_os_element_setting",
		"PRIMARY KEY (workspace_id, element_key)",
		"CREATE TABLE IF NOT EXISTS cerebro_goal_type",
		"UNIQUE (workspace_id, name)",
		"btrim(name) <> ''",
		"ADD COLUMN IF NOT EXISTS goal_type_id UUID REFERENCES cerebro_goal_type(id) ON DELETE SET NULL",
		"ADD COLUMN IF NOT EXISTS unit TEXT",
		"unit IN ('month', 'quarter', 'custom')",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("config migration missing %q", fragment)
		}
	}
}

func TestConfigMigrationFreezesLegacyTerminologyForExistingWorkspaces(t *testing.T) {
	up := readMigration(t, "9144_cerebro_operating_system_config.up.sql")

	for _, fragment := range []string{
		"INSERT INTO cerebro_operating_system_settings (workspace_id, terminology)",
		`{"strategy":"Strategy","rock":"Rock","rocks":"Rocks"}`,
		"ON CONFLICT (workspace_id) DO NOTHING",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("config migration missing terminology freeze %q", fragment)
		}
	}
}

func TestConfigDownMigrationRemovesConfigurationFoundation(t *testing.T) {
	down := readMigration(t, "9144_cerebro_operating_system_config.down.sql")
	want := []string{
		"DROP COLUMN IF EXISTS goal_type_id",
		"DROP TABLE IF EXISTS cerebro_goal_type;",
		"DROP TABLE IF EXISTS cerebro_os_element_setting;",
		"DROP COLUMN IF EXISTS unit",
	}
	position := -1
	for _, fragment := range want {
		next := strings.Index(down, fragment)
		if next <= position {
			t.Fatalf("config down migration must remove dependents first; missing or misplaced %q", fragment)
		}
		position = next
	}
}
