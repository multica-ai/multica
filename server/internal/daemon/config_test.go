package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func loadConfigForTest(t *testing.T) Config {
	t.Helper()

	binDir := t.TempDir()
	codexPath := filepath.Join(binDir, "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake codex binary: %v", err)
	}
	t.Setenv("MULTICA_CODEX_PATH", codexPath)

	cfg, err := LoadConfig(Overrides{
		DaemonID:       "test-daemon",
		WorkspacesRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	return cfg
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()

	original, hadOriginal := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if hadOriginal {
			_ = os.Setenv(key, original)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestLoadConfigControlPlaneRepoNameDefault(t *testing.T) {
	unsetEnvForTest(t, "MULTICA_CONTEXT_REPO_NAME")

	cfg := loadConfigForTest(t)

	if cfg.ControlPlaneRepoName != DefaultControlPlaneRepoName {
		t.Fatalf("expected %q, got %q", DefaultControlPlaneRepoName, cfg.ControlPlaneRepoName)
	}
}

func TestLoadConfigControlPlaneRepoNameFromEnv(t *testing.T) {
	t.Setenv("MULTICA_CONTEXT_REPO_NAME", "control-plane")

	cfg := loadConfigForTest(t)

	if cfg.ControlPlaneRepoName != "control-plane" {
		t.Fatalf("expected custom repo name, got %q", cfg.ControlPlaneRepoName)
	}
}

func TestLoadConfigControlPlaneRepoNameEmptyFallsBackToDefault(t *testing.T) {
	t.Setenv("MULTICA_CONTEXT_REPO_NAME", "")

	cfg := loadConfigForTest(t)

	if cfg.ControlPlaneRepoName != DefaultControlPlaneRepoName {
		t.Fatalf("expected %q, got %q", DefaultControlPlaneRepoName, cfg.ControlPlaneRepoName)
	}
}
