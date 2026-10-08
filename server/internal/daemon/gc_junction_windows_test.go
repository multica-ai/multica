//go:build windows

package daemon

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The per-task codex-home links shared user content into the task directory —
// skills (MUL-6000), the Codex session store, the plugin cache. On Windows
// those links are directory junctions whenever os.Symlink is denied, which is
// the default without Developer Mode.
//
// A junction is the one link shape a ModeSymlink check misses: since Go 1.23
// os.Lstat reports it as ModeDir|ModeIrregular with no ModeSymlink bit, and
// its DirEntry still answers IsDir() == true, so filepath.WalkDir walks
// straight into the target. Only a real Windows filesystem can prove the guard
// holds, which is why this lives in a windows-tagged file with its own CI step.

// createJunction links dst -> src as a directory junction. mklink /J is used
// directly (rather than createDirLink's symlink-first path) so the test always
// exercises the junction shape, even on a runner where symlinks are allowed.
func createJunction(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", dst, src).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %s: %v", dst, src, out, err)
	}
	fi, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("lstat junction: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected a junction, got a symlink (mode %v) — the test would not cover the junction path", fi.Mode())
	}
}

func TestCleanTaskArtifacts_DoesNotFollowDirectoryJunction(t *testing.T) {
	d := newGCTestDaemon(t, http.NewServeMux())

	taskDir := t.TempDir()
	userSkill := t.TempDir()
	keep := filepath.Join(userSkill, "node_modules", "pkg", "index.js")
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mirrors the real layout: codex-home/skills/<name> junctioned to the
	// user's ~/.codex/skills/<name>.
	createJunction(t, userSkill, filepath.Join(taskDir, "codex-home", "skills", "alpha"))

	removed, _, _ := d.cleanTaskArtifacts(taskDir, []string{"node_modules"})
	if removed != 0 {
		t.Errorf("removed %d artifacts through a junction, want 0", removed)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("GC deleted node_modules inside the junction target: %v", err)
	}
}

func TestTaskSize_DoesNotCountDirectoryJunction(t *testing.T) {
	taskDir := t.TempDir()
	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, "big.bin"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "own.bin"), make([]byte, 16), 0o644); err != nil {
		t.Fatal(err)
	}
	createJunction(t, linked, filepath.Join(taskDir, "codex-home", "skills", "alpha"))

	total, artifacts := taskSize(taskDir, newArtifactMatcher([]string{"node_modules"}, nil))
	if total != 16 {
		t.Errorf("total = %d, want 16 (junction target must not be counted)", total)
	}
	if artifacts != 0 {
		t.Errorf("artifacts = %d, want 0", artifacts)
	}
}
