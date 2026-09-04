package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJunieProtocolFamilyMigrationDirections protects the asymmetric contract
// of migration 451: up admits Junie, while down restores the exact capability
// boundary that preceded it. Both directions retain the repository's NOT VALID
// pattern so historical rows are not scanned or rejected during deployment.
func TestJunieProtocolFamilyMigrationDirections(t *testing.T) {
	dir := realMigrationsDir(t)
	up := readMigrationForTest(t, filepath.Join(dir, "451_runtime_profile_add_junie.up.sql"))
	down := readMigrationForTest(t, filepath.Join(dir, "451_runtime_profile_add_junie.down.sql"))

	if !strings.Contains(up, "'junie'") {
		t.Fatal("451 up migration must admit the Junie protocol family")
	}
	if strings.Contains(down, "'junie'") {
		t.Fatal("451 down migration must restore the pre-Junie protocol-family set")
	}
	for name, sql := range map[string]string{"up": up, "down": down} {
		if !strings.Contains(sql, "runtime_profile_protocol_family_check") || !strings.Contains(sql, "NOT VALID") {
			t.Fatalf("451 %s migration must replace the named CHECK using NOT VALID", name)
		}
	}
}

func readMigrationForTest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
