package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type localDirectoryExecution struct {
	Release  func()
	WorkDir  string
	Worktree bool
}

type localDirectoryExecutionContextKey struct{}

func withLocalDirectoryExecution(ctx context.Context, execution *localDirectoryExecution) context.Context {
	if execution == nil {
		return ctx
	}
	return context.WithValue(ctx, localDirectoryExecutionContextKey{}, execution)
}

func localDirectoryExecutionFromContext(ctx context.Context) *localDirectoryExecution {
	execution, _ := ctx.Value(localDirectoryExecutionContextKey{}).(*localDirectoryExecution)
	return execution
}

// issueWorktreeBranch is the git branch name the daemon creates for a
// per-issue worktree. The multica/ namespace keeps these branches visually
// distinct from the user's own branches. Unlike the github_repo path (which
// reaps stale branches from the daemon-owned bare cache), local worktree
// branches live in the user's own repository and are intentionally NOT
// auto-deleted by GC — they are cheap, clearly namespaced, and preserving
// them lets ensureIssueWorktree recover an issue's work after a directory
// removal. Users can prune them manually with `git branch -D multica/issue-*`.
func issueWorktreeBranch(issueID string) string {
	return "multica/issue-" + issueID
}

// issueWorktreePath returns the deterministic on-disk location of the per-issue
// worktree for a local_directory worktree-mode task. It lives under the
// daemon's WorkspacesRoot — outside the user's repository — so the user's tree
// is never littered with worktree metadata. Keyed by daemon then issue: two
// daemons on the same host never collide, and different issues get different
// directories (the basis for cross-issue parallelism).
func issueWorktreePath(workspacesRoot, daemonID, issueID string) string {
	return filepath.Join(workspacesRoot, "localwt", daemonID, issueID)
}

// ensureIssueWorktree creates the per-issue git worktree off the user's local
// repository if it does not yet exist, or reuses the existing one. It returns
// the worktree path and branch name.
//
// localRepo is the user's checkout (the caller has already validated it exists
// and is inside a Git working tree). baseRef, when non-empty, is the
// commit/branch/tag the worktree branch is created from; empty falls back to
// HEAD so the agent starts
// from the user's latest committed state. Uncommitted changes in the user's
// working tree are intentionally NOT carried — a worktree branches from a
// commit, not a dirty index.
//
// Reuse contract: once a worktree exists at the canonical path the daemon keeps
// it as-is. A follow-up task on the same issue continues the existing branch
// rather than resetting to baseRef, so accumulated edits survive across the
// issue's task chain. If the directory was removed (e.g. by GC) but the branch
// still exists in the user's repo, the worktree is recreated on that branch so
// the issue's prior work is recovered instead of being branched away from.
//
// The caller MUST hold the per-issue LocalPathLocker so two cold-start tasks on
// the same issue don't race `git worktree add`.
func (d *Daemon) ensureIssueWorktree(ctx context.Context, localRepo, issueID, baseRef string) (wtPath, branch string, err error) {
	if err := validateWorktreePathKey("daemonID", d.cfg.DaemonID); err != nil {
		return "", "", err
	}
	if err := validateWorktreePathKey("issueID", issueID); err != nil {
		return "", "", err
	}
	wtPath = issueWorktreePath(d.cfg.WorkspacesRoot, d.cfg.DaemonID, issueID)
	branch = issueWorktreeBranch(issueID)
	if err := validateManagedWorktreePath(d.cfg.WorkspacesRoot, wtPath); err != nil {
		return "", "", err
	}

	// Serialise git mutations on the same repository. Different-issue tasks
	// hold independent issue locks, so without this two of them could race
	// `git worktree add` on the user's repo and contend on git's lockfiles.
	// Keying on the common git dir (not the supplied working-tree path) also
	// collapses subdirectories and already-linked worktrees of the same repo.
	repoKey, commonErr := gitCommonDir(ctx, localRepo)
	if commonErr != nil {
		return "", "", fmt.Errorf("local_directory worktree: resolve git common dir: %w", commonErr)
	}
	gitRelease, err := d.repoGitLocks.Acquire(ctx, repoKey, "ensure-worktree:"+issueID, nil)
	if err != nil {
		return "", "", fmt.Errorf("local_directory worktree: repo lock: %w", err)
	}
	defer gitRelease()

	exists, validateErr := validateIssueWorktree(ctx, wtPath, repoKey, branch)
	if validateErr != nil {
		return "", "", validateErr
	}
	if exists {
		return wtPath, branch, nil
	}

	if baseRef == "" {
		baseRef = "HEAD"
	}

	branchExists, err := gitLocalBranchExists(ctx, localRepo, branch)
	if err != nil {
		return "", "", fmt.Errorf("local_directory worktree: inspect branch %q: %w", branch, err)
	}
	checkoutExisting := func() error {
		checkoutErr := gitWorktreeCheckoutExisting(ctx, localRepo, wtPath, branch)
		if checkoutErr == nil {
			return nil
		}
		// A second daemon process can win the same canonical-path creation
		// race because repoGitLocks is process-local. Accept only the exact
		// repo+branch worktree that process created; every other collision
		// remains a fail-closed error.
		exists, validateErr := validateIssueWorktree(ctx, wtPath, repoKey, branch)
		if validateErr == nil && exists {
			return nil
		}
		if validateErr != nil {
			return fmt.Errorf("%w; validate concurrent worktree: %v", checkoutErr, validateErr)
		}
		return checkoutErr
	}
	if branchExists {
		if err := checkoutExisting(); err != nil {
			return "", "", fmt.Errorf("local_directory worktree (reuse branch %q): %w", branch, err)
		}
		return wtPath, branch, nil
	}

	// Fresh branch from the base ref is the common path for a new issue. A
	// second process can create the branch after our check; in that case retry
	// through the existing-branch path instead of parsing localized stderr.
	if err := gitWorktreeAddBranch(ctx, localRepo, wtPath, branch, baseRef); err != nil {
		nowExists, checkErr := gitLocalBranchExists(ctx, localRepo, branch)
		if checkErr != nil || !nowExists {
			return "", "", fmt.Errorf("local_directory worktree: %w", err)
		}
		if checkoutErr := checkoutExisting(); checkoutErr != nil {
			return "", "", fmt.Errorf("local_directory worktree (reuse branch %q): %w", branch, checkoutErr)
		}
	}
	return wtPath, branch, nil
}

// removeIssueWorktree deletes a per-issue worktree from the user's repo. Used
// by the GC path once an issue has been terminal long enough to expire. The
// branch is left in place — it is cheap and lets a late follow-up task recover
// the issue's work via ensureIssueWorktree's branch-reuse path.
func (d *Daemon) removeIssueWorktree(ctx context.Context, localRepo, wtPath string) error {
	// Take the repo git lock so a GC removal can't race a concurrent
	// ensureIssueWorktree on the same repository.
	repoKey, err := gitCommonDir(ctx, localRepo)
	if err != nil {
		return fmt.Errorf("local_directory worktree: resolve git common dir: %w", err)
	}
	release, err := d.repoGitLocks.Acquire(ctx, repoKey, "gc-remove-worktree", nil)
	if err != nil {
		return fmt.Errorf("local_directory worktree: repo lock: %w", err)
	}
	defer release()
	return removeIssueWorktreeWithCommonDir(ctx, repoKey, wtPath)
}

func (d *Daemon) removeIssueWorktreeWithCommonDir(ctx context.Context, commonDir, wtPath string) error {
	release, err := d.repoGitLocks.Acquire(ctx, commonDir, "gc-remove-worktree", nil)
	if err != nil {
		return fmt.Errorf("local_directory worktree: repo lock: %w", err)
	}
	defer release()
	return removeIssueWorktreeWithCommonDir(ctx, commonDir, wtPath)
}

func removeIssueWorktreeWithCommonDir(ctx context.Context, commonDir, wtPath string) error {
	// Do not use --force: the preceding clean check is advisory, and an external
	// process can still write after it. Let git re-check and refuse removal if
	// the worktree became dirty in that window.
	out, err := exec.CommandContext(ctx, "git", "--git-dir", commonDir, "worktree", "remove", "--", wtPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// gitWorktreeAddBranch creates a new branch from baseRef in a new worktree.
func gitWorktreeAddBranch(ctx context.Context, repo, wtPath, branch, baseRef string) error {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "worktree", "add", "-b", branch, "--", wtPath, baseRef).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add %s: %s: %w", branch, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// gitWorktreeCheckoutExisting checks an existing branch out into a new worktree
// without creating a branch.
func gitWorktreeCheckoutExisting(ctx context.Context, repo, wtPath, branch string) error {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "worktree", "add", "--", wtPath, branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add (checkout %s): %s: %w", branch, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// gitLocalBranchExists checks the ref directly instead of parsing localized git
// error text from a failed worktree add.
func gitLocalBranchExists(ctx context.Context, repo, branch string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// isGitWorktreeDir reports whether path is an existing git worktree (a .git
// file, not directory, sits at its root). Mirrors repocache.isGitWorktree.
func isGitWorktreeDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && !info.IsDir()
}

func validateWorktreePathKey(name, value string) error {
	if value == "" {
		return fmt.Errorf("local_directory worktree: %s is required", name)
	}
	if value != strings.TrimSpace(value) || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("local_directory worktree: invalid %s %q", name, value)
	}
	return nil
}

// validateManagedWorktreePath rejects symlinked components below the configured
// workspace root. Without this, a local process could replace localwt or a
// daemon/issue directory with a symlink and redirect task or GC writes outside
// the daemon-owned subtree.
func validateManagedWorktreePath(workspacesRoot, target string) error {
	root, err := filepath.Abs(workspacesRoot)
	if err != nil {
		return fmt.Errorf("local_directory worktree: resolve workspace root: %w", err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("local_directory worktree: resolve target path: %w", err)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("local_directory worktree: target %s escapes workspace root %s", target, root)
	}
	current := root
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return fmt.Errorf("local_directory worktree: inspect managed path %s: %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("local_directory worktree: managed path contains symlink: %s", current)
		}
	}
	return nil
}

func probeGitWorkTree(ctx context.Context, path string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--is-inside-work-tree")
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(out)) == "true", nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if strings.Contains(strings.ToLower(string(out)), "not a git repository") {
		if marker, markerErr := findGitMarker(path); markerErr != nil {
			return false, markerErr
		} else if marker != "" {
			return false, fmt.Errorf("git metadata at %s is present but unusable: %s", marker, strings.TrimSpace(string(out)))
		}
		return false, nil
	}
	return false, fmt.Errorf("git rev-parse: %s: %w", strings.TrimSpace(string(out)), err)
}

func findGitMarker(path string) (string, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for {
		marker := filepath.Join(current, ".git")
		if _, err := os.Lstat(marker); err == nil {
			return marker, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect git metadata %s: %w", marker, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}

func gitCommonDir(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--path-format=absolute", "--git-common-dir").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir: %s: %w", strings.TrimSpace(string(out)), err)
	}
	commonDir := strings.TrimSpace(string(out))
	if commonDir == "" || !filepath.IsAbs(commonDir) {
		return "", fmt.Errorf("git returned invalid common dir %q", commonDir)
	}
	commonDir = filepath.Clean(commonDir)
	if real, evalErr := filepath.EvalSymlinks(commonDir); evalErr == nil {
		commonDir = real
	}
	return commonDir, nil
}

func validateIssueWorktree(ctx context.Context, wtPath, expectedCommonDir, expectedBranch string) (bool, error) {
	info, err := os.Lstat(wtPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("local_directory worktree: inspect %s: %w", wtPath, err)
	}
	gitMarker, markerErr := os.Lstat(filepath.Join(wtPath, ".git"))
	if !info.IsDir() || markerErr != nil || !gitMarker.Mode().IsRegular() {
		return false, fmt.Errorf("local_directory worktree: canonical path exists but is not a linked git worktree: %s", wtPath)
	}
	actualCommonDir, err := gitCommonDir(ctx, wtPath)
	if err != nil {
		return false, fmt.Errorf("local_directory worktree: validate existing common dir: %w", err)
	}
	if actualCommonDir != expectedCommonDir {
		return false, fmt.Errorf("local_directory worktree: canonical path belongs to %s, expected %s", actualCommonDir, expectedCommonDir)
	}
	out, err := exec.CommandContext(ctx, "git", "-C", wtPath, "symbolic-ref", "--quiet", "--short", "HEAD").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("local_directory worktree: validate existing branch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if actualBranch := strings.TrimSpace(string(out)); actualBranch != expectedBranch {
		return false, fmt.Errorf("local_directory worktree: canonical path is on branch %q, expected %q", actualBranch, expectedBranch)
	}
	return true, nil
}

func isWorktreeClean(ctx context.Context, wtPath string) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", wtPath, "status", "--porcelain", "--untracked-files=normal").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git status: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return len(strings.TrimSpace(string(out))) == 0, nil
}
