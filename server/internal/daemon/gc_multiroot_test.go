package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestGCWorkspaceRootsMultiProfile verifies that a single GC pass walks the
// current profile's root plus roots abandoned by a previous profile whose
// daemon is no longer running, while skipping roots owned by a live daemon.
func TestGCWorkspaceRootsMultiProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_WORKSPACES_ROOT", "")

	// The current (default) profile's root comes straight from the config, not
	// from $HOME resolution.
	currentRoot := t.TempDir()

	// An abandoned profile: its daemon exited (no daemon.pid), and its
	// workspace root exists. It must be included so GC can reclaim it.
	abandoned := "old-profile"
	abandonedProfileDir := filepath.Join(home, ".multica", "profiles", abandoned)
	if err := os.MkdirAll(abandonedProfileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	abandonedRoot := filepath.Join(home, "multica_workspaces_"+abandoned)
	if err := os.MkdirAll(abandonedRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// A live profile: daemon.pid is present, so its root must be skipped. GC
	// must never delete workdirs another daemon is actively using.
	live := "live-profile"
	liveProfileDir := filepath.Join(home, ".multica", "profiles", live)
	if err := os.MkdirAll(liveProfileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(liveProfileDir, "daemon.pid"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	liveRoot := filepath.Join(home, "multica_workspaces_"+live)
	if err := os.MkdirAll(liveRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// A profile whose state directory exists but whose workspace root was never
	// created. There is nothing to scan, so it must be skipped.
	neverUsed := "never-used"
	neverUsedProfileDir := filepath.Join(home, ".multica", "profiles", neverUsed)
	if err := os.MkdirAll(neverUsedProfileDir, 0o755); err != nil {
		t.Fatal(err)
	}

	d := New(Config{WorkspacesRoot: currentRoot}, slog.Default())
	roots := d.gcWorkspaceRoots()

	got := make(map[string]bool, len(roots))
	for _, gr := range roots {
		got[gr.root] = true
	}

	if !got[currentRoot] {
		t.Errorf("current root %q missing from roots", currentRoot)
	}
	if !got[abandonedRoot] {
		t.Errorf("abandoned root %q missing from roots (should be scanned)", abandonedRoot)
	}
	if got[liveRoot] {
		t.Errorf("live root %q present in roots (should be skipped: daemon running)", liveRoot)
	}
	neverUsedRoot := filepath.Join(home, "multica_workspaces_"+neverUsed)
	if got[neverUsedRoot] {
		t.Errorf("never-used root %q present in roots (should be skipped: root does not exist)", neverUsedRoot)
	}
}

// TestProfileDaemonActive verifies ownership detection is driven by the
// presence of a profile's daemon.pid file.
func TestProfileDaemonActive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	live := "live"
	liveDir := filepath.Join(home, ".multica", "profiles", live)
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(liveDir, "daemon.pid"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	dead := "dead"
	deadDir := filepath.Join(home, ".multica", "profiles", dead)
	if err := os.MkdirAll(deadDir, 0o755); err != nil {
		t.Fatal(err)
	}

	d := New(Config{}, slog.Default())

	if !d.profileDaemonActive(live) {
		t.Errorf("profile %q with daemon.pid should be active", live)
	}
	if d.profileDaemonActive(dead) {
		t.Errorf("profile %q without daemon.pid should be inactive", dead)
	}
}
