package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newSelfHealTestDaemon builds a Daemon with just the state resolveAgentEntry
// touches (logger + the two maps it reads/writes), so these tests don't need a
// fake HTTP server. healGroup / the mutexes are usable at their zero value.
func newSelfHealTestDaemon() *Daemon {
	return &Daemon{
		logger:        slog.Default(),
		resolvedPaths: make(map[string]healedAgent),
		agentVersions: make(map[string]string),
	}
}

// stubDetectVersionFromPath makes detectAgentVersion report the version encoded
// in an installVersionedCodex path (…/codex/<ver>/bin/codex), so a heal to a
// v2 directory "detects" v2. checkAgentMinVersion is intentionally left as the
// real agent.CheckMinVersion so the min-version gate is exercised for real.
func stubDetectVersionFromPath(t *testing.T) {
	t.Helper()
	orig := detectAgentVersion
	detectAgentVersion = func(_ context.Context, path string) (string, error) {
		return filepath.Base(filepath.Dir(filepath.Dir(path))), nil
	}
	t.Cleanup(func() { detectAgentVersion = orig })
}

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
// version. resolveAgentEntry must re-resolve the pinned path AND return the
// matching version, so the caller keys downstream policy off the binary that
// will actually run.
func TestResolveAgentEntry_SelfHealsAfterInPlaceUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/exec-bit layout is POSIX-specific")
	}
	stubDetectVersionFromPath(t)

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

	d := newSelfHealTestDaemon()
	d.setAgentVersion("codex", "0.144.1") // the version detected for v1 at startup
	entry := AgentEntry{Path: v1, Command: "codex"}
	ctx := context.Background()

	// While the pinned binary is present it must be returned unchanged, paired
	// with its registration-detected version — the anti-PATH-redirect case.
	if got, ver := d.resolveAgentEntry(ctx, "codex", entry); got.Path != v1 || ver != "0.144.1" {
		t.Fatalf("live pinned path/version rewritten: got (%q, %q), want (%q, %q)", got.Path, ver, v1, "0.144.1")
	}

	// In-place upgrade: drop the v1 tree and repoint the stable symlink at v2.
	if err := os.RemoveAll(filepath.Join(root, "Caskroom", "codex", "0.144.1")); err != nil {
		t.Fatalf("remove v1 tree: %v", err)
	}
	if agentExecutablePresent(v1) {
		t.Fatalf("v1 path still present after removing its tree: %q", v1)
	}
	v2 := installVersionedCodex(t, root, "0.144.3", stableBin)

	got, ver := d.resolveAgentEntry(ctx, "codex", entry)
	if got.Path != v2 {
		t.Fatalf("self-heal resolved %q, want re-resolved v2 %q", got.Path, v2)
	}
	if !agentExecutablePresent(got.Path) {
		t.Fatalf("self-healed path is not runnable: %q", got.Path)
	}
	// Must-fix (MUL-4486 review): the version returned is paired with the healed
	// path, and the shared version cache moved with it too.
	if ver != "0.144.3" {
		t.Fatalf("returned version not paired with healed path: got %q, want %q", ver, "0.144.3")
	}
	if v := d.agentVersion("codex"); v != "0.144.3" {
		t.Fatalf("version cache not updated in lockstep: got %q, want %q", v, "0.144.3")
	}

	// A subsequent call with the same stale entry must reuse the remembered
	// {path, version} without re-resolving. Prove it by breaking PATH: if it
	// re-resolved now it would miss, but the cached heal still resolves.
	t.Setenv("PATH", filepath.Join(root, "empty"))
	if got, ver := d.resolveAgentEntry(ctx, "codex", entry); got.Path != v2 || ver != "0.144.3" {
		t.Fatalf("cached self-heal not reused: got (%q, %q), want (%q, %q)", got.Path, ver, v2, "0.144.3")
	}
}

// TestResolveAgentEntry_RejectsBelowMinVersionAfterUpgrade covers the review's
// concern: if re-resolution lands on a build below the minimum supported
// version, it must NOT be adopted or launched, and the cached version must not
// be corrupted downward.
func TestResolveAgentEntry_RejectsBelowMinVersionAfterUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/exec-bit layout is POSIX-specific")
	}
	stubDetectVersionFromPath(t) // checkAgentMinVersion stays real: codex min is 0.100.0

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	t.Setenv("PATH", stableBin)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))

	v1 := installVersionedCodex(t, root, "0.144.1", stableBin)

	d := newSelfHealTestDaemon()
	d.setAgentVersion("codex", "0.144.1")
	entry := AgentEntry{Path: v1, Command: "codex"}
	ctx := context.Background()

	// "Upgrade" actually repoints at a below-minimum build (< 0.100.0).
	if err := os.RemoveAll(filepath.Join(root, "Caskroom", "codex", "0.144.1")); err != nil {
		t.Fatalf("remove v1 tree: %v", err)
	}
	installVersionedCodex(t, root, "0.9.0", stableBin)

	got, ver := d.resolveAgentEntry(ctx, "codex", entry)
	if got.Path != v1 {
		t.Fatalf("below-min build was adopted: got %q, want stale pinned %q", got.Path, v1)
	}
	// The returned version stays the (stale) base — never the below-min value.
	if ver != "0.144.1" {
		t.Fatalf("returned version corrupted to below-min: got %q, want %q", ver, "0.144.1")
	}
	d.resolvedPathsMu.RLock()
	_, cached := d.resolvedPaths["codex"]
	d.resolvedPathsMu.RUnlock()
	if cached {
		t.Fatalf("below-min build was cached as a healed path")
	}
	if v := d.agentVersion("codex"); v != "0.144.1" {
		t.Fatalf("cached version was corrupted to a below-min value: got %q, want %q", v, "0.144.1")
	}
}

// TestResolveAgentEntry_ReturnsVersionPairedWithCachedPath is the regression
// for the second-round review race: a task that hits the already-healed path
// (via the resolvedPaths fast path, without entering singleflight) must read
// the version that goes WITH that path, not whatever the separately-mutable
// agentVersions cache happens to hold. We heal to v2, then deliberately skew
// agentVersions to a bogus value to stand in for the window where the two
// caches disagree, and assert the reused resolution still returns v2's version.
func TestResolveAgentEntry_ReturnsVersionPairedWithCachedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/exec-bit layout is POSIX-specific")
	}
	stubDetectVersionFromPath(t)

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	t.Setenv("PATH", stableBin)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))

	v1 := installVersionedCodex(t, root, "0.144.1", stableBin)
	d := newSelfHealTestDaemon()
	d.setAgentVersion("codex", "0.144.1")
	entry := AgentEntry{Path: v1, Command: "codex"}
	ctx := context.Background()

	if err := os.RemoveAll(filepath.Join(root, "Caskroom", "codex", "0.144.1")); err != nil {
		t.Fatalf("remove v1 tree: %v", err)
	}
	v2 := installVersionedCodex(t, root, "0.144.3", stableBin)

	// First call heals and caches {v2, 0.144.3}.
	if got, ver := d.resolveAgentEntry(ctx, "codex", entry); got.Path != v2 || ver != "0.144.3" {
		t.Fatalf("initial heal wrong: got (%q, %q)", got.Path, ver)
	}
	// Simulate the two caches transiently disagreeing (the exact window the race
	// was about): the shared version cache holds a value that does NOT match the
	// cached path. The paired return must still win.
	d.setAgentVersion("codex", "0.0.1-stale")

	got, ver := d.resolveAgentEntry(ctx, "codex", entry)
	if got.Path != v2 {
		t.Fatalf("cached path not reused: got %q, want %q", got.Path, v2)
	}
	if ver != "0.144.3" {
		t.Fatalf("returned version came from the skewed shared cache, not the cached path pairing: got %q, want %q", ver, "0.144.3")
	}
}

// TestResolveAgentEntry_HealedPathWinsOverReappearingPinnedPath covers the
// follow-up edge from the third-round review: after a self-heal to v2, if the
// originally pinned v1 path reappears on disk (a downgrade / reinstall that
// recreates the old versioned directory) while the healed v2 binary is still
// present, resolveAgentEntry must keep returning the healed {v2, its version}
// pair. Returning the reappeared v1 path here would pair the old binary with
// the healed v2 version — the mismatched {old path, new version} the review
// flagged.
func TestResolveAgentEntry_HealedPathWinsOverReappearingPinnedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink/exec-bit layout is POSIX-specific")
	}
	stubDetectVersionFromPath(t)

	root := t.TempDir()
	stableBin := filepath.Join(root, "bin")
	t.Setenv("PATH", stableBin)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "fish"))

	v1 := installVersionedCodex(t, root, "0.144.1", stableBin)
	d := newSelfHealTestDaemon()
	d.setAgentVersion("codex", "0.144.1")
	entry := AgentEntry{Path: v1, Command: "codex"}
	ctx := context.Background()

	// In-place upgrade: drop v1, repoint the stable symlink at v2, and heal.
	if err := os.RemoveAll(filepath.Join(root, "Caskroom", "codex", "0.144.1")); err != nil {
		t.Fatalf("remove v1 tree: %v", err)
	}
	v2 := installVersionedCodex(t, root, "0.144.3", stableBin)
	if got, ver := d.resolveAgentEntry(ctx, "codex", entry); got.Path != v2 || ver != "0.144.3" {
		t.Fatalf("initial heal wrong: got (%q, %q), want (%q, %q)", got.Path, ver, v2, "0.144.3")
	}

	// The originally pinned v1 path reappears (a reinstall recreating the old
	// versioned dir) while the healed v2 binary is still present on disk.
	writeExecStub(t, v1)
	if !agentExecutablePresent(v1) {
		t.Fatalf("v1 path should be runnable again after reinstall: %q", v1)
	}

	got, ver := d.resolveAgentEntry(ctx, "codex", entry)
	if got.Path != v2 {
		t.Fatalf("reappearing pinned path hijacked the launch: got %q, want healed %q", got.Path, v2)
	}
	if ver != "0.144.3" {
		t.Fatalf("returned version not paired with healed path: got %q, want %q", ver, "0.144.3")
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

	d := newSelfHealTestDaemon()
	entry := AgentEntry{Path: pinned, Command: "codex"}
	got, _ := d.resolveAgentEntry(context.Background(), "codex", entry)
	if got.Path != pinned {
		t.Fatalf("expected entry unchanged when binary is gone, got %q want %q", got.Path, pinned)
	}
}

// TestResolveAgentEntry_NoCommandNoHeal verifies that a synthesized entry with
// no recorded Command (e.g. a custom runtime profile carrying an absolute path)
// is never re-resolved: the entry is returned as-is even when its path is gone.
func TestResolveAgentEntry_NoCommandNoHeal(t *testing.T) {
	d := newSelfHealTestDaemon()
	entry := AgentEntry{Path: filepath.Join(t.TempDir(), "does-not-exist"), Command: ""}
	if got, _ := d.resolveAgentEntry(context.Background(), "codex", entry); got.Path != entry.Path {
		t.Fatalf("entry with empty Command was rewritten: got %q, want %q", got.Path, entry.Path)
	}
}
