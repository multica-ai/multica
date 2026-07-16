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
// and is a git worktree). baseRef, when non-empty, is the commit/branch/tag the
// worktree branch is created from; empty falls back to HEAD so the agent starts
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
	if issueID == "" {
		// Without an issue id the path collapses to the daemon-level dir and
		// every such task would collide. Real agent tasks always carry an
		// issue id; fail closed rather than write into a shared path.
		return "", "", errors.New("local_directory worktree: issueID is required")
	}
	wtPath = issueWorktreePath(d.cfg.WorkspacesRoot, d.cfg.DaemonID, issueID)
	branch = issueWorktreeBranch(issueID)

	if isGitWorktreeDir(wtPath) {
		return wtPath, branch, nil
	}

	// Serialise git mutations on the same repository. Different-issue tasks
	// hold independent issue locks, so without this two of them could race
	// `git worktree add` on the user's repo and contend on git's lockfiles.
	// Brief and scoped to the mutation only — task execution still parallelises.
	repoKey, relErr := resolveRealPath(localRepo)
	if relErr != nil {
		return "", "", fmt.Errorf("local_directory worktree: resolve repo path: %w", relErr)
	}
	gitRelease, err := d.repoGitLocks.Acquire(ctx, repoKey, "ensure-worktree:"+issueID, nil)
	if err != nil {
		return "", "", fmt.Errorf("local_directory worktree: repo lock: %w", err)
	}
	defer gitRelease()

	if baseRef == "" {
		baseRef = "HEAD"
	}

	// Fresh branch from the base ref is the common path for a new issue.
	if err := gitWorktreeAddBranch(ctx, localRepo, wtPath, branch, baseRef); err == nil {
		return wtPath, branch, nil
	} else if !isBranchCollisionError(err) {
		return "", "", fmt.Errorf("local_directory worktree: %w", err)
	}

	// Branch already exists (GC removed the directory but not the branch, or
	// the user created it out of band). Check the existing branch out into the
	// new worktree so the issue's accumulated work is recovered.
	if err := gitWorktreeCheckoutExisting(ctx, localRepo, wtPath, branch); err != nil {
		return "", "", fmt.Errorf("local_directory worktree (reuse branch %q): %w", branch, err)
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
	repoKey, err := resolveRealPath(localRepo)
	if err != nil {
		repoKey = localRepo
	}
	release, err := d.repoGitLocks.Acquire(ctx, repoKey, "gc-remove-worktree", nil)
	if err != nil {
		return fmt.Errorf("local_directory worktree: repo lock: %w", err)
	}
	defer release()
	if out, err := exec.CommandContext(ctx, "git", "-C", localRepo, "worktree", "remove", "--force", "--", wtPath).CombinedOutput(); err != nil {
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

// isBranchCollisionError reports whether err is git's "a branch named X already
// exists" message. Mirrors repocache.isBranchCollisionError; duplicated here
// because that helper is package-private and the daemon package must not depend
// on repocache internals.
func isBranchCollisionError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "a branch named")
}

// isGitWorktreeDir reports whether path is an existing git worktree (a .git
// file, not directory, sits at its root). Mirrors repocache.isGitWorktree.
func isGitWorktreeDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && !info.IsDir()
}

// wantsWorktree reports whether a local_directory assignment should execute in
// a per-issue worktree rather than in place. Worktree mode additionally
// requires the source path to be a git worktree; a non-git path degrades to
// in_place so the task still runs. The lock gate and the runTask workdir wiring
// must both consult this so the lock key, the on-disk worktree, and the agent's
// working directory all agree on the strategy.
func wantsWorktree(ctx context.Context, a *localDirectoryAssignment) bool {
	if a == nil {
		return false
	}
	return a.Mode == localDirectoryModeWorktree && isGitWorkTree(ctx, a.AbsPath)
}

// resolvedLocalWorkDir returns the directory the agent should run in for a
// local_directory assignment: the per-issue worktree path for worktree mode,
// the user's own path otherwise. Mirrors the decision acquireLocalDirectoryLock
// IfNeeded already acted on (it created the worktree before runTask runs).
func resolvedLocalWorkDir(ctx context.Context, a *localDirectoryAssignment, workspacesRoot, daemonID string) string {
	if wantsWorktree(ctx, a) {
		return issueWorktreePath(workspacesRoot, daemonID, a.IssueID)
	}
	return a.AbsPath
}
