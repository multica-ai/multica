package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeExecStub writes a runnable no-op executable at path, creating parents.
func writeExecStub(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for stub %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", path, err)
	}
}

// installVersionedCodex lays out a version-manager style install under root:
// a concrete versioned binary plus a stable-name symlink pointing at it (the
// shape Homebrew Cask / nvm / fnm produce). It returns the canonical path the
// daemon would pin for the stable name, and repoints the symlink atomically on
// later calls to simulate an in-place upgrade.
func installVersionedCodex(t *testing.T, root, version, stableBin string) string {
	t.Helper()
	versioned := filepath.Join(root, "Caskroom", "codex", version, "bin", "codex")
	writeExecStub(t, versioned)
	link := filepath.Join(stableBin, "codex")
	_ = os.Remove(link) // repoint on upgrade
	if err := os.MkdirAll(stableBin, 0o755); err != nil {
		t.Fatalf("mkdir stable bin: %v", err)
	}
	if err := os.Symlink(versioned, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, versioned, err)
	}
	return canonicalExecutablePath(link)
}

// TestResolveAgentEntry_SelfHealsAfterInPlaceUpgrade reproduces MUL-4486: a
// version manager upgrades codex in place, deleting the versioned directory the
// daemon pinned at startup and repointing the stable command name at the new
// version. resolveAgentEntry must re-resolve the pinned path instead of leaving
// the daemon stuck on a path that no longer exists.
func TestResolveAgentEntry_SelfHealsAfterInPlaceUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/exec-bit layout is POSIX-specific")
	}

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin") // the stable "/opt/homebrew/bin" analogue
	t.Setenv("PATH", stableBin)
	// Pin resolution to the daemon's own PATH: an unsupported shell disables the
	// login-shell fallback so the test can't accidentally resolve a real codex
	// installed on the host running it.
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))

	v1 := installVersionedCodex(t, root, "0.144.1", stableBin)
	if !strings.Contains(v1, "0.144.1") {
		t.Fatalf("pinned path %q does not point into the v1 versioned dir", v1)
	}

	d := newTestDaemon(t)
	entry := AgentEntry{Path: v1, Command: "codex"}

	// While the pinned binary is present it must be returned unchanged — the
	// anti-PATH-redirect guarantee for the normal case.
	if got := d.resolveAgentEntry("codex", entry); got.Path != v1 {
		t.Fatalf("live pinned path was rewritten: got %q, want %q", got.Path, v1)
	}

	// In-place upgrade: drop the v1 tree and repoint the stable symlink at v2.
	if err := os.RemoveAll(filepath.Join(root, "Caskroom", "codex", "0.144.1")); err != nil {
		t.Fatalf("remove v1 tree: %v", err)
	}
	if agentExecutablePresent(v1) {
		t.Fatalf("v1 path still present after removing its tree: %q", v1)
	}
	v2 := installVersionedCodex(t, root, "0.144.3", stableBin)

	got := d.resolveAgentEntry("codex", entry)
	if got.Path != v2 {
		t.Fatalf("self-heal resolved %q, want re-resolved v2 %q", got.Path, v2)
	}
	if !agentExecutablePresent(got.Path) {
		t.Fatalf("self-healed path is not runnable: %q", got.Path)
	}

	// A subsequent call with the same stale entry must reuse the remembered
	// heal without re-resolving from scratch. Prove it by breaking PATH: if it
	// re-resolved now it would miss, but the cached healed path still resolves.
	t.Setenv("PATH", filepath.Join(root, "empty"))
	if got := d.resolveAgentEntry("codex", entry); got.Path != v2 {
		t.Fatalf("cached self-heal not reused: got %q, want %q", got.Path, v2)
	}
}

// TestResolveAgentEntry_UninstalledLeavesEntryUnchanged verifies that when the
// binary is genuinely gone (not just moved by an upgrade), resolveAgentEntry
// returns the entry untouched so the downstream "executable not found" error
// still surfaces rather than being silently swallowed.
func TestResolveAgentEntry_UninstalledLeavesEntryUnchanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-specific layout")
	}

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	t.Setenv("PATH", stableBin)
	// Disable the login-shell fallback so an actual codex on the host running
	// this test can't stand in for the "uninstalled" binary.
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))
	pinned := installVersionedCodex(t, root, "0.144.1", stableBin)

	// Uninstall entirely: remove the versioned tree and the stable symlink.
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove install root: %v", err)
	}

	d := newTestDaemon(t)
	entry := AgentEntry{Path: pinned, Command: "codex"}
	got := d.resolveAgentEntry("codex", entry)
	if got.Path != pinned {
		t.Fatalf("expected entry unchanged when binary is gone, got %q want %q", got.Path, pinned)
	}
}

// TestResolveAgentEntry_NoCommandNoHeal verifies that a synthesized entry with
// no recorded Command (e.g. a custom runtime profile carrying an absolute path)
// is never re-resolved: the entry is returned as-is even when its path is gone.
func TestResolveAgentEntry_NoCommandNoHeal(t *testing.T) {
	d := newTestDaemon(t)
	entry := AgentEntry{Path: filepath.Join(t.TempDir(), "does-not-exist"), Command: ""}
	if got := d.resolveAgentEntry("codex", entry); got.Path != entry.Path {
		t.Fatalf("entry with empty Command was rewritten: got %q, want %q", got.Path, entry.Path)
	}
}
