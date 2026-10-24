package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncCodexNativeConfigCaseOnlyRoleRename(t *testing.T) {
	shared, task := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(shared, "agents/Architect.toml"), "original")
	writeFile(t, filepath.Join(task, "agents/local.toml"), "task-owned")
	if err := syncCodexNativeConfig(task, shared); err != nil {
		t.Fatal(err)
	}

	// Remove then create the host role to preserve the new spelling even on
	// case-insensitive volumes. The destination still has the previous spelling.
	if err := os.Remove(filepath.Join(shared, "agents/Architect.toml")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(shared, "agents/architect.toml"), "renamed")
	for range 2 {
		if err := syncCodexNativeConfig(task, shared); err != nil {
			t.Fatal(err)
		}
		assertNativeConfigContent(t, filepath.Join(task, "agents/architect.toml"), "renamed")
		assertNativeConfigContent(t, filepath.Join(task, "agents/local.toml"), "task-owned")
		assertNativeConfigContent(t, filepath.Join(task, codexNativeAgentsManifest), `["architect.toml"]`)
		entries, err := os.ReadDir(filepath.Join(task, "agents"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 || entries[0].Name() != "architect.toml" || entries[1].Name() != "local.toml" {
			t.Fatalf("native agents after case-only rename = %v, want architect.toml and local.toml", entries)
		}
	}
}
