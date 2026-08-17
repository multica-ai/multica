package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestRunGCReclaimsAbandonedProfileRoot 是端到端验证：真实构造一个
// 废弃 profile 的 workspace root（daemon 已退出、无 daemon.pid），里面留一个
// 无 .gc_meta.json 且 mtime 超过 GCOrphanTTL 的孤儿 task 目录，然后真实调用
// runGC，断言孤儿目录被删除、而 mtime 较新的活跃目录被保留。
func TestRunGCReclaimsAbandonedProfileRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// 当前 profile (default) 的 workspace root（空）
	currentRoot := filepath.Join(home, "multica_workspaces")
	if err := os.MkdirAll(currentRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// 废弃 profile：daemon 已退出，profiles 目录存在但无 daemon.pid
	const abandoned = "old-profile"
	abandonedProfileDir := filepath.Join(home, ".multica", "profiles", abandoned)
	if err := os.MkdirAll(abandonedProfileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	abandonedRoot := filepath.Join(home, "multica_workspaces_"+abandoned)
	wsDir := filepath.Join(abandonedRoot, "ws-abc")

	// 孤儿 task 目录：无 .gc_meta.json，mtime 73 小时前
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

	// 活跃 task 目录：mtime 1 小时前，不应被删除
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
		t.Errorf("废弃 profile 的孤儿 task 目录未被 GC 删除: %v", err)
	}
	if _, err := os.Stat(activeTask); err != nil {
		t.Errorf("活跃 task 目录不应被删除: %v", err)
	}
}
