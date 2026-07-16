// Package repocache manages bare git clone caches for workspace repositories.
// The daemon uses these caches as the source for creating per-task worktrees.
package repocache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// gitEnv is deliberately independent of the daemon owner's environment.
// Repository broker commands must not inherit credentials, helpers, config,
// hooks, filters, URL rewrites, remote helpers, or interactive prompt state.
func gitEnv() []string {
	nullDevice := "/dev/null"
	if runtime.GOOS == "windows" {
		nullDevice = "NUL"
	}
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=" + nullDevice,
		"GIT_CONFIG_GLOBAL=" + nullDevice,
		"GIT_ALLOW_PROTOCOL=file:http:https:git",
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=core.hooksPath",
		"GIT_CONFIG_VALUE_1=" + nullDevice,
		"GIT_CONFIG_KEY_2=protocol.ext.allow",
		"GIT_CONFIG_VALUE_2=never",
		"LC_ALL=C",
		"LANG=C",
	}
}

var agentGitExcludePatterns = []string{
	".agent_context",
	"CLAUDE.md",
	"AGENTS.md",
	".claude",
	".opencode",
	".deveco",
	"CODEBUDDY.md",
	".codebuddy",
}

const repoCacheGitTimeout = 10 * time.Minute

// GitBrokerOptions configures the only executable and environment permitted
// for repository-cache Git operations.
type GitBrokerOptions struct {
	Executable string
	Timeout    time.Duration
}

type gitBroker struct {
	executable string
	env        []string
	timeout    time.Duration
}

func newGitBroker(options GitBrokerOptions) (*gitBroker, error) {
	if !filepath.IsAbs(options.Executable) {
		return nil, fmt.Errorf("git executable must be absolute: %q", options.Executable)
	}
	resolved, err := filepath.EvalSymlinks(options.Executable)
	if err != nil {
		return nil, fmt.Errorf("resolve git executable: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat git executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return nil, fmt.Errorf("git executable is not an executable regular file: %s", resolved)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("git executable is group/world writable: %s", resolved)
	}
	if err := validateTrustedExecutablePath(resolved); err != nil {
		return nil, err
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = repoCacheGitTimeout
	}
	return &gitBroker{executable: resolved, env: gitEnv(), timeout: timeout}, nil
}

func defaultGitBroker() (*gitBroker, error) {
	candidates := []string{"/usr/bin/git", "/bin/git"}
	if runtime.GOOS == "windows" {
		candidates = []string{`C:\\Program Files\\Git\\cmd\\git.exe`}
	}
	for _, candidate := range candidates {
		broker, err := newGitBroker(GitBrokerOptions{Executable: candidate})
		if err == nil {
			return broker, nil
		}
	}
	return nil, fmt.Errorf("no trusted absolute Git executable found")
}

func (g *gitBroker) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, g.executable, args...)
	cmd.Env = append([]string(nil), g.env...)
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func (g *gitBroker) withTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = g.timeout
	}
	return context.WithTimeout(parent, timeout)
}

func (g *gitBroker) combinedOutput(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := g.withTimeout(ctx, timeout)
	defer cancel()
	out, err := g.command(ctx, args...).CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func (g *gitBroker) combinedOutputInWorktree(ctx context.Context, timeout time.Duration, handle *worktreeHandle, args ...string) ([]byte, error) {
	ctx, cancel := g.withTimeout(ctx, timeout)
	defer cancel()
	cmd, err := g.worktreeCommand(ctx, handle, args...)
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func (g *gitBroker) combinedOutputInBareCache(ctx context.Context, timeout time.Duration, handle *bareCacheHandle, args ...string) ([]byte, error) {
	ctx, cancel := g.withTimeout(ctx, timeout)
	defer cancel()
	cmd, err := g.bareCacheCommand(ctx, handle, args...)
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if identityErr := handle.RecheckPath(); identityErr != nil {
		return out, identityErr
	}
	return out, err
}

func (g *gitBroker) outputInBareCache(ctx context.Context, timeout time.Duration, handle *bareCacheHandle, args ...string) ([]byte, error) {
	ctx, cancel := g.withTimeout(ctx, timeout)
	defer cancel()
	cmd, err := g.bareCacheCommand(ctx, handle, args...)
	if err != nil {
		return nil, err
	}
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if identityErr := handle.RecheckPath(); identityErr != nil {
		return out, identityErr
	}
	return out, err
}

func (g *gitBroker) runInBareCache(ctx context.Context, timeout time.Duration, handle *bareCacheHandle, args ...string) error {
	ctx, cancel := g.withTimeout(ctx, timeout)
	defer cancel()
	cmd, err := g.bareCacheCommand(ctx, handle, args...)
	if err != nil {
		return err
	}
	err = cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if identityErr := handle.RecheckPath(); identityErr != nil {
		return identityErr
	}
	return err
}

func (g *gitBroker) output(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := g.withTimeout(ctx, timeout)
	defer cancel()
	out, err := g.command(ctx, args...).Output()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func (g *gitBroker) run(ctx context.Context, timeout time.Duration, args ...string) error {
	ctx, cancel := g.withTimeout(ctx, timeout)
	defer cancel()
	err := g.command(ctx, args...).Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func legacyGitBroker() *gitBroker {
	broker, err := defaultGitBroker()
	if err != nil {
		return &gitBroker{executable: filepath.Join(string(filepath.Separator), "trusted-git-unavailable"), env: gitEnv(), timeout: repoCacheGitTimeout}
	}
	return broker
}

func runGitCombinedOutput(args ...string) ([]byte, error) {
	return runGitCombinedOutputWithTimeout(repoCacheGitTimeout, args...)
}

func runGitCombinedOutputWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	return legacyGitBroker().combinedOutput(context.Background(), timeout, args...)
}

func runGitOutput(args ...string) ([]byte, error) {
	return runGitOutputWithTimeout(repoCacheGitTimeout, args...)
}

func runGitOutputWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	return legacyGitBroker().output(context.Background(), timeout, args...)
}

func runGit(args ...string) error {
	return runGitWithTimeout(repoCacheGitTimeout, args...)
}

func runGitWithTimeout(timeout time.Duration, args ...string) error {
	return legacyGitBroker().run(context.Background(), timeout, args...)
}

// RepoInfo describes a repository to cache.
type RepoInfo struct {
	URL string
}

// CachedRepo describes a cached bare clone ready for worktree creation.
type CachedRepo struct {
	URL       string // remote URL
	LocalPath string // absolute path to the bare clone
}

// ResolvedRepo binds an assigned repository URL to the exact canonical bare
// cache path whose origin still matches that assignment.
type ResolvedRepo struct {
	URL      string
	BarePath string
}

// Cache manages bare git clones for workspace repositories.
type Cache struct {
	root   string // base directory for all caches (e.g. ~/multica_workspaces/.repos)
	logger *slog.Logger
	git    *gitBroker
	gitErr error
	// existingWorktreeHook is test-only synchronization for adversarial
	// replacement scenarios. Production caches leave it nil.
	existingWorktreeHook func(stage, worktreePath string)
	// worktreeAddHook is test-only synchronization after Git starts creating a
	// new worktree but before the daemon waits for it to exit.
	worktreeAddHook func(stage, worktreePath string)
	// worktreePublicationHook is test-only synchronization immediately before
	// an identity-bound staging worktree is published into the task workdir.
	worktreePublicationHook func(stage, worktreePath string)
	// bareCacheHook is test-only synchronization for clone postcondition
	// scenarios. Production caches leave it nil.
	bareCacheHook func(stage, barePath string)
	// repoLocks maps bare repo path → dedicated mutex. Any mutating operation
	// on a given bare repo (clone, fetch, worktree add, ref update) must
	// hold its lock — git's own lockfiles (packed-refs.lock, config.lock,
	// worktree admin dirs) don't tolerate parallel mutations on the same
	// repo. Separate repos are independent and run concurrently.
	repoLocks sync.Map // barePath -> *sync.Mutex
}

// New creates a new repo cache rooted at the given directory.
func New(root string, logger *slog.Logger) *Cache {
	broker, err := defaultGitBroker()
	return &Cache{root: root, logger: logger, git: broker, gitErr: err}
}

// NewWithGitBroker creates a cache bound to a caller-selected absolute Git
// executable. It fails closed instead of consulting PATH.
func NewWithGitBroker(root string, logger *slog.Logger, options GitBrokerOptions) (*Cache, error) {
	broker, err := newGitBroker(options)
	if err != nil {
		return nil, err
	}
	return &Cache{root: root, logger: logger, git: broker}, nil
}

func (c *Cache) requireGit() error {
	if c.gitErr != nil {
		return c.gitErr
	}
	if c.git == nil {
		return fmt.Errorf("repository Git broker is not configured")
	}
	return nil
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
	return c.SyncContext(context.Background(), workspaceID, repos)
}

// SyncContext is the task-safe Sync entry point. Every Git subprocess derives
// cancellation and deadlines from ctx and executes through the cache broker.
func (c *Cache) SyncContext(ctx context.Context, workspaceID string, repos []RepoInfo) error {
	if err := c.requireGit(); err != nil {
		return err
	}
	wsDir, err := c.workspaceCacheDir(workspaceID, true)
	if err != nil {
		return err
	}

	var firstErr error
	for _, repo := range repos {
		barePath, pathErr := cacheTargetPath(wsDir, repo.URL)
		if pathErr != nil {
			if firstErr == nil {
				firstErr = pathErr
			}
			continue
		}

		repoLock := c.lockForRepo(barePath)
		repoLock.Lock()
		_, statErr := os.Lstat(barePath)
		if statErr == nil {
			handle, err := c.openValidatedBareCache(barePath)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				repoLock.Unlock()
				continue
			}
			if c.isBareRepoHandle(ctx, handle) {
				// Already cached — fetch latest.
				c.logger.Info("repo cache: fetching", "url", repo.URL, "path", barePath)
				if err := c.gitFetchHandle(ctx, handle); err != nil {
					c.logger.Warn("repo cache: fetch failed", "url", repo.URL, "error", err)
					if firstErr == nil {
						firstErr = err
					}
				}
			} else {
				err := fmt.Errorf("repo cache target already exists and is not a bare git repository: %s", barePath)
				c.logger.Error("repo cache: unsafe existing target", "url", repo.URL, "path", barePath, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
			_ = handle.Close()
		} else if !os.IsNotExist(statErr) {
			wrapped := fmt.Errorf("stat repo cache target %s: %w", barePath, statErr)
			if firstErr == nil {
				firstErr = wrapped
			}
		} else {
			// Not cached — bare clone.
			c.logger.Info("repo cache: cloning", "url", repo.URL, "path", barePath)
			if err := c.gitCloneBare(ctx, repo.URL, barePath); err != nil {
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
	return c.LookupContext(context.Background(), workspaceID, url)
}

// LookupContext verifies a cached repository through the broker using ctx.
func (c *Cache) LookupContext(ctx context.Context, workspaceID, url string) string {
	if c.requireGit() != nil {
		return ""
	}
	barePath, err := c.lookupPath(ctx, workspaceID, url)
	if err != nil {
		return ""
	}
	handle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return ""
	}
	defer handle.Close()
	if c.isBareRepoHandle(ctx, handle) {
		return barePath
	}
	return ""
}

// Resolve returns a stable repository identity suitable for freezing into a
// task isolation policy. It rejects a cache whose on-disk origin has drifted
// from the URL that selected the cache path.
func (c *Cache) Resolve(workspaceID, rawURL string) (ResolvedRepo, error) {
	return c.ResolveContext(context.Background(), workspaceID, rawURL)
}

// ResolveContext resolves and verifies repository identity through the broker.
func (c *Cache) ResolveContext(ctx context.Context, workspaceID, rawURL string) (ResolvedRepo, error) {
	if err := c.requireGit(); err != nil {
		return ResolvedRepo{}, err
	}
	assignedURL := strings.TrimSpace(rawURL)
	barePath, err := c.lookupPath(ctx, workspaceID, assignedURL)
	if err != nil {
		return ResolvedRepo{}, err
	}
	canonical, err := filepath.EvalSymlinks(barePath)
	if err != nil {
		return ResolvedRepo{}, fmt.Errorf("resolve cached repo path %q: %w", barePath, err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return ResolvedRepo{}, fmt.Errorf("make cached repo path absolute: %w", err)
	}
	if canonical != barePath {
		return ResolvedRepo{}, fmt.Errorf("cached repo path %q resolves through symlink to %q", barePath, canonical)
	}
	handle, err := c.openValidatedBareCache(canonical)
	if err != nil {
		return ResolvedRepo{}, err
	}
	defer handle.Close()
	if !c.isBareRepoHandle(ctx, handle) {
		return ResolvedRepo{}, fmt.Errorf("repo not found in cache: %s (workspace: %s)", rawURL, workspaceID)
	}
	origin, err := c.git.outputInBareCache(ctx, 0, handle, "remote", "get-url", "origin")
	if err != nil {
		return ResolvedRepo{}, fmt.Errorf("read cached repo origin for %q: %w", assignedURL, err)
	}
	if got := strings.TrimSpace(string(origin)); got != assignedURL {
		return ResolvedRepo{}, fmt.Errorf("cached repo origin mismatch: got %q, want %q", got, assignedURL)
	}
	return ResolvedRepo{URL: assignedURL, BarePath: canonical}, nil
}

func (c *Cache) lookupPath(ctx context.Context, workspaceID, rawURL string) (string, error) {
	wsDir, err := c.workspaceCacheDir(workspaceID, false)
	if err != nil {
		return "", fmt.Errorf("repo not found in cache: %s (workspace: %s): %w", rawURL, workspaceID, err)
	}
	barePath, err := cacheTargetPath(wsDir, rawURL)
	if err != nil {
		return "", err
	}
	if err := c.validateBareCachePath(barePath); err != nil {
		return "", fmt.Errorf("repo not found in cache: %s (workspace: %s): %w", rawURL, workspaceID, err)
	}
	return barePath, nil
}

// WithRepoLock serializes caller-supplied mutations on a bare repo against all
// other same-repo operations that use the cache's lock (Sync, Fetch,
// CreateWorktree, and daemon GC maintenance).
func (c *Cache) WithRepoLock(barePath string, fn func() error) error {
	repoLock := c.lockForRepo(barePath)
	repoLock.Lock()
	defer repoLock.Unlock()
	return fn()
}

// Fetch runs `git fetch origin` on a cached bare clone to get latest refs.
func (c *Cache) Fetch(barePath string) error {
	return c.FetchContext(context.Background(), barePath)
}

// FetchContext fetches a cache through the broker using caller cancellation.
func (c *Cache) FetchContext(ctx context.Context, barePath string) error {
	if err := c.requireGit(); err != nil {
		return err
	}
	return c.WithRepoLock(barePath, func() error {
		handle, err := c.openValidatedBareCache(barePath)
		if err != nil {
			return err
		}
		defer handle.Close()
		return c.gitFetchHandle(ctx, handle)
	})
}

func (c *Cache) workspaceCacheDir(workspaceID string, create bool) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !isSafePathComponent(workspaceID) {
		return "", fmt.Errorf("unsafe workspace ID %q", workspaceID)
	}

	root, err := canonicalExistingDir(c.root)
	if err != nil {
		if !create || !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve repo cache root: %w", err)
		}
		if err := os.MkdirAll(c.root, 0o755); err != nil {
			return "", fmt.Errorf("create repo cache root: %w", err)
		}
		root, err = canonicalExistingDir(c.root)
		if err != nil {
			return "", fmt.Errorf("resolve repo cache root: %w", err)
		}
	}
	if err := validateDaemonOwnedDirectoryPath(root, "repo cache root"); err != nil {
		return "", err
	}

	wsDir := filepath.Join(root, workspaceID)
	info, statErr := os.Lstat(wsDir)
	if statErr != nil {
		if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("stat workspace cache dir: %w", statErr)
		}
		if !create {
			return "", fmt.Errorf("workspace cache not found: %s", workspaceID)
		}
		if err := os.Mkdir(wsDir, 0o755); err != nil {
			return "", fmt.Errorf("create workspace cache dir: %w", err)
		}
		info, statErr = os.Lstat(wsDir)
	}
	if statErr != nil {
		return "", fmt.Errorf("stat workspace cache dir: %w", statErr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("workspace cache dir must not be a symlink: %s", wsDir)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace cache path is not a directory: %s", wsDir)
	}

	canonical, err := filepath.EvalSymlinks(wsDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace cache dir: %w", err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", fmt.Errorf("make workspace cache dir absolute: %w", err)
	}
	if !pathWithin(root, canonical) {
		return "", fmt.Errorf("workspace cache dir escapes repo cache root: %s", canonical)
	}
	if err := validateDaemonOwnedDirectoryPath(canonical, "workspace cache directory"); err != nil {
		return "", err
	}
	return filepath.Clean(canonical), nil
}

// validateBareCachePath establishes the pathname trust boundary required
// before any Git process may inspect or mutate an existing bare cache.
func (c *Cache) validateBareCachePath(barePath string) error {
	abs, err := filepath.Abs(barePath)
	if err != nil {
		return fmt.Errorf("make bare repository cache path absolute: %w", err)
	}
	abs = filepath.Clean(abs)
	root, err := canonicalExistingDir(c.root)
	if err != nil {
		return fmt.Errorf("resolve repo cache root: %w", err)
	}
	canonical, err := canonicalExistingDir(barePath)
	if err != nil {
		return fmt.Errorf("resolve bare repository cache: %w", err)
	}
	if canonical != abs {
		return fmt.Errorf("bare repository cache path resolves through symlink: got %s, want %s", abs, canonical)
	}
	if !pathWithin(root, canonical) {
		return fmt.Errorf("bare repository cache escapes repo cache root: %s", canonical)
	}
	if err := validateDaemonOwnedDirectoryPath(root, "repo cache root"); err != nil {
		return err
	}
	for current := filepath.Dir(canonical); ; current = filepath.Dir(current) {
		if !pathWithin(root, current) && current != root {
			break
		}
		if err := validateDaemonOwnedDirectoryPath(current, "repository cache parent"); err != nil {
			return err
		}
		if current == root {
			break
		}
	}
	return validateDaemonOwnedDirectoryPath(canonical, "bare repository cache")
}

func (c *Cache) openValidatedBareCache(barePath string) (*bareCacheHandle, error) {
	if err := c.validateBareCachePath(barePath); err != nil {
		return nil, err
	}
	handle, err := openBareCacheHandle(barePath)
	if err != nil {
		return nil, fmt.Errorf("open bare repository cache identity: %w", err)
	}
	if err := c.validateBareCachePath(barePath); err != nil {
		_ = handle.Close()
		return nil, err
	}
	if err := handle.RecheckPath(); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return handle, nil
}

func cacheTargetPath(wsDir, rawURL string) (string, error) {
	name, err := bareDirNameSafe(rawURL)
	if err != nil {
		return "", err
	}
	target := filepath.Join(wsDir, name)
	if !pathWithin(wsDir, target) {
		return "", fmt.Errorf("repo cache target escapes workspace cache: %s", target)
	}
	return target, nil
}

func canonicalExistingDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(canonical), nil
}

func pathWithin(parent, target string) bool {
	parentAbs, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(parentAbs), filepath.Clean(targetAbs))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func isSafePathComponent(name string) bool {
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) {
		return false
	}
	return !strings.ContainsAny(name, `/\\`)
}

// bareDirName returns a filesystem-safe, collision-free directory name for
// the bare clone of rawURL. The name is built from the host plus each
// path segment, joined by '+'. '+' is disallowed in GitHub and GitLab
// path segments, so two URLs produce the same name only if they point at
// the same repository on the same host.
//
// Examples:
//
//	https://github.com/org/my-repo.git           -> github.com+org+my-repo.git
//	git@github.com:org/my-repo                   -> github.com+org+my-repo.git
//	git@github.com:foo/bar-baz.git               -> github.com+foo+bar-baz.git
//	git@github.com:foo-bar/baz.git               -> github.com+foo-bar+baz.git
//	git@github.com:org/repo.git                  -> github.com+org+repo.git
//	git@gitlab.example.com:org/repo.git          -> gitlab.example.com+org+repo.git
//	ssh://git@gitlab.example.com:22/g/s/r.git    -> gitlab.example.com%3A22+g+s+r.git
//	git@gitlab.example.com-22:org/repo.git       -> gitlab.example.com-22+org+repo.git
//	my-repo                                      -> my-repo.git (bare name fallback)
func bareDirName(rawURL string) string {
	name, err := bareDirNameSafe(rawURL)
	if err != nil {
		return ""
	}
	return name
}

func bareDirNameSafe(rawURL string) (string, error) {
	if err := validateRepoURL(rawURL); err != nil {
		return "", err
	}
	rawURL = strings.TrimRight(rawURL, "/")

	host, path := splitHostAndPath(rawURL)
	host = strings.ToLower(strings.TrimSpace(host))
	// Encode ':' as '%3A' so host:port is lossless. A naive ':'->'-' rewrite
	// would collapse `gitlab.example.com:22` onto a literal hostname
	// `gitlab.example.com-22`, reintroducing the silent wrong-remote class
	// this function exists to prevent. '%' is forbidden in valid hostnames
	// (RFC 952 / RFC 1123), and in GitHub/GitLab path segments, so the
	// encoded marker can never come from a legal input.
	host = strings.ReplaceAll(host, ":", "%3A")

	var parts []string
	if host != "" {
		parts = append(parts, host)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg != "" {
			parts = append(parts, seg)
		}
	}

	name := strings.Join(parts, "+")
	if !strings.HasSuffix(name, ".git") {
		name += ".git"
	}
	if name == "" || name == ".git" {
		return "", fmt.Errorf("unsafe repo URL %q: empty repository name", rawURL)
	}
	if !isSafePathComponent(name) {
		return "", fmt.Errorf("unsafe repo URL %q: invalid cache directory name", rawURL)
	}
	return name, nil
}

// splitHostAndPath extracts the host and path-with-namespace from the
// supported git URL forms:
//
//   - URL form (ssh://user@host[:port]/path, https://host/path) — returns
//     u.Host verbatim (may include :port) and u.Path without the leading slash.
//   - scp-style ([user@]host:path) — splits on the first ':' after the
//     optional 'user@'.
//   - Anything else (bare repo names, absolute filesystem paths) — returns
//     an empty host and the raw input as the path.
func splitHostAndPath(rawURL string) (host, path string) {
	if u, err := url.Parse(rawURL); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Host, strings.TrimPrefix(u.Path, "/")
	}
	s := rawURL
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, ":"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

func validateRepoURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return fmt.Errorf("unsafe repo URL %q: empty or reserved repository name", rawURL)
	}
	if strings.ContainsAny(trimmed, "\x00\r\n\t") {
		return fmt.Errorf("unsafe repo URL %q: control characters are not allowed", rawURL)
	}
	if strings.Contains(trimmed, `\`) {
		return fmt.Errorf("unsafe repo URL %q: backslashes are not allowed", rawURL)
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Scheme != "" {
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("unsafe repo URL %q: query and fragment are not allowed", rawURL)
		}
	}

	_, repoPath := splitHostAndPath(strings.TrimRight(trimmed, "/"))
	if repoPath == "" {
		return fmt.Errorf("unsafe repo URL %q: empty repository path", rawURL)
	}
	for _, segment := range strings.Split(repoPath, "/") {
		if segment == "" {
			continue
		}
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			return fmt.Errorf("unsafe repo URL %q: invalid path escape", rawURL)
		}
		if decoded == "." || decoded == ".." || strings.ContainsAny(decoded, `/\\`) {
			return fmt.Errorf("unsafe repo URL %q: path traversal is not allowed", rawURL)
		}
	}

	name := repoPath
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".git")
	decodedName, err := url.PathUnescape(name)
	if err != nil {
		return fmt.Errorf("unsafe repo URL %q: invalid repository name escape", rawURL)
	}
	if !isSafeRepoName(decodedName) {
		return fmt.Errorf("unsafe repo URL %q: invalid repository name %q", rawURL, decodedName)
	}
	return nil
}

func isSafeRepoName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// isBareRepo checks if a path looks like a bare git repository.
func isBareRepo(path string) bool {
	return legacyGitBroker().isBareRepo(context.Background(), path)
}

func (c *Cache) isBareRepo(ctx context.Context, path string) bool {
	if c.git == nil {
		return false
	}
	handle, err := c.openValidatedBareCache(path)
	if err != nil {
		return false
	}
	defer handle.Close()
	return c.isBareRepoHandle(ctx, handle)
}

func (g *gitBroker) isBareRepo(ctx context.Context, path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	out, err := g.output(ctx, 0, "-C", path, "rev-parse", "--is-bare-repository")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func (c *Cache) isBareRepoHandle(ctx context.Context, handle *bareCacheHandle) bool {
	out, err := c.git.outputInBareCache(ctx, 0, handle, "rev-parse", "--is-bare-repository")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// modernFetchRefspec is the remote-tracking refspec that keeps fetched heads
// out of the bare repo's refs/heads/* namespace. That namespace is reserved
// for per-task worktree branches created by `git worktree add -b ...`, and any
// mirror-style fetch that targets refs/heads/* can collide with those locked
// refs and abort the entire fetch.
const modernFetchRefspec = "+refs/heads/*:refs/remotes/origin/*"

func gitCloneBare(url, dest string) error {
	return legacyCache().gitCloneBare(context.Background(), url, dest)
}

func (c *Cache) gitCloneBare(ctx context.Context, url, dest string) error {
	if out, err := c.git.combinedOutput(ctx, 0, "clone", "--bare", url, dest); err != nil {
		// Clean up partial clone.
		os.RemoveAll(dest)
		return fmt.Errorf("git clone --bare: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if c.bareCacheHook != nil {
		c.bareCacheHook("after-clone", dest)
	}
	if err := c.validateBareCachePath(dest); err != nil {
		os.RemoveAll(dest)
		return fmt.Errorf("validate cloned bare repository: %w", err)
	}
	// `git clone --bare` populates refs/heads/* as a snapshot and defaults to
	// a mirror-style fetch refspec. Convert the bare repo to the standard
	// remote-tracking layout immediately so subsequent fetches write to
	// refs/remotes/origin/* and can't conflict with worktree-locked heads.
	if err := c.ensureRemoteTrackingLayout(ctx, dest); err != nil {
		os.RemoveAll(dest)
		return fmt.Errorf("configure fetch refspec: %w", err)
	}
	if err := c.validateBareCachePath(dest); err != nil {
		os.RemoveAll(dest)
		return fmt.Errorf("validate cloned bare repository: %w", err)
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
	return legacyCacheForBarePath(barePath).gitFetch(context.Background(), barePath)
}

func (c *Cache) gitFetch(ctx context.Context, barePath string) error {
	handle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return err
	}
	defer handle.Close()
	return c.gitFetchHandle(ctx, handle)
}

func (c *Cache) gitFetchHandle(ctx context.Context, handle *bareCacheHandle) error {
	if err := c.ensureRemoteTrackingLayoutHandle(ctx, handle); err != nil {
		return fmt.Errorf("ensure refspec: %w", err)
	}
	if err := c.runGitFetchHandle(ctx, handle); err != nil {
		return err
	}
	// Refresh refs/remotes/origin/HEAD after every successful fetch.
	// set-head --auto is lightweight (a single ls-remote HEAD round-trip)
	// and non-fatal: if it fails we still have the step 2-5 fallbacks in
	// getRemoteDefaultBranch, but the modern-cache default-branch-change
	// path (the only path that can't be recovered any other way) relies
	// on this call.
	_ = c.git.runInBareCache(ctx, 0, handle, "remote", "set-head", "origin", "--auto")
	return nil
}

// runGitFetch is the raw `git fetch origin` wrapper. Callers should go through
// gitFetch, which migrates legacy caches first.
func runGitFetch(barePath string) error {
	return legacyCacheForBarePath(barePath).runGitFetch(context.Background(), barePath)
}

func (c *Cache) runGitFetch(ctx context.Context, barePath string) error {
	handle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return err
	}
	defer handle.Close()
	return c.runGitFetchHandle(ctx, handle)
}

func (c *Cache) runGitFetchHandle(ctx context.Context, handle *bareCacheHandle) error {
	if out, err := c.git.combinedOutputInBareCache(ctx, 0, handle, "fetch", "origin"); err != nil {
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
	return legacyCacheForBarePath(barePath).ensureRemoteTrackingLayout(context.Background(), barePath)
}

func (c *Cache) ensureRemoteTrackingLayout(ctx context.Context, barePath string) error {
	handle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return err
	}
	defer handle.Close()
	return c.ensureRemoteTrackingLayoutHandle(ctx, handle)
}

func (c *Cache) ensureRemoteTrackingLayoutHandle(ctx context.Context, handle *bareCacheHandle) error {
	cur, err := c.readFetchRefspecHandle(ctx, handle)
	if err != nil {
		return err
	}
	if cur == modernFetchRefspec || cur == strings.TrimPrefix(modernFetchRefspec, "+") {
		return nil // already modern
	}
	if err := c.setFetchRefspecHandle(ctx, handle, modernFetchRefspec); err != nil {
		return err
	}
	// Backfill refs/remotes/origin/* by fetching with the new refspec. This
	// writes to the origin/* namespace, so even worktree-locked refs/heads/*
	// branches can't collide.
	if err := c.runGitFetchHandle(ctx, handle); err != nil {
		return fmt.Errorf("backfill fetch after refspec migration: %w", err)
	}
	// Set refs/remotes/origin/HEAD so getRemoteDefaultBranch can read it.
	// Non-fatal: if this fails we fall back to origin/main, origin/master.
	_ = c.git.runInBareCache(ctx, 0, handle, "remote", "set-head", "origin", "--auto")
	return nil
}

// readFetchRefspec returns the current remote.origin.fetch config value, or
// the empty string if it's not set. Distinguishes "missing" (exit 1) from
// real git errors.
func readFetchRefspec(barePath string) (string, error) {
	return legacyCacheForBarePath(barePath).readFetchRefspec(context.Background(), barePath)
}

func (c *Cache) readFetchRefspec(ctx context.Context, barePath string) (string, error) {
	handle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	return c.readFetchRefspecHandle(ctx, handle)
}

func (c *Cache) readFetchRefspecHandle(ctx context.Context, handle *bareCacheHandle) (string, error) {
	out, err := c.git.outputInBareCache(ctx, 0, handle, "config", "--get", "remote.origin.fetch")
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "", nil // key missing, not an error
		}
		return "", fmt.Errorf("read remote.origin.fetch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func setFetchRefspec(barePath, refspec string) error {
	return legacyCacheForBarePath(barePath).setFetchRefspec(context.Background(), barePath, refspec)
}

func (c *Cache) setFetchRefspec(ctx context.Context, barePath, refspec string) error {
	handle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return err
	}
	defer handle.Close()
	return c.setFetchRefspecHandle(ctx, handle, refspec)
}

func (c *Cache) setFetchRefspecHandle(ctx context.Context, handle *bareCacheHandle, refspec string) error {
	out, err := c.git.combinedOutputInBareCache(ctx, 0, handle, "config", "remote.origin.fetch", refspec)
	if err != nil {
		return fmt.Errorf("set remote.origin.fetch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func legacyCache() *Cache {
	broker := legacyGitBroker()
	return &Cache{git: broker, logger: slog.Default()}
}

func legacyCacheForBarePath(barePath string) *Cache {
	cache := legacyCache()
	abs, err := filepath.Abs(barePath)
	if err == nil {
		cache.root = filepath.Dir(filepath.Clean(abs))
	}
	return cache
}

// WorktreeParams holds inputs for creating a worktree from a cached bare clone.
type WorktreeParams struct {
	WorkspaceID         string // workspace that owns the repo
	RepoURL             string // remote URL to look up in the cache
	WorkDir             string // parent directory for the worktree (e.g. task workdir)
	Ref                 string // optional branch, tag, or commit to base the worktree on
	AgentName           string // for branch naming
	TaskID              string // for branch naming uniqueness
	CoAuthoredByEnabled bool   // install prepare-commit-msg hook for Co-authored-by trailer
	LocalDirectory      bool   // reject checkout when the task runs in a user-owned local directory
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
	return c.CreateWorktreeContext(context.Background(), params)
}

// CreateWorktreeContext is the task-safe checkout entry point. All Git
// subprocesses use ctx and the cache's immutable broker configuration.
func (c *Cache) CreateWorktreeContext(ctx context.Context, params WorktreeParams) (_ *WorktreeResult, returnErr error) {
	if err := c.requireGit(); err != nil {
		return nil, err
	}
	if params.LocalDirectory || c.isGitCheckoutRoot(ctx, params.WorkDir) {
		return nil, fmt.Errorf("local_directory mode forbids creating an additional repo checkout")
	}
	dirName, err := worktreeDirName(params.RepoURL)
	if err != nil {
		return nil, err
	}
	worktreePath, err := canonicalWorktreeTarget(params.WorkDir, dirName)
	if err != nil {
		return nil, err
	}
	barePath, err := c.lookupPath(ctx, params.WorkspaceID, params.RepoURL)
	if err != nil {
		return nil, err
	}

	// Serialize concurrent CreateWorktree calls on the same bare repo. Git's
	// own lockfiles (packed-refs.lock, config.lock, worktree admin dirs)
	// can't tolerate parallel fetch + worktree mutations on the same repo.
	repoLock := c.lockForRepo(barePath)
	repoLock.Lock()
	defer repoLock.Unlock()
	bareHandle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return nil, err
	}
	defer bareHandle.Close()
	if c.bareCacheHook != nil {
		c.bareCacheHook("after-open", barePath)
	}
	if err := bareHandle.RecheckPath(); err != nil {
		return nil, err
	}

	// Fetch latest from origin. This also migrates the bare cache's refspec
	// to the modern remote-tracking layout on first run, so subsequent fetches
	// never collide with the refs/heads/agent/* branches that worktree creation
	// locks in this same bare repo.
	if err := c.gitFetchHandle(ctx, bareHandle); err != nil {
		// Non-fatal: preserve cached state and continue, but make the warning
		// loud enough that it's findable in the daemon log. The agent will
		// receive an older snapshot than the remote head.
		c.logger.Warn("repo checkout: fetch failed, agent will see possibly stale code",
			"url", params.RepoURL,
			"error", err,
		)
	}

	// Determine the ref to base the worktree on. By default this is the remote's
	// default branch (resolved internally via getRemoteDefaultBranch, which walks
	// origin/HEAD → origin/main, origin/master → bare-HEAD hint into origin/<same>
	// → single-entry scan of origin/* → bare HEAD when origin/* is empty).
	// Callers may request a specific branch, tag, or commit so review/QA agents
	// can inspect the exact revision without trying to mutate the daemon-owned
	// worktree metadata themselves.
	baseRef, err := c.resolveBaseRefHandle(ctx, bareHandle, params.Ref)
	if err != nil {
		return nil, err
	}

	// Empty here means params.Ref was unset and getRemoteDefaultBranch couldn't
	// resolve a default — the cache is in a state we refuse to guess from (no
	// origin/HEAD, no main/master, bare HEAD doesn't match any origin/* entry,
	// and origin/* has multiple candidates). The requested-ref path returns an
	// explicit error before reaching here, so this branch only fires for the
	// default-branch case.
	if baseRef == "" {
		return nil, fmt.Errorf("cannot resolve default branch for %s: bare cache at %s has no usable refs (origin/* is empty or ambiguous and bare HEAD has no match). The cache may be corrupted; delete it and retry", params.RepoURL, barePath)
	}

	// Build branch name: agent/{sanitized-name}/{short-task-id}
	branchName := fmt.Sprintf("agent/%s/%s", sanitizeName(params.AgentName), shortID(params.TaskID))

	// If worktree already exists (reused environment from a prior task),
	// update it to the latest remote code instead of creating a new one.
	if isGitWorktree(worktreePath) {
		handle, err := openWorktreeHandle(worktreePath)
		if err != nil {
			return nil, fmt.Errorf("open existing worktree identity: %w", err)
		}
		defer handle.Close()
		if err := c.verifyWorktreeOwnerHandle(ctx, handle, barePath); err != nil {
			return nil, err
		}
		if err := c.rejectExecutableRepositoryConfig(ctx, handle); err != nil {
			return nil, err
		}
		if c.existingWorktreeHook != nil {
			c.existingWorktreeHook("after-owner-proof", worktreePath)
		}
		if err := handle.RecheckPaths(); err != nil {
			return nil, err
		}
		actualBranch, err := c.updateExistingWorktreeHandle(ctx, handle, branchName, baseRef)
		if err != nil {
			return nil, fmt.Errorf("update existing worktree: %w", err)
		}
		if err := handle.RecheckPaths(); err != nil {
			return nil, err
		}

		for _, pattern := range agentGitExcludePatterns {
			_ = excludeFromGitDirHandle(handle, pattern)
		}

		// Install or remove the Co-authored-by hook based on the workspace
		// setting. The hook lives in the bare repo's shared hooks dir, so we
		// must actively remove it when disabled — otherwise a previously
		// installed hook keeps appending the trailer to every commit even
		// after the user toggles the setting off.
		if params.CoAuthoredByEnabled {
			if err := installCoAuthoredByHookInCommonDirHandle(handle); err != nil {
				c.logger.Warn("repo checkout: install co-authored-by hook failed (non-fatal)", "error", err)
			}
		} else {
			if err := removeCoAuthoredByHookInCommonDirHandle(handle); err != nil {
				c.logger.Warn("repo checkout: remove co-authored-by hook failed (non-fatal)", "error", err)
			}
		}
		if err := handle.RecheckPaths(); err != nil {
			return nil, err
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

	// Create new worktrees outside the task-visible workdir, then publish the
	// completed checkout atomically. This keeps Git from following a target
	// path that the task replaces while `git worktree add` is still running.
	publication, err := newWorktreePublication(c.root, worktreePath)
	if err != nil {
		return nil, fmt.Errorf("prepare worktree publication: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, publication.Close())
	}()
	actualBranch, err := c.createWorktreeHandle(ctx, bareHandle, publication.StagingPath(), branchName, baseRef)
	if err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	committed := false
	var publicationHandle *worktreeHandle
	defer func() {
		if committed {
			if publicationHandle != nil {
				returnErr = errors.Join(returnErr, publicationHandle.Close())
			}
			return
		}
		cleanupCtx := context.WithoutCancel(ctx)
		rollbackErr := publication.Rollback(publicationHandle)
		var cleanupErr error
		if publication.CleanupAllowed() {
			cleanupErr = c.cleanupUnpublishedWorktreeHandle(cleanupCtx, bareHandle, publication.StagingPath(), actualBranch)
		}
		var closeErr error
		if publicationHandle != nil {
			closeErr = publicationHandle.Close()
		}
		returnErr = errors.Join(returnErr, rollbackErr, cleanupErr, closeErr)
	}()

	if identityBoundWorktreeAccessSupported() {
		handle, err := openWorktreeHandle(publication.StagingPath())
		if err != nil {
			return nil, fmt.Errorf("open new worktree identity: %w", err)
		}
		publicationHandle = handle
		if err := c.verifyWorktreeOwnerHandle(ctx, handle, barePath); err != nil {
			return nil, fmt.Errorf("verify new worktree ownership: %w", err)
		}
		if err := publication.Prepare(handle); err != nil {
			return nil, fmt.Errorf("prepare new worktree backlink: %w", err)
		}
		if c.worktreePublicationHook != nil {
			c.worktreePublicationHook("before-publication", worktreePath)
		}
		if err := publication.Publish(handle); err != nil {
			return nil, fmt.Errorf("publish new worktree: %w", err)
		}
		if c.worktreePublicationHook != nil {
			c.worktreePublicationHook("after-publication", worktreePath)
		}
		if err := handle.VerifyGitDirBacklink(); err != nil {
			return nil, fmt.Errorf("verify published worktree backlink: %w", err)
		}
		if err := handle.RecheckPaths(); err != nil {
			return nil, err
		}
		if c.existingWorktreeHook != nil {
			c.existingWorktreeHook("after-worktree-add", worktreePath)
		}

		if c.existingWorktreeHook != nil {
			c.existingWorktreeHook("before-exclude", worktreePath)
		}
		if err := handle.RecheckPaths(); err != nil {
			return nil, err
		}
		for _, pattern := range agentGitExcludePatterns {
			if err := excludeFromGitDirHandle(handle, pattern); err != nil {
				c.logger.Warn("repo checkout: exclude agent context failed (non-fatal)", "pattern", pattern, "error", err)
			}
		}
		if err := handle.RecheckPaths(); err != nil {
			return nil, err
		}

		if c.existingWorktreeHook != nil {
			c.existingWorktreeHook("before-hook", worktreePath)
		}
		if err := handle.RecheckPaths(); err != nil {
			return nil, err
		}
		if params.CoAuthoredByEnabled {
			if err := installCoAuthoredByHookInCommonDirHandle(handle); err != nil {
				c.logger.Warn("repo checkout: install co-authored-by hook failed (non-fatal)", "error", err)
			}
		} else {
			if err := removeCoAuthoredByHookInCommonDirHandle(handle); err != nil {
				c.logger.Warn("repo checkout: remove co-authored-by hook failed (non-fatal)", "error", err)
			}
		}
		if err := handle.RecheckPaths(); err != nil {
			return nil, err
		}
		if c.existingWorktreeHook != nil {
			c.existingWorktreeHook("before-return", worktreePath)
		}
		if err := handle.RecheckPaths(); err != nil {
			return nil, err
		}
	} else {
		if err := publication.Publish(nil); err != nil {
			return nil, fmt.Errorf("publish new worktree: %w", err)
		}
		// Windows does not advertise task execution and lacks the descriptor-
		// relative primitives used above. Preserve operator-only worktree
		// creation compatibility while reused worktrees remain fail closed.
		for _, pattern := range agentGitExcludePatterns {
			_ = c.excludeFromGit(ctx, worktreePath, pattern)
		}
		if params.CoAuthoredByEnabled {
			if err := c.installCoAuthoredByHook(ctx, worktreePath); err != nil {
				c.logger.Warn("repo checkout: install co-authored-by hook failed (non-fatal)", "error", err)
			}
		} else {
			if err := c.removeCoAuthoredByHook(ctx, worktreePath); err != nil {
				c.logger.Warn("repo checkout: remove co-authored-by hook failed (non-fatal)", "error", err)
			}
		}
	}
	publication.Commit()
	committed = true

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

func resolveBaseRef(barePath, requestedRef string) (string, error) {
	return legacyCacheForBarePath(barePath).resolveBaseRef(context.Background(), barePath, requestedRef)
}

func (c *Cache) resolveBaseRef(ctx context.Context, barePath, requestedRef string) (string, error) {
	handle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	return c.resolveBaseRefHandle(ctx, handle, requestedRef)
}

func (c *Cache) resolveBaseRefHandle(ctx context.Context, handle *bareCacheHandle, requestedRef string) (string, error) {
	ref := strings.TrimSpace(requestedRef)
	if ref == "" {
		return c.getRemoteDefaultBranchHandle(ctx, handle), nil
	}

	// Prefer remote-tracking branches for human branch names. Then allow full
	// local refs, tags, and raw commits that exist in the fetched bare cache.
	candidates := []string{
		"refs/remotes/origin/" + ref,
		"refs/tags/" + ref,
		ref,
	}
	for _, candidate := range candidates {
		if c.gitRefExistsHandle(ctx, handle, candidate+"^{commit}") {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot resolve requested ref %q in repo cache at %s", ref, handle.Path())
}

func gitRefExists(repoPath, ref string) bool {
	return legacyCacheForBarePath(repoPath).gitRefExists(context.Background(), repoPath, ref)
}

func (c *Cache) gitRefExists(ctx context.Context, repoPath, ref string) bool {
	handle, err := c.openValidatedBareCache(repoPath)
	if err != nil {
		return false
	}
	defer handle.Close()
	return c.gitRefExistsHandle(ctx, handle, ref)
}

func (c *Cache) gitRefExistsHandle(ctx context.Context, handle *bareCacheHandle, ref string) bool {
	return c.git.runInBareCache(ctx, 0, handle, "rev-parse", "--verify", "--quiet", ref) == nil
}

// createWorktree creates a git worktree at the given path with a new branch.
// Returns the actual branch name used — which may differ from the requested
// branchName if a collision was resolved by appending a timestamp suffix.
func createWorktree(gitRoot, worktreePath, branchName, baseRef string) (string, error) {
	return legacyCacheForBarePath(gitRoot).createWorktree(context.Background(), gitRoot, worktreePath, branchName, baseRef)
}

func (c *Cache) createWorktree(ctx context.Context, gitRoot, worktreePath, branchName, baseRef string) (string, error) {
	handle, err := c.openValidatedBareCache(gitRoot)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	return c.createWorktreeHandle(ctx, handle, worktreePath, branchName, baseRef)
}

func (c *Cache) createWorktreeHandle(ctx context.Context, handle *bareCacheHandle, worktreePath, branchName, baseRef string) (string, error) {
	// Pre-check: if the worktree path already exists we would get a confusing
	// "already exists" error from `git worktree add` — which used to be
	// misclassified as a branch collision, causing the retry to leak branches
	// into the bare repo. Fail cleanly here instead. The caller is expected
	// to route reused workdirs through updateExistingWorktree via isGitWorktree.
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree path already exists and is not a valid git worktree: %s", worktreePath)
	}

	err := c.runWorktreeAddHandle(ctx, handle, worktreePath, branchName, baseRef)
	if err != nil && isBranchCollisionError(err) {
		// Branch name collision: append timestamp and retry once.
		branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
		err = c.runWorktreeAddHandle(ctx, handle, worktreePath, branchName, baseRef)
	}
	if err != nil {
		return "", err
	}
	return branchName, nil
}

func runWorktreeAdd(gitRoot, worktreePath, branchName, baseRef string) error {
	return legacyCacheForBarePath(gitRoot).runWorktreeAdd(context.Background(), gitRoot, worktreePath, branchName, baseRef)
}

func (c *Cache) runWorktreeAdd(ctx context.Context, gitRoot, worktreePath, branchName, baseRef string) error {
	handle, err := c.openValidatedBareCache(gitRoot)
	if err != nil {
		return err
	}
	defer handle.Close()
	return c.runWorktreeAddHandle(ctx, handle, worktreePath, branchName, baseRef)
}

func (c *Cache) runWorktreeAddHandle(ctx context.Context, handle *bareCacheHandle, worktreePath, branchName, baseRef string) error {
	ctx, cancel := c.git.withTimeout(ctx, 0)
	defer cancel()
	cmd, err := c.git.bareCacheCommand(ctx, handle, "worktree", "add", "-b", branchName, worktreePath, baseRef)
	if err != nil {
		return err
	}
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start git worktree add: %w", err)
	}
	if c.worktreeAddHook != nil {
		c.worktreeAddHook("after-start", worktreePath)
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if identityErr := handle.RecheckPath(); identityErr != nil {
		return identityErr
	}
	if err != nil {
		out := output.String()
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (c *Cache) cleanupUnpublishedWorktreeHandle(ctx context.Context, handle *bareCacheHandle, worktreePath, branchName string) error {
	out, err := c.git.combinedOutputInBareCache(ctx, 0, handle, "worktree", "remove", "--force", worktreePath)
	if err != nil {
		return fmt.Errorf("remove unpublished worktree: %s: %w", strings.TrimSpace(string(out)), err)
	}
	out, err = c.git.combinedOutputInBareCache(ctx, 0, handle, "branch", "-D", branchName)
	if err != nil {
		return fmt.Errorf("delete unpublished worktree branch: %s: %w", strings.TrimSpace(string(out)), err)
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

func isGitCheckoutRoot(path string) bool {
	return legacyCache().isGitCheckoutRoot(context.Background(), path)
}

func (c *Cache) isGitCheckoutRoot(ctx context.Context, path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	out, err := c.git.output(ctx, 0, "-C", path, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	root, err := canonicalExistingDir(strings.TrimSpace(string(out)))
	if err != nil {
		return false
	}
	want, err := canonicalExistingDir(path)
	return err == nil && samePath(root, want)
}

func (c *Cache) verifyWorktreeOwnerHandle(_ context.Context, handle *worktreeHandle, expectedBarePath string) error {
	gitFile, err := readFileAt(handle.worktree.file, ".git")
	if err != nil {
		return fmt.Errorf("prove existing worktree ownership: read .git file: %w", err)
	}
	const gitDirPrefix = "gitdir: "
	gitFileValue := strings.TrimSpace(string(gitFile))
	if !strings.HasPrefix(gitFileValue, gitDirPrefix) {
		return fmt.Errorf("prove existing worktree ownership: invalid .git file")
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(gitFileValue, gitDirPrefix))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(handle.Path(), gitDir)
	}
	gitDir, err = canonicalExistingDir(gitDir)
	if err != nil {
		return fmt.Errorf("prove existing worktree ownership: resolve git dir: %w", err)
	}
	handle.gitDir, err = openDirectoryHandle(gitDir)
	if err != nil {
		return fmt.Errorf("open worktree git-dir identity: %w", err)
	}
	commonFile, err := readFileAt(handle.gitDir.file, "commondir")
	if err != nil {
		return fmt.Errorf("prove existing worktree ownership: read common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(commonFile))
	if commonDir == "" {
		return fmt.Errorf("prove existing worktree ownership: empty common dir")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(gitDir, commonDir)
	}
	commonDir, err = canonicalExistingDir(commonDir)
	if err != nil {
		return fmt.Errorf("prove existing worktree ownership: resolve common dir: %w", err)
	}
	expectedBarePath, err = canonicalExistingDir(expectedBarePath)
	if err != nil {
		return fmt.Errorf("prove existing worktree ownership: resolve expected bare cache: %w", err)
	}
	if !samePath(commonDir, expectedBarePath) {
		return fmt.Errorf("existing worktree belongs to a different bare cache: got %s, want %s", commonDir, expectedBarePath)
	}
	worktreesRoot := filepath.Join(expectedBarePath, "worktrees")
	if !samePath(filepath.Dir(gitDir), worktreesRoot) {
		return fmt.Errorf("existing worktree git dir escapes expected bare cache: %s", gitDir)
	}
	handle.common, err = openDirectoryHandle(commonDir)
	if err != nil {
		return fmt.Errorf("open git common-dir identity: %w", err)
	}
	if err := validateRepositoryMetadataIdentity(&handle.gitDir); err != nil {
		return err
	}
	if err := validateRepositoryMetadataIdentity(&handle.common); err != nil {
		return err
	}
	if err := handle.VerifyGitDirBacklink(); err != nil {
		return fmt.Errorf("prove existing worktree ownership: %w", err)
	}
	return handle.RecheckPaths()
}

func canonicalWorktreeTarget(workDir, dirName string) (string, error) {
	if strings.TrimSpace(workDir) == "" {
		return "", fmt.Errorf("workdir is empty")
	}
	if !isSafePathComponent(dirName) {
		return "", fmt.Errorf("unsafe repo directory name %q", dirName)
	}
	canonicalWorkDir, err := canonicalExistingDir(workDir)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}
	target := filepath.Join(canonicalWorkDir, dirName)
	if !pathWithin(canonicalWorkDir, target) {
		return "", fmt.Errorf("worktree target escapes workdir: %s", target)
	}

	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return target, nil
		}
		return "", fmt.Errorf("stat worktree target: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := filepath.EvalSymlinks(target)
		if resolveErr != nil {
			return "", fmt.Errorf("worktree target is a symlink: %s", target)
		}
		if !pathWithin(canonicalWorkDir, resolved) {
			return "", fmt.Errorf("worktree target symlink escapes workdir: %s -> %s", target, resolved)
		}
		return "", fmt.Errorf("worktree target must not be a symlink: %s", target)
	}
	canonicalTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve worktree target: %w", err)
	}
	if !pathWithin(canonicalWorkDir, canonicalTarget) {
		return "", fmt.Errorf("canonical worktree target escapes workdir: %s", canonicalTarget)
	}
	return filepath.Clean(canonicalTarget), nil
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// updateExistingWorktreeHandle resets a reused worktree and checks out a new
// branch while keeping every content operation bound to the retained directory
// identity established by openWorktreeHandle.
func (c *Cache) updateExistingWorktreeHandle(ctx context.Context, handle *worktreeHandle, branchName, baseRef string) (string, error) {
	gitArgs := func(args ...string) []string {
		return append([]string{"--git-dir=" + handle.gitDir.path, "--work-tree=.", "-c", "core.bare=false"}, args...)
	}
	type commandResult struct {
		output      []byte
		commandErr  error
		identityErr error
	}
	run := func(stage string, args ...string) commandResult {
		if err := handle.RecheckPaths(); err != nil {
			return commandResult{identityErr: err}
		}
		out, commandErr := c.git.combinedOutputInWorktree(ctx, 0, handle, gitArgs(args...)...)
		if commandErr == nil && c.existingWorktreeHook != nil {
			c.existingWorktreeHook(stage, handle.Path())
		}
		return commandResult{
			output:      out,
			commandErr:  commandErr,
			identityErr: handle.RecheckPaths(),
		}
	}

	reset := run("after-reset", "reset", "--hard")
	if reset.identityErr != nil {
		return "", fmt.Errorf("git reset --hard identity check: %w", reset.identityErr)
	}
	if reset.commandErr != nil {
		return "", fmt.Errorf("git reset --hard: %s: %w", strings.TrimSpace(string(reset.output)), reset.commandErr)
	}
	clean := run("after-clean", "clean", "-fd")
	if clean.identityErr != nil {
		return "", fmt.Errorf("git clean -fd identity check: %w", clean.identityErr)
	}
	if clean.commandErr != nil {
		return "", fmt.Errorf("git clean -fd: %s: %w", strings.TrimSpace(string(clean.output)), clean.commandErr)
	}
	checkout := run("after-checkout", "checkout", "-b", branchName, baseRef)
	if checkout.identityErr != nil {
		return "", fmt.Errorf("git checkout -b identity check: %w", checkout.identityErr)
	}
	if checkout.commandErr == nil {
		return branchName, nil
	}
	wrapped := fmt.Errorf("git checkout -b: %s: %w", strings.TrimSpace(string(checkout.output)), checkout.commandErr)
	if !isBranchCollisionError(wrapped) {
		return "", wrapped
	}
	branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
	retry := run("after-checkout-retry", "checkout", "-b", branchName, baseRef)
	if retry.identityErr != nil {
		return "", fmt.Errorf("git checkout -b (retry) identity check: %w", retry.identityErr)
	}
	if retry.commandErr != nil {
		return "", fmt.Errorf("git checkout -b (retry): %s: %w", strings.TrimSpace(string(retry.output)), retry.commandErr)
	}
	return branchName, nil
}

var executableRepositoryConfig = regexp.MustCompile(`(?i)^(include\.path$|includeif\..*\.path$|alias\.|core\.(fsmonitor|sshcommand|hookspath)$|credential\.helper$|filter\..*\.(clean|smudge|process)$|diff\..*\.(command|textconv)$|merge\..*\.driver$|gpg\..*\.program$)`)

func (c *Cache) rejectExecutableRepositoryConfig(ctx context.Context, handle *worktreeHandle) error {
	if err := handle.RecheckPaths(); err != nil {
		return err
	}
	cmd, err := c.git.worktreeCommand(ctx, handle,
		"--git-dir="+handle.gitDir.path, "--work-tree=.", "-c", "core.bare=false",
		"config", "--local", "--no-includes", "--name-only", "--list")
	if err != nil {
		return fmt.Errorf("prepare repository-local executable Git config inspection: %w", err)
	}
	cmd.Env = removeEnv(cmd.Env, "GIT_CONFIG")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect repository-local executable Git config: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if err := handle.RecheckPaths(); err != nil {
		return err
	}
	var rejected []string
	for _, key := range strings.Fields(string(out)) {
		if executableRepositoryConfig.MatchString(key) {
			rejected = append(rejected, key)
		}
	}
	if len(rejected) != 0 {
		return fmt.Errorf("existing worktree repository config contains executable settings: %s", strings.Join(rejected, ", "))
	}
	return nil
}

func removeEnv(env []string, key string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return result
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
	return legacyCacheForBarePath(barePath).getRemoteDefaultBranch(context.Background(), barePath)
}

func (c *Cache) getRemoteDefaultBranch(ctx context.Context, barePath string) string {
	handle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return ""
	}
	defer handle.Close()
	return c.getRemoteDefaultBranchHandle(ctx, handle)
}

func (c *Cache) getRemoteDefaultBranchHandle(ctx context.Context, handle *bareCacheHandle) string {
	// 1) Primary: refs/remotes/origin/HEAD set by `git remote set-head
	//    origin --auto` during ensureRemoteTrackingLayout. Verify the
	//    target actually exists — a partial set-head or a manually-broken
	//    repo can leave a symref pointing at a deleted ref, and returning
	//    it here would later fail in `git worktree add` with a confusing
	//    "invalid reference" error.
	if out, err := c.git.outputInBareCache(ctx, 0, handle, "symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(string(out))
		if ref != "" {
			if err := c.git.runInBareCache(ctx, 0, handle, "rev-parse", "--verify", ref); err == nil {
				return ref
			}
		}
	}
	// 2) Common default branch names under the origin namespace.
	for _, candidate := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master"} {
		if err := c.git.runInBareCache(ctx, 0, handle, "rev-parse", "--verify", candidate); err == nil {
			return candidate
		}
	}
	// 3) Use the bare repo's own HEAD as a hint. `git clone --bare` sets HEAD
	//    to the remote's default branch, so this reliably identifies custom
	//    default branch names (trunk, develop, ...) when set-head --auto
	//    didn't populate refs/remotes/origin/HEAD. We only return when the
	//    matching origin/<name> exists, so we still pick up up-to-date code
	//    rather than a stale local head.
	bareRef := c.bareHeadBranchHandle(ctx, handle)
	if bareRef != "" {
		originRef := "refs/remotes/origin/" + strings.TrimPrefix(bareRef, "refs/heads/")
		if err := c.git.runInBareCache(ctx, 0, handle, "rev-parse", "--verify", originRef); err == nil {
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
	if out, err := c.git.outputInBareCache(ctx, 0, handle, "for-each-ref", "--format=%(refname)", "refs/remotes/origin/"); err == nil {
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
	return legacyCacheForBarePath(barePath).bareHeadBranch(context.Background(), barePath)
}

func (c *Cache) bareHeadBranch(ctx context.Context, barePath string) string {
	handle, err := c.openValidatedBareCache(barePath)
	if err != nil {
		return ""
	}
	defer handle.Close()
	return c.bareHeadBranchHandle(ctx, handle)
}

func (c *Cache) bareHeadBranchHandle(ctx context.Context, handle *bareCacheHandle) string {
	out, err := c.git.outputInBareCache(ctx, 0, handle, "symbolic-ref", "HEAD")
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(out))
	if ref == "" {
		return ""
	}
	if err := c.git.runInBareCache(ctx, 0, handle, "rev-parse", "--verify", ref); err != nil {
		return ""
	}
	return ref
}

// multicaHookMarker is a sentinel comment embedded in every prepare-commit-msg
// hook installed by the daemon. removeCoAuthoredByHook uses it to recognize
// hooks it owns so it never deletes a hook installed by the user or another
// tool. Do not change without bumping the recognition logic.
const multicaHookMarker = "# multica:prepare-commit-msg:co-authored-by"

// daemonInstalledHookSignatures lists substrings that identify a
// prepare-commit-msg hook as one the daemon installed. removeCoAuthoredByHook
// treats a hook as Multica-owned if its content contains ANY of these
// substrings. The list deliberately includes the legacy comment that the
// daemon used before multicaHookMarker existed, so disabling the toggle on
// existing installations still cleans up old hooks seeded by previous daemon
// versions. Add to this list — never remove from it — so future tweaks to
// prepareCommitMsgHook keep recognizing every previously-shipped variant.
var daemonInstalledHookSignatures = []string{
	multicaHookMarker,
	"# Installed by the Multica daemon.",
}

// prepareCommitMsgHook is the prepare-commit-msg hook script that appends a
// Co-authored-by trailer for the Multica Agent to every commit message.
const prepareCommitMsgHook = `#!/bin/sh
# multica:prepare-commit-msg:co-authored-by
# Multica: add Co-authored-by trailer for the Multica Agent.
# Installed by the Multica daemon. Do not edit — it will be overwritten.

COMMIT_MSG_FILE="$1"
COMMIT_SOURCE="$2"

# Skip merge and squash commits.
case "$COMMIT_SOURCE" in
  merge|squash) exit 0 ;;
esac

TRAILER="Co-authored-by: multica-agent <github@multica.ai>"

# Don't add if already present.
if grep -qF "$TRAILER" "$COMMIT_MSG_FILE"; then
  exit 0
fi

# Use git interpret-trailers for proper formatting.
git interpret-trailers --in-place --trailer "$TRAILER" "$COMMIT_MSG_FILE"
`

// installCoAuthoredByHook installs a prepare-commit-msg git hook that appends
// a Co-authored-by trailer for the Multica Agent. The hook is installed in the
// git common directory (the bare repo for worktrees) so it applies to all
// worktrees created from this cache.
func installCoAuthoredByHook(worktreePath string) error {
	return legacyCache().installCoAuthoredByHook(context.Background(), worktreePath)
}

func (c *Cache) installCoAuthoredByHook(ctx context.Context, worktreePath string) error {
	out, err := c.git.output(ctx, 0, "-C", worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}
	return installCoAuthoredByHookInCommonDir(commonDir)
}

func installCoAuthoredByHookInCommonDir(commonDir string) error {
	hooksDir := filepath.Join(commonDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}

	hookPath := filepath.Join(hooksDir, "prepare-commit-msg")
	if err := os.WriteFile(hookPath, []byte(prepareCommitMsgHook), 0o755); err != nil {
		return fmt.Errorf("write prepare-commit-msg hook: %w", err)
	}
	return nil
}

// isDaemonInstalledHook reports whether a prepare-commit-msg hook on disk was
// installed by the Multica daemon (current or any previously released
// version). It returns false for hooks that don't carry any known daemon
// signature, so a user-installed hook at the same path is left alone.
func isDaemonInstalledHook(contents []byte) bool {
	body := string(contents)
	for _, sig := range daemonInstalledHookSignatures {
		if strings.Contains(body, sig) {
			return true
		}
	}
	return false
}

// removeCoAuthoredByHook removes the prepare-commit-msg hook installed by
// installCoAuthoredByHook. It only deletes the file when the content matches
// a known daemon signature (current marker or any previously released hook
// content), so a user-installed prepare-commit-msg hook is never touched.
// Returns nil when no hook is present or when an unrelated hook occupies
// the path.
func removeCoAuthoredByHook(worktreePath string) error {
	return legacyCache().removeCoAuthoredByHook(context.Background(), worktreePath)
}

func (c *Cache) removeCoAuthoredByHook(ctx context.Context, worktreePath string) error {
	out, err := c.git.output(ctx, 0, "-C", worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}
	return removeCoAuthoredByHookInCommonDir(commonDir)
}

func removeCoAuthoredByHookInCommonDir(commonDir string) error {
	hookPath := filepath.Join(commonDir, "hooks", "prepare-commit-msg")
	contents, err := os.ReadFile(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read prepare-commit-msg hook: %w", err)
	}
	if !isDaemonInstalledHook(contents) {
		// Unrelated hook (user or third-party): leave it alone.
		return nil
	}
	if err := os.Remove(hookPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove prepare-commit-msg hook: %w", err)
	}
	return nil
}

// excludeFromGit adds a pattern to the worktree's .git/info/exclude file.
func excludeFromGit(worktreePath, pattern string) error {
	return legacyCache().excludeFromGit(context.Background(), worktreePath, pattern)
}

func (c *Cache) excludeFromGit(ctx context.Context, worktreePath, pattern string) error {
	out, err := c.git.output(ctx, 0, "-C", worktreePath, "rev-parse", "--git-dir")
	if err != nil {
		return fmt.Errorf("resolve git dir: %w", err)
	}

	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}
	return excludeFromGitDir(gitDir, pattern)
}

func excludeFromGitDir(gitDir, pattern string) error {
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

func worktreeDirName(rawURL string) (string, error) {
	if err := validateRepoURL(rawURL); err != nil {
		return "", err
	}
	_, repoPath := splitHostAndPath(strings.TrimRight(strings.TrimSpace(rawURL), "/"))
	base := repoPath
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".git")
	base, err := url.PathUnescape(base)
	if err != nil || !isSafeRepoName(base) {
		return "", fmt.Errorf("unsafe repo URL %q: invalid repository name", rawURL)
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(rawURL)))
	return fmt.Sprintf("%s-%x", base, digest[:6]), nil
}

// repoNameFromURL is retained for internal tests and legacy callers. New
// checkout paths use worktreeDirName so equal basenames cannot collide.
func repoNameFromURL(rawURL string) string {
	name, err := worktreeDirName(rawURL)
	if err != nil {
		return ""
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
