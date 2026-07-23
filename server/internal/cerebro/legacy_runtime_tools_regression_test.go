package cerebro_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// FIR-3403: retired runtime-tool tables and their UI switch must never return.
func TestLegacyRuntimeToolStoreHasNoReaders(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(here), "..", "..", ".."))
	legacy := "cerebro_runtime" + "_tool"
	legacyGrantUI := legacy + "_grant_ui"
	legacyAgentOverrideRoute := "tool-" + "overrides"
	legacyAgentGrantRoute := "tool-" + "grants"
	// The FIR-3403 schema guard (TestRetiredAccessTablesAbsentFromLiveSchema in
	// server/cmd/migrate/idempotent_test.go) is this scan's companion: it must
	// name the retired tables to assert they never reappear in the live schema.
	// Exempt it exactly as this file exempts itself.
	companionSchemaGuard := filepath.Join("server", "cmd", "migrate", "idempotent_test.go")
	var offenders []string
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot, path)
		if entry.IsDir() && (rel == ".git" || rel == "docs" || rel == "graphify-out" || rel == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() || path == here || rel == companionSchemaGuard {
			return nil
		}
		if strings.HasPrefix(rel, filepath.Join("server", "migrations")+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)+"testdata"+string(filepath.Separator)) {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".sql" && ext != ".ts" && ext != ".tsx" && ext != ".json" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		if (ext == ".go" || ext == ".sql") && strings.Contains(text, legacy) && !strings.Contains(text, legacyGrantUI) {
			offenders = append(offenders, rel)
		}
		if strings.Contains(text, legacyGrantUI) {
			offenders = append(offenders, rel+" (legacy grant UI switch)")
		}
		if strings.Contains(text, legacyAgentOverrideRoute) || strings.Contains(text, legacyAgentGrantRoute) {
			offenders = append(offenders, rel+" (legacy agent authoring alias)")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) != 0 {
		t.Fatalf("legacy runtime-tool store readers returned: %v", offenders)
	}
}
