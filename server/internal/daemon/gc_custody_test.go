package daemon

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCleanTaskDirRetainsUniqueCommits(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for custody checks")
	}

	d := newGCTestDaemon(t, http.NewServeMux())
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "unique-git", nil)
	repo := filepath.Join(taskDir, "workdir", "command-center")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "custody@example.com")
	runGit(t, repo, "config", "user.name", "Custody")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("unique\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "sole copy")

	if got := d.cleanTaskDir(taskDir); got != 0 {
		t.Fatalf("cleanTaskDir reclaimed %d bytes of a repo with unique commits", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("unique work was deleted: %v", err)
	}
}

func TestCleanTaskDirReclaimsCleanClone(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for custody checks")
	}

	d := newGCTestDaemon(t, http.NewServeMux())
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws1", "clean-git", nil)
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare")

	repo := filepath.Join(taskDir, "workdir", "command-center")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "custody@example.com")
	runGit(t, repo, "config", "user.name", "Custody")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "on remote")
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "-u", "origin", "HEAD")

	if got := d.cleanTaskDir(taskDir); got == 0 {
		t.Fatal("cleanTaskDir retained a regenerable clone")
	}
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Fatalf("regenerable task dir still present: %v", err)
	}
}

func TestTaskDirCustodyHoldUntracked(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for custody checks")
	}
	taskDir := t.TempDir()
	repo := filepath.Join(taskDir, "workdir", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "custody@example.com")
	runGit(t, repo, "config", "user.name", "Custody")
	if err := os.WriteFile(filepath.Join(repo, "committed.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := taskDirCustodyHold(context.Background(), taskDir); got == "" {
		t.Fatal("expected a hold for untracked files")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
