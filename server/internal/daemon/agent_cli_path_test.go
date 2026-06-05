package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveAgentCLIDir_HintWinsOverSelfExecutable(t *testing.T) {
	dir := t.TempDir()
	binName := "multica"
	if runtime.GOOS == "windows" {
		binName = "multica.exe"
	}
	hint := filepath.Join(dir, binName)
	if err := os.WriteFile(hint, []byte("stub"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	t.Setenv(MulticaCLIPathEnv, hint)

	got := resolveAgentCLIDir()
	if got != dir {
		t.Fatalf("expected %q, got %q", dir, got)
	}
}

func TestResolveAgentCLIDir_HintMissingFallsBackToSelf(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "definitely-not-here", "multica")
	t.Setenv(MulticaCLIPathEnv, missing)

	got := resolveAgentCLIDir()
	if got == "" {
		t.Fatal("expected fallback to self executable dir, got empty string")
	}
	selfBin, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable failed in this environment: %v", err)
	}
	if got != filepath.Dir(selfBin) {
		t.Fatalf("expected fallback %q, got %q", filepath.Dir(selfBin), got)
	}
}

func TestResolveAgentCLIDir_HintIsDirectoryFallsBack(t *testing.T) {
	// A directory path is not a valid CLI binary location; the resolver
	// should ignore it and fall back to the self-executable dir.
	hintDir := t.TempDir()
	t.Setenv(MulticaCLIPathEnv, hintDir)

	got := resolveAgentCLIDir()
	selfBin, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable failed in this environment: %v", err)
	}
	if got != filepath.Dir(selfBin) {
		t.Fatalf("expected fallback %q, got %q", filepath.Dir(selfBin), got)
	}
}

func TestResolveAgentCLIDir_NoHintUsesSelfExecutable(t *testing.T) {
	t.Setenv(MulticaCLIPathEnv, "")

	got := resolveAgentCLIDir()
	selfBin, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable failed in this environment: %v", err)
	}
	if got != filepath.Dir(selfBin) {
		t.Fatalf("expected %q, got %q", filepath.Dir(selfBin), got)
	}
}
