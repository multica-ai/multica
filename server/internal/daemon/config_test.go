package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigLocalNotificationDefaultsAndOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", os.Getenv("PATH"))

	stub := t.TempDir()
	codex := filepath.Join(stub, "codex")
	if err := os.WriteFile(codex, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write codex stub: %v", err)
	}
	t.Setenv("MULTICA_CODEX_PATH", codex)

	cfg, err := LoadConfig(Overrides{})
	if err != nil {
		t.Fatalf("LoadConfig default: %v", err)
	}
	if !cfg.LocalNotificationEnabled || !cfg.LocalNotificationOnSuccess || !cfg.LocalNotificationOnFailure {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	t.Setenv("MULTICA_LOCAL_NOTIFICATION_ENABLED", "false")
	t.Setenv("MULTICA_LOCAL_NOTIFICATION_ON_SUCCESS", "0")
	t.Setenv("MULTICA_LOCAL_NOTIFICATION_ON_FAILURE", "no")

	cfg, err = LoadConfig(Overrides{})
	if err != nil {
		t.Fatalf("LoadConfig overrides: %v", err)
	}
	if cfg.LocalNotificationEnabled || cfg.LocalNotificationOnSuccess || cfg.LocalNotificationOnFailure {
		t.Fatalf("unexpected overrides: %+v", cfg)
	}
}
