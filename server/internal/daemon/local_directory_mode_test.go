package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalDirectoryAssignmentPropagatesMode(t *testing.T) {
	const daemonID = "d-mine"
	tmp := t.TempDir()

	mk := func(mode string) []ProjectResourceData {
		raw, _ := json.Marshal(localDirectoryRef{LocalPath: tmp, DaemonID: daemonID, Mode: mode})
		return []ProjectResourceData{{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: raw}}
	}

	cases := []struct{ in, want string }{
		{"", localDirectoryModeInPlace},
		{"in_place", localDirectoryModeInPlace},
		{"worktree", localDirectoryModeWorktree},
	}
	for _, c := range cases {
		got, err := localDirectoryAssignmentForTask(Task{ID: "t1", IssueID: "issue-1", ProjectResources: mk(c.in)}, daemonID)
		if err != nil || got == nil {
			t.Fatalf("mode=%q: (%+v, %v)", c.in, got, err)
		}
		if got.Mode != c.want {
			t.Errorf("mode=%q: Mode=%q want %q", c.in, got.Mode, c.want)
		}
		if got.IssueID != "issue-1" {
			t.Errorf("IssueID=%q want issue-1", got.IssueID)
		}
	}
}

func TestLocalDirectoryAssignmentRejectsUnknownMode(t *testing.T) {
	const daemonID = "d-mine"
	tmp := t.TempDir()
	raw, _ := json.Marshal(localDirectoryRef{LocalPath: tmp, DaemonID: daemonID, Mode: "parallel"})
	_, err := localDirectoryAssignmentForTask(Task{ID: "t1", IssueID: "i", ProjectResources: []ProjectResourceData{
		{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: raw},
	}}, daemonID)
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestIssueLockKey(t *testing.T) {
	a := issueLockKey("issue-1")
	b := issueLockKey("issue-1")
	c := issueLockKey("issue-2")
	if a != b {
		t.Errorf("same issue produced different keys: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("different issues collapsed to same key %q", a)
	}
	if a != "issue:issue-1" {
		t.Errorf("issueLockKey=%q, want issue:issue-1", a)
	}
}

// A definite non-git path is the only worktree request that degrades to
// in_place. Operational git failures are tested separately and fail closed.
func TestProbeGitWorkTree_FalseForNonGit(t *testing.T) {
	tmp := t.TempDir() // exists, writable, but not a git repo
	got, err := probeGitWorkTree(context.Background(), tmp)
	if err != nil {
		t.Fatalf("probeGitWorkTree: %v", err)
	}
	if got {
		t.Fatal("probeGitWorkTree true for non-git path")
	}
}

func TestProbeGitWorkTree_FailsClosedForBrokenGitMetadata(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, ".git"), []byte("gitdir: /missing/repository"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := probeGitWorkTree(context.Background(), tmp); err == nil {
		t.Fatal("expected broken git metadata to fail closed")
	}
}

func TestLocalDirectoryExecutionContextPreservesGateDecision(t *testing.T) {
	want := filepath.Join("/ws", "localwt", "d-1", "issue-7")
	execution := &localDirectoryExecution{WorkDir: want, Worktree: true, Release: func() {}}
	got := localDirectoryExecutionFromContext(withLocalDirectoryExecution(context.Background(), execution))
	if got != execution || got.WorkDir != want || !got.Worktree {
		t.Fatalf("execution context = %+v, want %+v", got, execution)
	}
}

// Cross-issue parallelism is the core invariant of worktree mode: two tasks on
// different issues must acquire their locks concurrently, while two tasks on the
// same issue serialize behind one key. Tested at the locker level because the
// key derivation (issueLockKey) is the only thing that varies from in_place.
func TestIssueLock_ParallelAcrossIssuesSerialWithinIssue(t *testing.T) {
	locker := NewLocalPathLocker()
	release, err := locker.Acquire(context.Background(), issueLockKey("issue-A"), "task-a1", nil)
	if err != nil {
		t.Fatalf("acquire issue-A: %v", err)
	}
	defer release()

	// Different issue must not block.
	done := make(chan struct{})
	go func() {
		rel, err := locker.Acquire(context.Background(), issueLockKey("issue-B"), "task-b1", nil)
		if err != nil {
			t.Errorf("acquire issue-B: %v", err)
			return
		}
		rel()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("issue-B blocked behind issue-A")
	}

	// Same issue must serialize.
	serialDone := make(chan struct{})
	go func() {
		rel, err := locker.Acquire(context.Background(), issueLockKey("issue-A"), "task-a2", nil)
		if err != nil {
			t.Errorf("acquire issue-A second: %v", err)
			return
		}
		rel()
		close(serialDone)
	}()
	select {
	case <-serialDone:
		t.Fatal("second task on issue-A did not serialize behind first")
	case <-time.After(100 * time.Millisecond):
		// expected: still parked while release is deferred
	}
}

// acquireLocalDirectoryLockIfNeeded in worktree mode against a real git repo
// creates the per-issue worktree on the fast (uncontended) path. Validates the
// wiring between the lock gate and ensureIssueWorktree end to end.
func TestAcquireLock_WorktreeMode_CreatesIssueWorktree(t *testing.T) {
	t.Parallel()
	const daemonID = "d-test"
	repo := initLocalRepo(t)

	raw, _ := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: daemonID, Mode: localDirectoryModeWorktree})
	task := Task{ID: "task-1", IssueID: "issue-42", ProjectResources: []ProjectResourceData{
		{ID: "r1", ResourceType: localDirectoryResourceType, ResourceRef: raw},
	}}

	d := &Daemon{
		cfg:            Config{DaemonID: daemonID, WorkspacesRoot: t.TempDir()},
		localPathLocks: NewLocalPathLocker(),
		repoGitLocks:   NewLocalPathLocker(),
		logger:         slog.Default(),
	}

	release, abort := d.acquireLocalDirectoryLockIfNeeded(context.Background(), task, slog.Default())
	if abort {
		t.Fatal("acquire aborted")
	}
	if release == nil {
		t.Fatal("nil release")
	}
	defer release()

	wtPath := issueWorktreePath(d.cfg.WorkspacesRoot, daemonID, "issue-42")
	if !isGitWorktreeDir(wtPath) {
		t.Errorf("worktree not created at %s", wtPath)
	}
	// Lock keyed on the issue, not the repo realpath.
	if got := d.localPathLocks.Holder(issueLockKey("issue-42")); got != task.ID {
		t.Errorf("issue lock holder=%q want %q", got, task.ID)
	}
}
