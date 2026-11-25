//go:build windows

package execenv

import (
	"bufio"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// startWorktreeHolder waits for a live process to take the worktree as its
// working directory, the same pin a parked provider leaves on Windows.
func startWorktreeHolder(t *testing.T, worktreePath string) func() {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "ping.exe", "-n", "120", "127.0.0.1")
	cmd.Dir = worktreePath
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("open holder stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start pinned-cwd holder: %v", err)
	}
	release := sync.OnceFunc(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	t.Cleanup(release)
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatalf("wait for pinned-cwd holder: %v", err)
	}
	return release
}

func TestRemoveLocalWorktreeDirRetriesUntilThePinningProcessExits(t *testing.T) {
	repo := newTestRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	gitRun(t, repo, "worktree", "add", "-b", "pin-test", worktreePath)
	release := startWorktreeHolder(t, worktreePath)

	// Release the pin between attempts, after the first removal fails.
	retried := false
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == "backoff" {
				retried = true
				release()
			}
			return attr
		},
	}))
	if err := removeLocalWorktreeDir(repo, worktreePath, logger); err != nil {
		t.Fatalf("removeLocalWorktreeDir with a released pin: %v", err)
	}
	if !retried {
		t.Fatal("the pinned first removal should have required a retry")
	}
	assertRemovedWorktree(t, repo, worktreePath, "pin-test")
}

func TestRemoveLocalWorktreeDirFailsWhenThePinNeverLifts(t *testing.T) {
	repo := newTestRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	gitRun(t, repo, "worktree", "add", "-b", "pin-stuck-test", worktreePath)
	startWorktreeHolder(t, worktreePath)

	started := time.Now()
	err := removeLocalWorktreeDir(repo, worktreePath, worktreeTestLogger())
	if err == nil {
		t.Fatal("removeLocalWorktreeDir succeeded under a permanent pin; want a bounded failure")
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("removal took %s; want a small bounded budget", elapsed)
	}
	if _, statErr := os.Lstat(worktreePath); statErr != nil {
		t.Fatalf("worktree path should survive the failed removal: %v", statErr)
	}
	if got := err.Error(); !strings.Contains(got, "still exists") {
		t.Fatalf("error does not describe the surviving worktree: %q", got)
	}
	gitRun(t, repo, "show-ref", "--verify", "refs/heads/pin-stuck-test")
}

func TestRemoveLocalWorktreeDirCleansUpJunctionOnFirstAttempt(t *testing.T) {
	repo := newTestRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	gitRun(t, repo, "worktree", "add", "-b", "junction-test", worktreePath)

	scopeDir := filepath.Join(worktreePath, "node_modules", "@scope")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(worktreePath, "packages", "pkg")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// npm workspace links point inside the worktree. An external target
	// also checks that cleanup does not follow directory junctions.
	externalPackage := t.TempDir()
	marker := filepath.Join(externalPackage, "package.json")
	if err := os.WriteFile(marker, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"pkg": packageDir, "external": externalPackage} {
		junction := filepath.Join(scopeDir, name)
		if out, err := exec.Command("cmd", "/d", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J %s: %s: %v", name, out, err)
		}
	}
	if err := removeLocalWorktreeDirOnce(repo, worktreePath, worktreeTestLogger()); err != nil {
		t.Fatalf("remove worktree with a junction on the first attempt: %v", err)
	}
	assertRemovedWorktree(t, repo, worktreePath, "junction-test")
	if body, err := os.ReadFile(marker); err != nil || string(body) != "{}" {
		t.Fatalf("junction target was changed: contents %q, error %v", body, err)
	}
}

func assertRemovedWorktree(t *testing.T, repo, worktreePath, branch string) {
	t.Helper()
	if _, err := os.Lstat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree path still present after successful removal: %v", err)
	}
	ref := "refs/heads/" + branch
	for line := range strings.Lines(gitRun(t, repo, "worktree", "list", "--porcelain")) {
		if strings.TrimSpace(line) == "branch "+ref {
			t.Fatalf("worktree for %s is still registered", branch)
		}
	}
	gitRun(t, repo, "show-ref", "--verify", ref)
}
