// Package repocache manages bare git clone caches for workspace repositories.
// The daemon uses these caches as the source for creating per-task worktrees.
package repocache

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// gitEnv returns an environment for git subprocesses that contact remotes.
// It passes the full daemon environment so credential helpers (e.g. gh) can
// locate their config, and disables TTY prompting so auth failures produce
// clear errors instead of blocking on a non-existent terminal.
func gitEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
}

// RepoInfo describes a repository to cache.
type RepoInfo struct {
	URL         string
	Description string
}

// CachedRepo describes a cached bare clone ready for worktree creation.
type CachedRepo struct {
	URL         string // remote URL
	Description string // human-readable description
	LocalPath   string // absolute path to the bare clone
}

// Cache manages bare git clones for workspace repositories.
type Cache struct {
	root   string // base directory for all caches (e.g. ~/multica_workspaces/.repos)
	logger *slog.Logger
	// repoLocks maps bare repo path → dedicated mutex. Any mutating operation
	// on a given bare repo (clone, fetch, worktree add, ref update) must
	// hold its lock — git's own lockfiles (packed-refs.lock, config.lock,
	// worktree admin dirs) don't tolerate parallel mutations on the same
	// repo. Separate repos are independent and run concurrently.
	repoLocks sync.Map // barePath -> *sync.Mutex
}

// New creates a new repo cache rooted at the given directory.
func New(root string, logger *slog.Logger) *Cache {
	return &Cache{root: root, logger: logger}
}

// lockForRepo returns the mutex dedicated to the given bare repo path. See
// the Cache.repoLocks field comment for semantics.
func (c *Cache) lockForRepo(barePath string) *sync.Mutex {
	if l, ok := c.repoLocks.Load(barePath); ok {
		return l.(*sync.Mutex)
	}
	newLock := &sync.Mutex{}
	actual, _ := c.repoLocks.LoadOrStore(barePath, newLock)
	return actual.(*sync.Mutex)
}

// Sync ensures all repos for a workspace are cloned (or fetched if already cached).
// Repos no longer in the list are left in place (cheap to keep, avoids re-cloning
// if a repo is temporarily removed and re-added).
//
// Per-repo mutation serializes against CreateWorktree on the same bare path
// via lockForRepo. Different repos run sequentially within a single Sync call
// but concurrent Sync calls (different workspaces, or the same workspace
// re-synced while checkouts are running) do not block each other.
func (c *Cache) Sync(workspaceID string, repos []RepoInfo) error {
	wsDir := filepath.Join(c.root, workspaceID)
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return fmt.Errorf("create workspace cache dir: %w", err)
	}

	var firstErr error
	for _, repo := range repos {
		if repo.URL == "" {
			continue
		}
		barePath := filepath.Join(wsDir, bareDirName(repo.URL))

		repoLock := c.lockForRepo(barePath)
		repoLock.Lock()
		if isBareRepo(barePath) {
			// Already cached — fetch latest.
			c.logger.Info("repo cache: fetching", "url", repo.URL, "path", barePath)
			if err := gitFetch(barePath); err != nil {
				c.logger.Warn("repo cache: fetch failed", "url", repo.URL, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		} else {
			// Not cached — bare clone.
			c.logger.Info("repo cache: cloning", "url", repo.URL, "path", barePath)
			if err := gitCloneBare(repo.URL, barePath); err != nil {
				c.logger.Error("repo cache: clone failed", "url", repo.URL, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		repoLock.Unlock()
	}
	return firstErr
}

// Lookup returns the local bare clone path for a repo URL within a workspace.
// Returns "" if not cached.
func (c *Cache) Lookup(workspaceID, url string) string {
	barePath := filepath.Join(c.root, workspaceID, bareDirName(url))
	if isBareRepo(barePath) {
		return barePath
	}
	return ""
}

// Fetch runs `git fetch origin` on a cached bare clone to get latest refs.
func (c *Cache) Fetch(barePath string) error {
	return gitFetch(barePath)
}

// bareDirName derives a directory name from a repo URL.
// e.g. "https://github.com/org/my-repo.git" → "my-repo.git"
func bareDirName(url string) string {
	url = strings.TrimRight(url, "/")
	name := url
	if i := strings.LastIndex(url, "/"); i >= 0 {
		name = url[i+1:]
	}
	// Handle SSH-style "host:org/repo".
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
		if j := strings.LastIndex(name, "/"); j >= 0 {
			name = name[j+1:]
		}
	}
	if !strings.HasSuffix(name, ".git") {
		name += ".git"
	}
	if name == ".git" {
		name = "repo.git"
	}
	return name
}

// isBareRepo checks if a path looks like a bare git repository.
func isBareRepo(path string) bool {
	// A bare repo has a HEAD file at the root.
	_, err := os.Stat(filepath.Join(path, "HEAD"))
	return err == nil
}

// modernFetchRefspec is the remote-tracking refspec that keeps fetched heads
// out of the bare repo's refs/heads/* namespace. That namespace is reserved
// for per-task worktree branches created by `git worktree add -b ...`, and any
// mirror-style fetch that targets refs/heads/* can collide with those locked
// refs and abort the entire fetch.
const modernFetchRefspec = "+refs/heads/*:refs/remotes/origin/*"

func gitCloneBare(url, dest string) error {
	cmd := exec.Command("git", "clone", "--bare", url, dest)
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		// Clean up partial clone.
		os.RemoveAll(dest)
		return fmt.Errorf("git clone --bare: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// `git clone --bare` populates refs/heads/* as a snapshot and defaults to
	// a mirror-style fetch refspec. Convert the bare repo to the standard
	// remote-tracking layout immediately so subsequent fetches write to
	// refs/remotes/origin/* and can't conflict with worktree-locked heads.
	if err := ensureRemoteTrackingLayout(dest); err != nil {
		os.RemoveAll(dest)
		return fmt.Errorf("configure fetch refspec: %w", err)
	}
	return nil
}

// gitFetch runs `git fetch origin` on a bare cache, migrating its fetch
// refspec to the remote-tracking layout first if it's still using the legacy
// mirror-style layout from an older version of this package. After a
// successful fetch it also refreshes refs/remotes/origin/HEAD so a remote
// default-branch change (e.g. master→main on an existing repo) actually
// takes effect in getRemoteDefaultBranch. Plain `git fetch origin` never
// touches that symref on its own, so without this call an existing cache
// would keep basing new worktrees on the original default branch forever
// after the remote flipped.
func gitFetch(barePath string) error {
	if err := ensureRemoteTrackingLayout(barePath); err != nil {
		return fmt.Errorf("ensure refspec: %w", err)
	}
	if err := runGitFetch(barePath); err != nil {
		return err
	}
	// Refresh refs/remotes/origin/HEAD after every successful fetch.
	// set-head --auto is lightweight (a single ls-remote HEAD round-trip)
	// and non-fatal: if it fails we still have the step 2-5 fallbacks in
	// getRemoteDefaultBranch, but the modern-cache default-branch-change
	// path (the only path that can't be recovered any other way) relies
	// on this call.
	cmd := exec.Command("git", "-C", barePath, "remote", "set-head", "origin", "--auto")
	cmd.Env = gitEnv()
	_ = cmd.Run()
	return nil
}

// runGitFetch is the raw `git fetch origin` wrapper. Callers should go through
// gitFetch, which migrates legacy caches first.
func runGitFetch(barePath string) error {
	cmd := exec.Command("git", "-C", barePath, "fetch", "origin")
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ensureRemoteTrackingLayout upgrades a bare repo from the legacy mirror
// refspec (+refs/heads/*:refs/heads/*) to the standard remote-tracking refspec
// (+refs/heads/*:refs/remotes/origin/*). It's idempotent: on an already-modern
// cache it's a single `git config --get` call. On legacy caches it rewrites
// the refspec, performs a backfill fetch to populate refs/remotes/origin/*,
// and runs `git remote set-head origin --auto` so getRemoteDefaultBranch can
// resolve the remote's default branch.
func ensureRemoteTrackingLayout(barePath string) error {
	cur, err := readFetchRefspec(barePath)
	if err != nil {
		return err
	}
	if cur == modernFetchRefspec || cur == strings.TrimPrefix(modernFetchRefspec, "+") {
		return nil // already modern
	}
	if err := setFetchRefspec(barePath, modernFetchRefspec); err != nil {
		return err
	}
	// Backfill refs/remotes/origin/* by fetching with the new refspec. This
	// writes to the origin/* namespace, so even worktree-locked refs/heads/*
	// branches can't collide.
	if err := runGitFetch(barePath); err != nil {
		return fmt.Errorf("backfill fetch after refspec migration: %w", err)
	}
	// Set refs/remotes/origin/HEAD so getRemoteDefaultBranch can read it.
	// Non-fatal: if this fails we fall back to origin/main, origin/master.
	cmd := exec.Command("git", "-C", barePath, "remote", "set-head", "origin", "--auto")
	cmd.Env = gitEnv()
	_ = cmd.Run()
	return nil
}

// readFetchRefspec returns the current remote.origin.fetch config value, or
// the empty string if it's not set. Distinguishes "missing" (exit 1) from
// real git errors.
func readFetchRefspec(barePath string) (string, error) {
	out, err := exec.Command("git", "-C", barePath, "config", "--get", "remote.origin.fetch").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "", nil // key missing, not an error
		}
		return "", fmt.Errorf("read remote.origin.fetch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func setFetchRefspec(barePath, refspec string) error {
	out, err := exec.Command("git", "-C", barePath, "config", "remote.origin.fetch", refspec).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set remote.origin.fetch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// WorktreeParams holds inputs for creating a worktree from a cached bare clone.
type WorktreeParams struct {
	WorkspaceID string // workspace that owns the repo
	RepoURL     string // remote URL to look up in the cache
	WorkDir     string // parent directory for the worktree (e.g. task workdir)
	AgentName   string // for branch naming
	TaskID      string // for branch naming uniqueness
}

// WorktreeResult describes a successfully created worktree.
type WorktreeResult struct {
	Path       string `json:"path"`        // absolute path to the worktree
	BranchName string `json:"branch_name"` // git branch created for this worktree
}

// CreateWorktree looks up the bare cache for a repo, fetches latest, and creates
// a git worktree in the agent's working directory. If a worktree already exists
// at the target path (reused environment), it updates the existing worktree to
// the latest remote default branch instead of failing.
func (c *Cache) CreateWorktree(params WorktreeParams) (*WorktreeResult, error) {
	barePath := c.Lookup(params.WorkspaceID, params.RepoURL)
	if barePath == "" {
		return nil, fmt.Errorf("repo not found in cache: %s (workspace: %s)", params.RepoURL, params.WorkspaceID)
	}

	// Serialize concurrent CreateWorktree calls on the same bare repo. Git's
	// own lockfiles (packed-refs.lock, config.lock, worktree admin dirs)
	// can't tolerate parallel fetch + worktree mutations on the same repo.
	repoLock := c.lockForRepo(barePath)
	repoLock.Lock()
	defer repoLock.Unlock()

	// Fetch latest from origin. This also migrates the bare cache's refspec
	// to the modern remote-tracking layout on first run, so subsequent fetches
	// never collide with the refs/heads/agent/* branches that worktree creation
	// locks in this same bare repo.
	if err := gitFetch(barePath); err != nil {
		// Non-fatal: preserve cached state and continue, but make the warning
		// loud enough that it's findable in the daemon log. The agent will
		// receive an older snapshot than the remote head.
		c.logger.Warn("repo checkout: fetch failed, agent will see possibly stale code",
			"url", params.RepoURL,
			"error", err,
		)
	}

	// Determine the default branch to base the worktree on. getRemoteDefaultBranch
	// walks origin/HEAD → origin/main, origin/master → bare-HEAD hint into
	// origin/<same> → single-entry scan of origin/* → bare HEAD (only if
	// origin/* is empty). Reaching "" here means the cache is in a state we
	// refuse to guess from (no origin/HEAD, no main/master, bare HEAD doesn't
	// match any origin/* entry, and origin/* has multiple candidates).
	baseRef := getRemoteDefaultBranch(barePath)
	if baseRef == "" {
		return nil, fmt.Errorf("cannot resolve default branch for %s: bare cache at %s has no usable refs (origin/* is empty or ambiguous and bare HEAD has no match). The cache may be corrupted; delete it and retry", params.RepoURL, barePath)
	}

	// Build branch name: agent/{sanitized-name}/{short-task-id}
	branchName := fmt.Sprintf("agent/%s/%s", sanitizeName(params.AgentName), shortID(params.TaskID))

	// Derive directory name from repo URL.
	dirName := repoNameFromURL(params.RepoURL)
	worktreePath := filepath.Join(params.WorkDir, dirName)

	// If worktree already exists (reused environment from a prior task),
	// update it to the latest remote code instead of creating a new one.
	if isGitWorktree(worktreePath) {
		actualBranch, err := updateExistingWorktree(worktreePath, branchName, baseRef)
		if err != nil {
			return nil, fmt.Errorf("update existing worktree: %w", err)
		}

		for _, pattern := range []string{".agent_context", "CLAUDE.md", "AGENTS.md", ".claude", ".config/opencode"} {
			_ = excludeFromGit(worktreePath, pattern)
		}

		c.logger.Info("repo checkout: existing worktree updated",
			"url", params.RepoURL,
			"path", worktreePath,
			"branch", actualBranch,
			"base", baseRef,
		)

		return &WorktreeResult{
			Path:       worktreePath,
			BranchName: actualBranch,
		}, nil
	}

	// Create a new worktree. createWorktree may rename the branch to avoid
	// collisions with stale per-task refs left over from previous runs.
	actualBranch, err := createWorktree(barePath, worktreePath, branchName, baseRef)
	if err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}

	// Exclude agent context files from git tracking.
	for _, pattern := range []string{".agent_context", "CLAUDE.md", "AGENTS.md", ".claude", ".config/opencode"} {
		_ = excludeFromGit(worktreePath, pattern)
	}

	c.logger.Info("repo checkout: worktree created",
		"url", params.RepoURL,
		"path", worktreePath,
		"branch", actualBranch,
		"base", baseRef,
	)

	return &WorktreeResult{
		Path:       worktreePath,
		BranchName: actualBranch,
	}, nil
}

// createWorktree creates a git worktree at the given path with a new branch.
// Returns the actual branch name used — which may differ from the requested
// branchName if a collision was resolved by appending a timestamp suffix.
func createWorktree(gitRoot, worktreePath, branchName, baseRef string) (string, error) {
	// Pre-check: if the worktree path already exists we would get a confusing
	// "already exists" error from `git worktree add` — which used to be
	// misclassified as a branch collision, causing the retry to leak branches
	// into the bare repo. Fail cleanly here instead. The caller is expected
	// to route reused workdirs through updateExistingWorktree via isGitWorktree.
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree path already exists and is not a valid git worktree: %s", worktreePath)
	}

	err := runWorktreeAdd(gitRoot, worktreePath, branchName, baseRef)
	if err != nil && isBranchCollisionError(err) {
		// Branch name collision: append timestamp and retry once.
		branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
		err = runWorktreeAdd(gitRoot, worktreePath, branchName, baseRef)
	}
	if err != nil {
		return "", err
	}
	return branchName, nil
}

func runWorktreeAdd(gitRoot, worktreePath, branchName, baseRef string) error {
	cmd := exec.Command("git", "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath, baseRef)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// isBranchCollisionError returns true if err is specifically about a branch
// name already existing. Git's other "already exists" messages (notably path
// collisions from `git worktree add`) must NOT be treated as branch
// collisions, or the retry-with-timestamp logic will leak branches while
// still failing on the original path collision.
func isBranchCollisionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Git's message is "fatal: a branch named 'X' already exists".
	return strings.Contains(msg, "a branch named")
}

// isGitWorktree checks if a path is an existing git worktree.
// Worktrees have a .git *file* (not directory) that points to the main repo.
func isGitWorktree(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && !info.IsDir()
}

// updateExistingWorktree resets the worktree to a clean state and checks out a
// new branch from the default branch. The caller is responsible for fetching
// the bare cache beforehand (worktrees share the same object store).
// Returns the actual branch name used (may differ from input on collision).
func updateExistingWorktree(worktreePath, branchName, baseRef string) (string, error) {
	// Discard any leftover uncommitted changes from the previous task.
	resetCmd := exec.Command("git", "-C", worktreePath, "reset", "--hard")
	if out, err := resetCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git reset --hard: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Clean untracked files (e.g. build artifacts from previous task).
	cleanCmd := exec.Command("git", "-C", worktreePath, "clean", "-fd")
	if out, err := cleanCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clean -fd: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Create a new branch from the resolved default-branch ref and switch to
	// it. baseRef is a ref path returned by getRemoteDefaultBranch — usually
	// "refs/remotes/origin/<branch>" but may be "refs/heads/<branch>" on a
	// legacy/migration-pending cache. Either form is valid as a checkout
	// startpoint.
	checkoutCmd := exec.Command("git", "-C", worktreePath, "checkout", "-b", branchName, baseRef)
	out, err := checkoutCmd.CombinedOutput()
	if err == nil {
		return branchName, nil
	}
	wrapped := fmt.Errorf("git checkout -b: %s: %w", strings.TrimSpace(string(out)), err)
	if !isBranchCollisionError(wrapped) {
		return "", wrapped
	}
	// Branch name collision: append timestamp and retry once.
	branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
	checkoutCmd = exec.Command("git", "-C", worktreePath, "checkout", "-b", branchName, baseRef)
	if out2, err2 := checkoutCmd.CombinedOutput(); err2 != nil {
		return "", fmt.Errorf("git checkout -b (retry): %s: %w", strings.TrimSpace(string(out2)), err2)
	}
	return branchName, nil
}

// getRemoteDefaultBranch returns a ref path (e.g. "refs/remotes/origin/main")
// that points at the remote's default branch in a bare cache. The return value
// is usable directly as a `git worktree add` / `git checkout -b` startpoint.
//
// Resolution order:
//  1. refs/remotes/origin/HEAD (verified; set by `git remote set-head origin --auto`)
//  2. refs/remotes/origin/main, refs/remotes/origin/master (common defaults)
//  3. The bare repo's own HEAD mapped into refs/remotes/origin/<same name> —
//     `git clone --bare` sets HEAD to the remote's default, so this is a
//     reliable hint for custom default branches (trunk, develop, …) when
//     `git remote set-head --auto` failed to populate refs/remotes/origin/HEAD.
//  4. Scan refs/remotes/origin/* — returns a result ONLY when exactly one
//     non-HEAD ref exists. Multiple refs cannot be disambiguated from refname
//     order alone (git for-each-ref sorts alphabetically), so we refuse to
//     guess; returning a wrong default would silently base new agent work on
//     an arbitrary feature branch.
//  5. Legacy last-resort: the bare repo's own HEAD as a plain refs/heads/*
//     ref, for caches that haven't populated refs/remotes/origin/* at all
//     yet (e.g. a migration-pending cache whose backfill fetch failed).
//     Gated on refs/remotes/origin/* being completely empty so we don't fall
//     back to a stale snapshot when the cache has real remote-tracking refs
//     but we just can't pick between them.
//
// Returns "" only when none of the above resolve — which the caller treats
// as a hard error with a clear "cache has no usable refs" message.
func getRemoteDefaultBranch(barePath string) string {
	// 1) Primary: refs/remotes/origin/HEAD set by `git remote set-head
	//    origin --auto` during ensureRemoteTrackingLayout. Verify the
	//    target actually exists — a partial set-head or a manually-broken
	//    repo can leave a symref pointing at a deleted ref, and returning
	//    it here would later fail in `git worktree add` with a confusing
	//    "invalid reference" error.
	if out, err := exec.Command("git", "-C", barePath, "symbolic-ref", "refs/remotes/origin/HEAD").Output(); err == nil {
		ref := strings.TrimSpace(string(out))
		if ref != "" {
			if err := exec.Command("git", "-C", barePath, "rev-parse", "--verify", ref).Run(); err == nil {
				return ref
			}
		}
	}
	// 2) Common default branch names under the origin namespace.
	for _, candidate := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master"} {
		if err := exec.Command("git", "-C", barePath, "rev-parse", "--verify", candidate).Run(); err == nil {
			return candidate
		}
	}
	// 3) Use the bare repo's own HEAD as a hint. `git clone --bare` sets HEAD
	//    to the remote's default branch, so this reliably identifies custom
	//    default branch names (trunk, develop, ...) when set-head --auto
	//    didn't populate refs/remotes/origin/HEAD. We only return when the
	//    matching origin/<name> exists, so we still pick up up-to-date code
	//    rather than a stale local head.
	bareRef := bareHeadBranch(barePath)
	if bareRef != "" {
		originRef := "refs/remotes/origin/" + strings.TrimPrefix(bareRef, "refs/heads/")
		if err := exec.Command("git", "-C", barePath, "rev-parse", "--verify", originRef).Run(); err == nil {
			return originRef
		}
	}
	// 4) Scan refs/remotes/origin/* — return a result ONLY when there's
	//    exactly one non-HEAD candidate. Multiple candidates cannot be
	//    disambiguated from refname order alone; returning the alphabetically-
	//    first entry would silently base new agent work on a feature branch
	//    instead of the real default. Count entries here so step 5 can tell
	//    "legacy empty" apart from "ambiguous".
	originCount := 0
	var singleton string
	if out, err := exec.Command("git", "-C", barePath, "for-each-ref", "--format=%(refname)", "refs/remotes/origin/").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line == "refs/remotes/origin/HEAD" {
				continue
			}
			originCount++
			if singleton == "" {
				singleton = line
			}
		}
		if originCount == 1 {
			return singleton
		}
	}
	// 5) Last-resort fallback: legacy / migration-pending caches still have
	//    refs/heads/* and a bare HEAD from the mirror-style layout. Gate this
	//    on refs/remotes/origin/* being completely empty — if origin/* has
	//    multiple refs but none match bare HEAD, the cache is in an
	//    ambiguous state and returning the local head would mask the
	//    problem with a stale snapshot. Let the caller fail loudly instead.
	if originCount == 0 && bareRef != "" {
		return bareRef
	}
	return ""
}

// bareHeadBranch returns the bare repo's local HEAD ref (e.g.
// "refs/heads/main") if HEAD is a symbolic ref to an existing branch.
// Returns "" if HEAD is detached, missing, or points at a non-existent ref.
//
// Only used by getRemoteDefaultBranch as a last-resort fallback for caches
// that haven't successfully populated refs/remotes/origin/* yet. Healthy
// modern caches should never reach this path because origin/* resolution
// succeeds first.
func bareHeadBranch(barePath string) string {
	out, err := exec.Command("git", "-C", barePath, "symbolic-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(out))
	if ref == "" {
		return ""
	}
	if err := exec.Command("git", "-C", barePath, "rev-parse", "--verify", ref).Run(); err != nil {
		return ""
	}
	return ref
}

// excludeFromGit adds a pattern to the worktree's .git/info/exclude file.
func excludeFromGit(worktreePath, pattern string) error {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-dir")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("resolve git dir: %w", err)
	}

	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}

	excludePath := filepath.Join(gitDir, "info", "exclude")

	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("create info dir: %w", err)
	}

	existing, _ := os.ReadFile(excludePath)
	if strings.Contains(string(existing), pattern) {
		return nil
	}

	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open exclude file: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "\n%s\n", pattern); err != nil {
		return fmt.Errorf("write exclude pattern: %w", err)
	}
	return nil
}

// repoNameFromURL extracts a short directory name from a git remote URL.
// e.g. "https://github.com/org/my-repo.git" → "my-repo"
func repoNameFromURL(url string) string {
	url = strings.TrimRight(url, "/")
	url = strings.TrimSuffix(url, ".git")

	if i := strings.LastIndex(url, "/"); i >= 0 {
		url = url[i+1:]
	}
	if i := strings.LastIndex(url, ":"); i >= 0 {
		url = url[i+1:]
		if j := strings.LastIndex(url, "/"); j >= 0 {
			url = url[j+1:]
		}
	}

	name := strings.TrimSpace(url)
	if name == "" {
		return "repo"
	}
	return name
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeName produces a git-branch-safe name from a human-readable string.
func sanitizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 30 {
		s = s[:30]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		s = "agent"
	}
	return s
}

// shortID returns the first 8 characters of a UUID string (dashes stripped).
func shortID(uuid string) string {
	s := strings.ReplaceAll(uuid, "-", "")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
