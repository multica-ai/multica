package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// local_worktree.go — per-task worktree isolation for local_directory tasks (VWO-367).
//
// Background. A local_directory project_resource binds a task's WorkDir to a
// user's live checkout so the agent edits that tree in place (execenv.Prepare,
// LocalWorkDir). To keep two tasks from corrupting each other's sidecars and git
// index in that one shared tree, the daemon serialises every local_directory
// task on a whole-task path mutex (LocalPathLocker, keyed on the checkout path).
// For a fleet whose agents all target one checkout that mutex collapses their
// designed concurrency to strictly one task at a time.
//
// Isolation. When the local_directory resource opts in (Isolate), the daemon
// instead cuts a *per-task git worktree* off the same repository: the task gets
// its own working tree and its own git index under the task's envRoot, sharing
// only the repository's object store and refs. Two tasks on the same checkout no
// longer share a working tree, an index, or a sidecar directory, so the
// whole-task mutex is unnecessary — only the brief `git worktree add`/`remove`
// critical sections (which mutate shared .git metadata) need serialising, done
// here with a per-source-repo in-process lock (cross-process add/remove races
// are handled by git's own lockfiles; two-daemon safety is VWO-365's single
// owner lock).
//
// Ownership of the git lifecycle:
//   - create   : PrepareIsolatedLocalWorktree — `git worktree add -b
//     multica/worktree/<shortTaskID> <envRoot/workdir> HEAD`, off the checkout's
//     current HEAD. Sidecars are excluded per-worktree so `git add -A` can't
//     stage them.
//   - commit/rebase/push: the AGENT owns, in its worktree, on its per-task
//     branch. The daemon never commits; it provides the isolated tree. Same-
//     passage serialisation and the shared book/glossary flock are the
//     pub-workstream reducer's job (tools/passage-lock.py, tools/shared_lock.rb),
//     not the worktree's — distinct worktrees deliberately do not serialise.
//   - conflict : surfaced to the agent as an ordinary non-fast-forward / rebase
//     conflict in its own worktree; isolation turns a silent lost-update into a
//     visible conflict.
//   - cleanup  : Remove — `git worktree remove --force` + branch delete + prune.
//     Idempotent. On daemon crash (no deferred cleanup) the worktree dir is
//     GC'd with its envRoot and PruneOrphanLocalWorktrees reclaims the dangling
//     registry entry + branch.
//
// Sidecar containment. The daemon writes provider sidecars (.claude/skills/,
// .agent_context/, .multica/, the runtime brief) into WorkDir because providers
// discover them relative to the working directory. In the isolated case WorkDir
// is this per-task worktree, so those sidecars are contained by construction:
// (1) they live on a disposable per-task branch, never on main; (2) Remove runs
// `git worktree remove --force`, discarding every uncommitted/untracked sidecar;
// (3) the fleet stages named paths at gate-park (`git add <paths>`), never
// `git add -A`. We deliberately do NOT write to <repo>/.git/info/exclude: for a
// linked worktree git reads excludes from the COMMON gitdir, so an effective
// exclude would be a write into the user's shared repository state — exactly
// what isolation must avoid. This matches the existing github_repo worktree flow.

// worktreeBranchPrefix namespaces per-task isolation branches so
// PruneOrphanLocalWorktrees and operators can identify daemon-created worktrees.
const worktreeBranchPrefix = "multica/worktree/"

// repoLocks serialises worktree add/remove/prune per source repository within
// this process. The critical section is brief (a single git call), never the
// whole task, so it does not reintroduce the whole-task serialization this
// change removes.
var (
	repoLocksMu sync.Mutex
	repoLocks   = map[string]*sync.Mutex{}
)

func lockForRepo(repoRoot string) *sync.Mutex {
	repoLocksMu.Lock()
	defer repoLocksMu.Unlock()
	m, ok := repoLocks[repoRoot]
	if !ok {
		m = &sync.Mutex{}
		repoLocks[repoRoot] = m
	}
	return m
}

// IsolatedLocalWorktree is a per-task git worktree cut from a user's live
// local_directory checkout.
type IsolatedLocalWorktree struct {
	SourceRepo string // git toplevel of the user's checkout (the worktree's parent)
	WorkDir    string // envRoot/workdir — the isolated working tree
	Branch     string // multica/worktree/<shortTaskID>
}

// PrepareIsolatedLocalWorktree cuts an isolated worktree for taskID at workDir,
// branched off sourceDir's current HEAD. sourceDir is the user's checkout
// (local_directory local_path). Returns an error when sourceDir is not inside a
// git repository — isolation requires a repo to branch from; the caller should
// fall back to the in-place local_directory flow or fail the task with a clear
// message rather than silently running unisolated.
func PrepareIsolatedLocalWorktree(sourceDir, workDir, taskID string, logger *slog.Logger) (*IsolatedLocalWorktree, error) {
	gitRoot, ok := detectGitRepo(sourceDir)
	if !ok {
		return nil, fmt.Errorf("execenv: local_directory isolation requires a git repository, but %q is not inside one", sourceDir)
	}
	branch := worktreeBranchPrefix + shortID(taskID)

	lock := lockForRepo(gitRoot)
	lock.Lock()
	// Self-heal before adding ours: reclaim BOTH the registry entries and the
	// multica/worktree/* branches left behind by a prior daemon crash (envRoot
	// GC'd, no `git worktree remove` ran). This is what makes crash recovery
	// reachable from the normal task path — the fleet runs many tasks on one
	// repo, so orphans are reaped within one task cycle without any GC wiring.
	// It can never touch a live sibling: prune only drops entries whose dir is
	// missing, and reap skips branches still checked out in a worktree.
	reapOrphansLocked(gitRoot, logger)
	err := setupGitWorktree(gitRoot, workDir, branch, "HEAD")
	lock.Unlock()
	if err != nil {
		return nil, fmt.Errorf("execenv: create isolated worktree: %w", err)
	}

	if logger != nil {
		logger.Info("execenv: prepared isolated local worktree", "source", gitRoot, "workdir", workDir, "branch", branch)
	}
	return &IsolatedLocalWorktree{SourceRepo: gitRoot, WorkDir: workDir, Branch: branch}, nil
}

// Remove tears the worktree down: `git worktree remove --force`, delete the
// per-task branch, and prune dangling registry entries. Idempotent — calling it
// after the worktree is already gone (or twice) is a no-op, not an error. Safe
// to call on the deferred cleanup path AND from GC.
func (w *IsolatedLocalWorktree) Remove(logger *slog.Logger) {
	if w == nil || w.SourceRepo == "" {
		return
	}
	lock := lockForRepo(w.SourceRepo)
	lock.Lock()
	defer lock.Unlock()
	removeGitWorktree(w.SourceRepo, w.WorkDir, w.Branch, orDiscardLogger(logger))
	pruneWorktrees(w.SourceRepo)
}

// PruneOrphanLocalWorktrees reclaims isolation worktrees whose working
// directory has already been removed (the daemon-crash path: the envRoot — and
// with it envRoot/workdir — is GC'd, but no `git worktree remove` ran, leaving a
// dangling entry in <sourceRepo>/.git/worktrees and a multica/worktree/* branch).
// `git worktree prune` drops the dangling registry entries; then any
// multica/worktree/* branch no longer backing a live worktree is deleted. Safe
// to run repeatedly and safe against live worktrees (prune only removes entries
// whose directory is missing; branches still checked out cannot be deleted).
func PruneOrphanLocalWorktrees(sourceRepo string, logger *slog.Logger) error {
	if sourceRepo == "" {
		return nil
	}
	gitRoot, ok := detectGitRepo(sourceRepo)
	if !ok {
		return nil
	}
	lock := lockForRepo(gitRoot)
	lock.Lock()
	defer lock.Unlock()
	reapOrphansLocked(gitRoot, logger)
	return nil
}

// reapOrphansLocked prunes dangling worktree registry entries and deletes any
// multica/worktree/* branch no longer backing a live worktree. Caller MUST hold
// lockForRepo(gitRoot). Safe against live worktrees: `git worktree prune` only
// drops entries whose directory is missing, and a branch still checked out in a
// worktree is skipped (and git refuses to delete it anyway).
func reapOrphansLocked(gitRoot string, logger *slog.Logger) {
	pruneWorktrees(gitRoot)
	live := liveWorktreeBranches(gitRoot)
	for _, br := range listBranchesWithPrefix(gitRoot, worktreeBranchPrefix) {
		if live[br] {
			continue
		}
		cmd := exec.Command("git", "-C", gitRoot, "branch", "-D", br)
		if out, err := cmd.CombinedOutput(); err != nil && logger != nil {
			logger.Warn("execenv: prune orphan worktree branch failed", "branch", br, "output", strings.TrimSpace(string(out)), "error", err)
		}
	}
}

// pruneWorktrees runs `git worktree prune` on repoRoot. Best-effort.
func pruneWorktrees(repoRoot string) {
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "prune")
	_ = cmd.Run()
}

// liveWorktreeBranches returns the set of branch names currently checked out in
// a worktree of repoRoot (so pruning never deletes an in-use branch).
func liveWorktreeBranches(repoRoot string) map[string]bool {
	out, err := exec.Command("git", "-C", repoRoot, "worktree", "list", "--porcelain").Output()
	set := map[string]bool{}
	if err != nil {
		return set
	}
	for _, line := range strings.Split(string(out), "\n") {
		if b, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
			set[strings.TrimSpace(b)] = true
		}
	}
	return set
}

// listBranchesWithPrefix returns local branches under refs/heads/<prefix>.
func listBranchesWithPrefix(repoRoot, prefix string) []string {
	out, err := exec.Command("git", "-C", repoRoot, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+prefix).Output()
	if err != nil {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			branches = append(branches, s)
		}
	}
	return branches
}

// orDiscardLogger returns a non-nil logger so removeGitWorktree (which always
// logs) never nil-panics when the caller passed nil.
func orDiscardLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// isolatedWorkDir is the path a task's isolated worktree lives at, under envRoot.
func isolatedWorkDir(envRoot string) string {
	return filepath.Join(envRoot, "workdir")
}
