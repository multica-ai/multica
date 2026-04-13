package repocache

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LocalWorktreeParams holds inputs for creating a worktree from a user's
// on-disk git repository. Unlike WorktreeParams (which sources from a bare
// clone in the daemon's cache), this path shares the git object store with
// the user's own checkout — including remotes, refs, and history.
type LocalWorktreeParams struct {
	LocalPath string // absolute path to the user's repo (can be a subdir or a worktree)
	WorkDir   string // parent directory where the new worktree lives (NEVER inside LocalPath)
	AgentName string
	TaskID    string
	// BaseRef selects the commit/branch the new worktree branches from. Empty
	// means HEAD (whatever the user last checked out). Callers that want a
	// specific branch should pass e.g. "refs/heads/main".
	BaseRef string
	// DirName is the directory name to use for the worktree under WorkDir.
	// Empty means derive from filepath.Base(LocalPath).
	DirName string
}

// CreateWorktreeFromLocal creates a git worktree rooted at WorkDir/<name>
// whose .git links back to LocalPath's git-common-dir. The user's working
// tree is never touched. A new branch is created from BaseRef (or HEAD).
//
// Concurrency: multiple calls with different TaskID on the same LocalPath are
// safe — git worktree admin is serialized by the repo-level mutex keyed on
// the resolved git-common-dir.
//
// This path deliberately refuses to place the worktree anywhere under
// LocalPath (`git worktree add` would allow it, but it's a foot-gun: users
// don't expect Multica to drop subdirectories into their tracked repo).
func (c *Cache) CreateWorktreeFromLocal(params LocalWorktreeParams) (*WorktreeResult, error) {
	if params.LocalPath == "" {
		return nil, fmt.Errorf("local path is required")
	}
	if params.WorkDir == "" {
		return nil, fmt.Errorf("work dir is required")
	}

	absLocal, err := filepath.Abs(params.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("resolve local path: %w", err)
	}
	absWorkDir, err := filepath.Abs(params.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve work dir: %w", err)
	}

	// Refuse to place the new worktree inside the user's repo. This prevents
	// accidentally polluting the user's tree with per-task worktree
	// directories that appear as untracked files.
	if strings.HasPrefix(absWorkDir+string(os.PathSeparator), absLocal+string(os.PathSeparator)) {
		return nil, fmt.Errorf("work dir %q must not be inside local repo %q", absWorkDir, absLocal)
	}

	commonDir, err := GitCommonDir(absLocal)
	if err != nil {
		return nil, fmt.Errorf("resolve git dir for %s: %w", absLocal, err)
	}

	// Serialize git worktree mutations on the user's repo. Two tasks targeting
	// the same repo concurrently would otherwise race on git's lockfiles.
	repoLock := c.lockForRepo(commonDir)
	repoLock.Lock()
	defer repoLock.Unlock()

	dirName := params.DirName
	if dirName == "" {
		dirName = sanitizeDirName(filepath.Base(absLocal))
	}
	if dirName == "" || dirName == "." || dirName == "/" {
		dirName = "repo"
	}
	worktreePath := filepath.Join(absWorkDir, dirName)

	// If the exact worktree already exists (reuse path), refresh it instead.
	if isGitWorktree(worktreePath) {
		branchName := fmt.Sprintf("agent/%s/%s", sanitizeName(params.AgentName), shortID(params.TaskID))
		actual, err := updateExistingWorktree(worktreePath, branchName, pickLocalBaseRef(absLocal, params.BaseRef))
		if err != nil {
			return nil, fmt.Errorf("refresh local worktree: %w", err)
		}
		for _, pattern := range []string{".agent_context", "CLAUDE.md", "AGENTS.md", ".claude", ".config/opencode"} {
			_ = excludeFromGit(worktreePath, pattern)
		}
		c.logger.Info("repo checkout (local): worktree refreshed",
			"local_path", absLocal, "path", worktreePath, "branch", actual)
		return &WorktreeResult{Path: worktreePath, BranchName: actual}, nil
	}

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return nil, fmt.Errorf("create work dir: %w", err)
	}

	branchName := fmt.Sprintf("agent/%s/%s", sanitizeName(params.AgentName), shortID(params.TaskID))
	baseRef := pickLocalBaseRef(absLocal, params.BaseRef)

	// `git -C <user-repo> worktree add -b <branch> <path> <baseRef>` creates a
	// new working tree under the daemon's workdir that shares the user's
	// object store and refs. The user's own checkout stays on whatever branch
	// they were on.
	actualBranch, err := createWorktreeWithRetry(absLocal, worktreePath, branchName, baseRef)
	if err != nil {
		return nil, fmt.Errorf("git worktree add (local): %w", err)
	}

	for _, pattern := range []string{".agent_context", "CLAUDE.md", "AGENTS.md", ".claude", ".config/opencode"} {
		_ = excludeFromGit(worktreePath, pattern)
	}

	c.logger.Info("repo checkout (local): worktree created",
		"local_path", absLocal, "path", worktreePath,
		"branch", actualBranch, "base", baseRef)

	return &WorktreeResult{Path: worktreePath, BranchName: actualBranch}, nil
}

// RemoveLocalWorktree deletes a worktree created by CreateWorktreeFromLocal.
// Safe to call after task completion/failure. The agent branch is kept so the
// user can inspect or merge it; only the working copy and the worktree admin
// entry are removed.
func (c *Cache) RemoveLocalWorktree(localPath, worktreePath string) error {
	if localPath == "" || worktreePath == "" {
		return nil
	}
	commonDir, err := GitCommonDir(localPath)
	if err != nil {
		return err
	}
	repoLock := c.lockForRepo(commonDir)
	repoLock.Lock()
	defer repoLock.Unlock()

	cmd := exec.Command("git", "-C", localPath, "worktree", "remove", "--force", worktreePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: best-effort `git worktree prune` + rm -rf so we don't leak
		// stale admin entries if the directory was already deleted.
		_ = exec.Command("git", "-C", localPath, "worktree", "prune").Run()
		_ = os.RemoveAll(worktreePath)
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// GitCommonDir resolves the absolute path to the shared .git dir (the
// "common dir") for a repo at path. Accepts regular repos, worktrees, and
// subdirectories within them.
func GitCommonDir(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", path)
	}
	cmd := exec.Command("git", "-C", path, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository at %s: %w", path, err)
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) {
		dir = filepath.Clean(filepath.Join(path, dir))
	}
	return dir, nil
}

// pickLocalBaseRef returns a ref usable as the startpoint for the agent
// branch. When the caller specifies BaseRef, use it. Otherwise fall back to
// HEAD — which mirrors the user's current checkout at worktree-add time.
func pickLocalBaseRef(localPath, preferred string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return "HEAD"
}

// createWorktreeWithRetry calls `git worktree add -b <branch> <path> <base>`,
// retrying with a timestamp-suffixed branch name if the initial call fails on
// branch-name collision. Mirrors the cache's remote-worktree retry behavior so
// concurrent agent tasks on the same local repo don't step on each other when
// two tasks happen to share a short-id prefix.
func createWorktreeWithRetry(gitRoot, worktreePath, branchName, baseRef string) (string, error) {
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree path already exists and is not a valid git worktree: %s", worktreePath)
	}
	err := runLocalWorktreeAdd(gitRoot, worktreePath, branchName, baseRef)
	if err != nil && isBranchCollisionError(err) {
		branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
		err = runLocalWorktreeAdd(gitRoot, worktreePath, branchName, baseRef)
	}
	if err != nil {
		return "", err
	}
	return branchName, nil
}

func runLocalWorktreeAdd(gitRoot, worktreePath, branchName, baseRef string) error {
	cmd := exec.Command("git", "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath, baseRef)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// sanitizeDirName turns a filesystem-unsafe name into a simple, filesystem-safe
// string. Unlike sanitizeName (which targets git branch names), this is more
// permissive (allows uppercase, underscores) so directory names stay readable.
func sanitizeDirName(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return ""
	}
	// Replace path separators and other noisy chars with '-'.
	bad := func(r rune) bool {
		switch r {
		case '/', '\\', '\x00', ':', '*', '?', '"', '<', '>', '|':
			return true
		}
		return false
	}
	var b strings.Builder
	for _, r := range s {
		if bad(r) {
			b.WriteRune('-')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
