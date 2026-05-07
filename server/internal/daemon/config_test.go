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
	"reflect"
	"testing"
)

func TestPatternsFromEnv_DefaultsWhenUnset(t *testing.T) {
	t.Setenv("MULTICA_GC_ARTIFACT_PATTERNS", "")
	defaults := []string{"node_modules", ".next", ".turbo"}
	got := patternsFromEnv("MULTICA_GC_ARTIFACT_PATTERNS", defaults)
	if !reflect.DeepEqual(got, defaults) {
		t.Fatalf("expected defaults %v, got %v", defaults, got)
	}
	// Ensure callers get a copy, not a shared backing array.
	got[0] = "mutated"
	if defaults[0] == "mutated" {
		t.Fatal("patternsFromEnv must not return a slice aliased with defaults")
	}
}

func TestPatternsFromEnv_DropsSeparatorBearingEntries(t *testing.T) {
	t.Setenv("MULTICA_GC_ARTIFACT_PATTERNS", "node_modules, .next ,foo/bar, ../etc, ,target")
	got := patternsFromEnv("MULTICA_GC_ARTIFACT_PATTERNS", nil)
	want := []string{"node_modules", ".next", "target"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
