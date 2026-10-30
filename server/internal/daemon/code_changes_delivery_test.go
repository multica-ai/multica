package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestCodeChangesRejectsUndeliveredWorktreeHead(t *testing.T) {
	root := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git(root, "init")
	git(root, "commit", "--allow-empty", "-m", "base")
	worktree := filepath.Join(t.TempDir(), "task")
	git(root, "worktree", "add", "-b", "refused-delivery", worktree)
	local := &execenv.LocalWorktree{GitRoot: root, Path: worktree, WorkDir: worktree, Branch: "refused-delivery"}
	env := &execenv.Environment{WorkDir: worktree, LocalWorktree: local}
	capture := prepareTaskCodeChanges(context.Background(), Task{IssueID: "issue", CodeChangesEnabled: true, RuntimeID: publicCodeChangesRef().RuntimeID, DispatchedAt: publicCodeChangesRef().DispatchedAt}, env)
	if capture == nil {
		t.Fatal("capture not prepared")
	}
	if err := os.WriteFile(filepath.Join(worktree, ".mcp.json"), []byte("private-runtime-test-sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	git(worktree, "add", "-A")
	git(worktree, "commit", "-m", "agent commit before cleanup failure")
	local.AbortWithReason(errors.New("runtime sidecar cleanup failed"))
	outcome, err := local.Finalize(nil)
	if err == nil || outcome.Branch != "" || outcome.PreservedPath != worktree {
		t.Fatalf("expected refused delivery with preserved work: outcome=%+v err=%v", outcome, err)
	}
	result := TaskResult{Status: "failed", WorkDir: outcome.PreservedPath}
	capture.finish(&result)
	if result.CodeChanges == nil || result.CodeChanges.Reason != "capture_failed" || len(result.CodeChanges.Files) != 0 {
		t.Fatal("undelivered HEAD must produce an empty failed capture")
	}
	if result.WorkDir != worktree || result.BranchName != "" || result.Status != "failed" {
		t.Fatal("capture changed the preserved-work receipt")
	}
}
