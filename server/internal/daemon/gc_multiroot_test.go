package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const nonexistentTestPID = 2147483647

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

	// A live profile points at this test process, so its root must be skipped.
	// GC must never delete workdirs another daemon is actively using.
	live := "live-profile"
	liveProfileDir := filepath.Join(home, ".multica", "profiles", live)
	if err := os.MkdirAll(liveProfileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(liveProfileDir, "daemon.pid"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	liveRoot := filepath.Join(home, "multica_workspaces_"+live)
	if err := os.MkdirAll(liveRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// A stale pid file left by a crashed daemon must not strand the profile's
	// workspace root forever.
	stale := "stale-profile"
	staleProfileDir := filepath.Join(home, ".multica", "profiles", stale)
	if err := os.MkdirAll(staleProfileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleProfileDir, "daemon.pid"), []byte(strconv.Itoa(nonexistentTestPID)), 0o644); err != nil {
		t.Fatal(err)
	}
	staleRoot := filepath.Join(home, "multica_workspaces_"+stale)
	if err := os.MkdirAll(staleRoot, 0o755); err != nil {
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
	if !got[staleRoot] {
		t.Errorf("stale-pid root %q missing from roots (should be scanned)", staleRoot)
	}
	neverUsedRoot := filepath.Join(home, "multica_workspaces_"+neverUsed)
	if got[neverUsedRoot] {
		t.Errorf("never-used root %q present in roots (should be skipped: root does not exist)", neverUsedRoot)
	}
}

// TestProfileDaemonActive verifies that ownership detection distinguishes a
// live process from a stale pid file while failing safe on malformed content.
func TestProfileDaemonActive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	d := New(Config{}, slog.Default())
	tests := []struct {
		name     string
		pid      string
		want     bool
		writePID bool
	}{
		{name: "missing", want: false},
		{name: "live", pid: strconv.Itoa(os.Getpid()), want: true, writePID: true},
		{name: "live-whitespace", pid: " \n" + strconv.Itoa(os.Getpid()) + "\t", want: true, writePID: true},
		{name: "stale", pid: strconv.Itoa(nonexistentTestPID), want: false, writePID: true},
		{name: "malformed", pid: "not-a-pid", want: true, writePID: true},
		{name: "non-positive", pid: "0", want: true, writePID: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profileDir := filepath.Join(home, ".multica", "profiles", tt.name)
			if err := os.MkdirAll(profileDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if tt.writePID {
				if err := os.WriteFile(filepath.Join(profileDir, "daemon.pid"), []byte(tt.pid), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := d.profileDaemonActive(tt.name); got != tt.want {
				t.Errorf("profileDaemonActive(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// TestRunGCReclaimsAbandonedProfileRoot exercises the real GC entry point
// against a profile root whose crashed daemon left a stale pid file. An orphan
// older than GCOrphanTTL is removed while a recent task directory is preserved.
func TestRunGCReclaimsAbandonedProfileRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// The current default profile's workspace root is empty.
	currentRoot := filepath.Join(home, "multica_workspaces")
	if err := os.MkdirAll(currentRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// The abandoned profile has the stale pid file a crash would leave behind.
	const abandoned = "old-profile"
	abandonedProfileDir := filepath.Join(home, ".multica", "profiles", abandoned)
	if err := os.MkdirAll(abandonedProfileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(abandonedProfileDir, "daemon.pid"), []byte(strconv.Itoa(nonexistentTestPID)), 0o644); err != nil {
		t.Fatal(err)
	}
	abandonedRoot := filepath.Join(home, "multica_workspaces_"+abandoned)
	wsDir := filepath.Join(abandonedRoot, "ws-abc")

	// The orphan has no .gc_meta.json and is older than the 72-hour TTL.
	orphanTask := filepath.Join(wsDir, "task-orphan")
	if err := os.MkdirAll(orphanTask, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphanTask, "leftover.txt"), []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}
	orphanMtime := time.Now().Add(-73 * time.Hour)
	if err := os.Chtimes(orphanTask, orphanMtime, orphanMtime); err != nil {
		t.Fatal(err)
	}

	// A recent task directory must be preserved.
	activeTask := filepath.Join(wsDir, "task-active")
	if err := os.MkdirAll(activeTask, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeTask, "recent.txt"), []byte("active"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := New(Config{
		Profile:        "",
		WorkspacesRoot: currentRoot,
		GCOrphanTTL:    72 * time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	d.runGC(context.Background())

	if _, err := os.Stat(orphanTask); !os.IsNotExist(err) {
		t.Errorf("abandoned profile's orphan task was not removed: %v", err)
	}
	if _, err := os.Stat(activeTask); err != nil {
		t.Errorf("recent task directory should be preserved: %v", err)
	}
}
