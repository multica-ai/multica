//go:build windows

package execenv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startWorktreeHolder launches a suspended child whose working directory is
// the worktree and returns a release function plus the process. Windows
// refuses to delete a directory that is any process's cwd, so while the child
// is alive the worktree is pinned exactly the way a parked warm-pool provider
// process pins it. The child is created suspended so it never runs — the cwd
// pin exists from creation, and the resume performed by the caller's timing
// is what models the provider exiting late.
//
// The process is killed and reaped unconditionally at test end so a failure
// cannot leak a sleeper into the test host.
func startWorktreeHolder(t *testing.T, worktreePath string) (release func(), wait func()) {
	t.Helper()
	cmd := exec.Command(os.Getenv("COMSPEC"))
	if cmd.Path == "" {
		cmd = exec.Command("cmd.exe")
	}
	cmd.Args = append(cmd.Args, "/c", "ping", "-n", "30", "127.0.0.1", ">nul")
	cmd.Dir = worktreePath
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x00000004 // CREATE_SUSPENDED
	if err := cmd.Start(); err != nil {
		t.Fatalf("start pinned-cwd holder: %v", err)
	}
	wait = func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	t.Cleanup(wait)
	release = func() {
		// Resume via ntdll NtResumeProcess is what a real process does when it
		// finishes; killing achieves the same handle teardown from the test's
		// perspective, and is simpler: the pin ends when the process dies.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return release, wait
}

func TestRemoveLocalWorktreeDirRetriesUntilThePinningProcessExits(t *testing.T) {
	repo := newTestRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	out, err := exec.Command("git", "-C", repo, "worktree", "add", "-b", "pin-test", worktreePath).CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree add: %s: %v", strings.TrimSpace(string(out)), err)
	}

	release, _ := startWorktreeHolder(t, worktreePath)

	// Let go of the pin shortly after removal starts: the first unlink must
	// hit the sharing violation, a later attempt must succeed.
	go func() {
		time.Sleep(600 * time.Millisecond)
		release()
	}()

	started := time.Now()
	if err := removeLocalWorktreeDir(repo, worktreePath, worktreeTestLogger()); err != nil {
		t.Fatalf("removeLocalWorktreeDir with a released pin: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond {
		t.Fatalf("removal returned in %s; the pinned first attempt should have forced at least one backoff cycle", elapsed)
	}
	if _, err := os.Lstat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree path still present after successful removal: %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").CombinedOutput(); err != nil {
		t.Fatalf("git worktree list: %v", err)
	} else if strings.Contains(string(out), worktreePath) {
		t.Fatalf("worktree still registered: %s", out)
	}
}

func TestRemoveLocalWorktreeDirFailsWhenThePinNeverLifts(t *testing.T) {
	repo := newTestRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	out, err := exec.Command("git", "-C", repo, "worktree", "add", "-b", "pin-stuck-test", worktreePath).CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree add: %s: %v", strings.TrimSpace(string(out)), err)
	}

	// Never released: the holder dies only via t.Cleanup at test end. Removal
	// must exhaust its bounded budget and report failure rather than hang.
	started := time.Now()
	err = removeLocalWorktreeDir(repo, worktreePath, worktreeTestLogger())
	if err == nil {
		t.Fatal("removeLocalWorktreeDir succeeded under a permanent pin; want a bounded failure")
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("removal took %s; want a small bounded budget", elapsed)
	}
	if _, statErr := os.Lstat(worktreePath); statErr != nil {
		t.Fatalf("worktree path should survive the failed removal: %v", statErr)
	}
	// The error must name why removal could not complete — the caller folds it
	// into the task failure the operator reads.
	if got := fmt.Sprint(err); !strings.Contains(got, "still exists") {
		t.Fatalf("error does not describe the surviving worktree: %q", got)
	}
}
