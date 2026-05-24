package execenv

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareCodexHome_CleansStaleMemories(t *testing.T) {
	dir := t.TempDir()

	codexHome := filepath.Join(dir, "codex-home")
	sharedHome := filepath.Join(dir, "shared-codex")

	// Seed a shared ~/.codex/ so prepareCodexHomeWithOpts doesn't warn.
	os.MkdirAll(sharedHome, 0o755)
	t.Setenv("CODEX_HOME", sharedHome)

	// First call — simulate a prior task that left memories behind.
	if err := os.MkdirAll(filepath.Join(codexHome, "memories"), 0o755); err != nil {
		t.Fatalf("seed memories dir: %v", err)
	}
	staleMemory := filepath.Join(codexHome, "memories", "raw_memories.md")
	if err := os.WriteFile(staleMemory, []byte("cwd: D:\\OldProject\nthread: old-thread-id\n"), 0o644); err != nil {
		t.Fatalf("seed stale memory: %v", err)
	}

	// Verify the stale file exists before prepare.
	if _, err := os.Stat(staleMemory); err != nil {
		t.Fatalf("stale memory should exist before prepare: %v", err)
	}

	// Run prepare — should clean stale memories.
	logger := slog.Default()
	if err := prepareCodexHome(codexHome, logger); err != nil {
		t.Fatalf("prepareCodexHome: %v", err)
	}

	// Stale memories directory should be gone.
	if _, err := os.Stat(filepath.Join(codexHome, "memories")); !os.IsNotExist(err) {
		t.Errorf("expected memories dir to be removed after prepare, err: %v", err)
	}
}

func TestPrepareCodexHome_NoMemoriesDir_NoError(t *testing.T) {
	dir := t.TempDir()

	codexHome := filepath.Join(dir, "codex-home")
	sharedHome := filepath.Join(dir, "shared-codex")

	os.MkdirAll(sharedHome, 0o755)
	t.Setenv("CODEX_HOME", sharedHome)

	// No memories directory exists — should not error.
	logger := slog.Default()
	if err := prepareCodexHome(codexHome, logger); err != nil {
		t.Fatalf("prepareCodexHome: %v", err)
	}
}
