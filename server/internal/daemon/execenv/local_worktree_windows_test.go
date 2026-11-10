//go:build windows

package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRemoveLocalWorktreeDirKeepsJunctionTarget(t *testing.T) {
	t.Parallel()
	target := t.TempDir()
	sentinel := filepath.Join(target, "keep.txt")
	writeFile(t, sentinel, "user files")

	worktree := filepath.Join(t.TempDir(), "worktree")
	junction := filepath.Join(worktree, "node_modules", "pkg")
	makeWorktreeJunction(t, junction, target)

	requireWorktreeRemovedAfterGitSuccess(t, worktree, nil)
	if got := readFile(t, sentinel); got != "user files" {
		t.Errorf("junction target content = %q, want unchanged user files", got)
	}
}

func TestRemoveLocalWorktreeDirRemovesDanglingJunction(t *testing.T) {
	t.Parallel()
	worktree := filepath.Join(t.TempDir(), "worktree")
	packages := filepath.Join(worktree, "packages")
	target := filepath.Join(packages, "pkg")
	writeFile(t, filepath.Join(target, "index.js"), "module.exports = {};")
	junction := filepath.Join(worktree, "node_modules", "pkg")
	makeWorktreeJunction(t, junction, target)

	requireWorktreeRemovedAfterGitSuccess(t, worktree, func() {
		// Git deleted the package but left its workspace junction behind.
		if err := os.RemoveAll(packages); err != nil {
			t.Fatalf("simulate Git removing the junction target: %v", err)
		}
		assertAbsent(t, target)
		if _, err := os.Lstat(junction); err != nil {
			t.Fatalf("expected Git to leave a dangling junction: %v", err)
		}
	})
}

func makeWorktreeJunction(t *testing.T, junction, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(junction), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %s: %v", out, err)
	}
}
