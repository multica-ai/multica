package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

func TestResolveAgentExecutableFallsBackWhenEnvUnset(t *testing.T) {
	dir := t.TempDir()
	fallback := filepath.Join(dir, "codex-desktop")
	if err := os.WriteFile(fallback, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fallback executable: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MULTICA_CODEX_PATH", "")

	got, ok := resolveAgentExecutable("MULTICA_CODEX_PATH", "codex", []string{fallback})
	if !ok {
		t.Fatal("expected fallback executable to resolve")
	}
	if got != fallback {
		t.Fatalf("expected %q, got %q", fallback, got)
	}
}

func TestResolveAgentExecutableExplicitEnvDoesNotFallback(t *testing.T) {
	dir := t.TempDir()
	fallback := filepath.Join(dir, "codex-desktop")
	if err := os.WriteFile(fallback, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fallback executable: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MULTICA_CODEX_PATH", filepath.Join(dir, "missing-codex"))

	if got, ok := resolveAgentExecutable("MULTICA_CODEX_PATH", "codex", []string{fallback}); ok {
		t.Fatalf("expected explicit missing executable to disable fallback, got %q", got)
	}
}

func TestCodexDesktopFallbackPathsDarwinOnly(t *testing.T) {
	got := codexDesktopFallbackPaths()
	if runtime.GOOS == "darwin" {
		if len(got) != 1 || got[0] != "/Applications/Codex.app/Contents/Resources/codex" {
			t.Fatalf("unexpected darwin fallback paths: %#v", got)
		}
		return
	}
	if len(got) != 0 {
		t.Fatalf("expected no non-darwin fallback paths, got %#v", got)
	}
}

func TestBoolFromEnv(t *testing.T) {
	t.Setenv("MULTICA_CODEX_APP_VISIBLE", "yes")
	got, err := boolFromEnv("MULTICA_CODEX_APP_VISIBLE", false)
	if err != nil {
		t.Fatalf("boolFromEnv: %v", err)
	}
	if !got {
		t.Fatal("expected yes to parse as true")
	}

	t.Setenv("MULTICA_CODEX_APP_VISIBLE", "off")
	got, err = boolFromEnv("MULTICA_CODEX_APP_VISIBLE", true)
	if err != nil {
		t.Fatalf("boolFromEnv: %v", err)
	}
	if got {
		t.Fatal("expected off to parse as false")
	}

	t.Setenv("MULTICA_CODEX_APP_VISIBLE", "sometimes")
	if _, err := boolFromEnv("MULTICA_CODEX_APP_VISIBLE", false); err == nil {
		t.Fatal("expected invalid bool to fail")
	}
}
