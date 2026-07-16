package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"
)

// initLocalRepo creates a real git repository with one commit, so
// `git worktree add` has a valid HEAD to branch from. Returns the repo path.
func initLocalRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s: %v", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
		}
	}
	mustGit("init", "-b", "main")
	mustGit("config", "user.email", "t@t")
	mustGit("config", "user.name", "t")
	mustGit("commit", "--allow-empty", "-m", "init")
	return dir
}

func TestIssueWorktreePathAndBranch(t *testing.T) {
	path := issueWorktreePath("/ws/root", "d-1", "issue-9")
	if path != filepath.Join("/ws/root", "localwt", "d-1", "issue-9") {
		t.Errorf("issueWorktreePath = %q", path)
	}
	if got := issueWorktreeBranch("issue-9"); got != "multica/issue-issue-9" {
		t.Errorf("issueWorktreeBranch = %q", got)
	}
}

func TestEnsureIssueWorktree_RejectsEmptyIssueID(t *testing.T) {
	repo := initLocalRepo(t)
	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir(), DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}
	if _, _, err := d.ensureIssueWorktree(context.Background(), repo, "", ""); err == nil {
		t.Fatal("expected error for empty issueID")
	}
}

func TestEnsureIssueWorktree_RejectsPathTraversalKeys(t *testing.T) {
	repo := initLocalRepo(t)
	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir(), DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}
	for _, issueID := range []string{"../escape", "a/b", `a\\b`, ".."} {
		if _, _, err := d.ensureIssueWorktree(context.Background(), repo, issueID, ""); err == nil {
			t.Errorf("issueID %q: expected rejection", issueID)
		}
	}
}

func TestEnsureIssueWorktree_CreatesFromLocalRepo(t *testing.T) {
	repo := initLocalRepo(t)
	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir(), DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}

	wtPath, branch, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", "")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !strings.Contains(wtPath, filepath.Join("localwt", "d-test", "issue-1")) {
		t.Errorf("wtPath=%q, want under localwt/d-test/issue-1", wtPath)
	}
	if branch != "multica/issue-issue-1" {
		t.Errorf("branch=%q, want multica/issue-issue-1", branch)
	}
	if !isGitWorktreeDir(wtPath) {
		t.Errorf("wtPath %q is not a git worktree", wtPath)
	}
}

func TestEnsureIssueWorktree_ReusesSameIssue(t *testing.T) {
	repo := initLocalRepo(t)
	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir(), DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}

	wt1, branch1, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", "")
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	// Make a commit in the worktree so we can assert reuse preserves state.
	if out, err := exec.Command("git", "-C", wt1, "commit", "--allow-empty", "-m", "work").CombinedOutput(); err != nil {
		t.Fatalf("commit in worktree: %s: %v", out, err)
	}

	wt2, branch2, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", "")
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if wt1 != wt2 || branch1 != branch2 {
		t.Errorf("reuse changed identity: path %q→%q branch %q→%q", wt1, wt2, branch1, branch2)
	}
	// Prior commit must survive reuse (same branch checked out, not reset).
	out, err := exec.Command("git", "-C", wt2, "log", "--oneline", "-1").Output()
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(string(out), "work") {
		t.Errorf("reuse lost prior commit; log=%s", out)
	}
}

func TestEnsureIssueWorktree_RejectsUnexpectedCanonicalDirectory(t *testing.T) {
	repo := initLocalRepo(t)
	root := t.TempDir()
	d := &Daemon{cfg: Config{WorkspacesRoot: root, DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}
	wtPath := issueWorktreePath(root, "d-test", "issue-1")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(wtPath, "keep.txt")
	if err := os.WriteFile(marker, []byte("user data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", ""); err == nil {
		t.Fatal("expected canonical non-worktree directory to fail closed")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("unexpected directory was modified: %v", err)
	}
}

func TestEnsureIssueWorktree_RejectsSymlinkedManagedRoot(t *testing.T) {
	repo := initLocalRepo(t)
	root := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "localwt")); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{cfg: Config{WorkspacesRoot: root, DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}
	if _, _, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", ""); err == nil {
		t.Fatal("expected symlinked managed root to fail closed")
	}
	entries, err := os.ReadDir(external)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("worktree creation escaped through symlink: %v", entries)
	}
}

func TestEnsureIssueWorktree_RejectsWorktreeFromDifferentRepo(t *testing.T) {
	repoA := initLocalRepo(t)
	repoB := initLocalRepo(t)
	root := t.TempDir()
	d := &Daemon{cfg: Config{WorkspacesRoot: root, DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}
	wtPath, _, err := d.ensureIssueWorktree(context.Background(), repoA, "issue-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.ensureIssueWorktree(context.Background(), repoB, "issue-1", ""); err == nil {
		t.Fatal("expected existing worktree from a different repository to be rejected")
	}
	if !isGitWorktreeDir(wtPath) {
		t.Fatal("existing worktree was removed or damaged")
	}
}

func TestGitCommonDir_CollapsesRepoSubdirectoryAndLinkedWorktree(t *testing.T) {
	repo := initLocalRepo(t)
	if err := os.Mkdir(filepath.Join(repo, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir(), DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}
	wtPath, _, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", "")
	if err != nil {
		t.Fatal(err)
	}
	want, err := gitCommonDir(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(repo, "subdir"), wtPath} {
		got, err := gitCommonDir(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("gitCommonDir(%q)=%q, want %q", path, got, want)
		}
	}
}

func TestEnsureIssueWorktree_DistinctIssuesAreDistinctPaths(t *testing.T) {
	repo := initLocalRepo(t)
	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir(), DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}

	wt1, _, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", "")
	if err != nil {
		t.Fatal(err)
	}
	wt2, _, err := d.ensureIssueWorktree(context.Background(), repo, "issue-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if wt1 == wt2 {
		t.Fatalf("two issues share one worktree path %q", wt1)
	}
}

func TestEnsureIssueWorktree_BasesOnProvidedRef(t *testing.T) {
	repo := initLocalRepo(t)
	// Create a second commit and capture its SHA; baseRef must point HEAD there.
	if out, err := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "second").CombinedOutput(); err != nil {
		t.Fatalf("commit: %s: %v", out, err)
	}
	headOut, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	head := strings.TrimSpace(string(headOut))

	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir(), DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}
	wtPath, _, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", head)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	wtHead, err := exec.Command("git", "-C", wtPath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("worktree rev-parse: %v", err)
	}
	if strings.TrimSpace(string(wtHead)) != head {
		t.Errorf("worktree HEAD=%q, want base ref %q", wtHead, head)
	}
}

// Branch survives a directory removal (GC): ensureIssueWorktree recreates the
// worktree on the existing branch instead of branching from base again.
func TestEnsureIssueWorktree_RecoversBranchAfterDirRemoval(t *testing.T) {
	repo := initLocalRepo(t)
	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir(), DaemonID: "d-test"}, logger: slog.Default(), repoGitLocks: NewLocalPathLocker()}

	wt1, _, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", wt1, "commit", "--allow-empty", "-m", "persist").CombinedOutput(); err != nil {
		t.Fatalf("commit: %s: %v", out, err)
	}
	// Simulate GC removing the directory but leaving the branch in the main repo.
	if err := d.removeIssueWorktree(context.Background(), repo, wt1); err != nil {
		t.Fatalf("remove: %v", err)
	}

	wt2, _, err := d.ensureIssueWorktree(context.Background(), repo, "issue-1", "")
	if err != nil {
		t.Fatalf("re-ensure after removal: %v", err)
	}
	out, err := exec.Command("git", "-C", wt2, "log", "--oneline").Output()
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(string(out), "persist") {
		t.Errorf("branch recovery lost prior commit; log=%s", out)
	}
}
