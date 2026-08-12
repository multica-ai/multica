// Package repocache manages bare git clone caches for workspace repositories.
// The daemon uses these caches as the source for creating per-task worktrees.
package repocache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
)

// gitEnv returns an environment for git subprocesses that contact remotes.
// It passes the full daemon environment so credential helpers (e.g. gh) can
// locate their config, and disables TTY prompting so auth failures produce
// clear errors instead of blocking on a non-existent terminal.
//
// safe.directory=* is set via GIT_CONFIG_* env vars so git trusts all
// directories regardless of ownership. The daemon manages its own bare
// caches and worktrees, so the ownership check adds no security value
// and breaks CI environments where the runner UID differs from the
// directory owner.
func gitEnv() []string {
	base := os.Environ()

	// Find the existing GIT_CONFIG_COUNT so we append at the next index
	// rather than overwriting any env-scoped git config (auth, URL
	// rewrites, extra headers, etc.).
	existing := 0
	for _, e := range base {
		if strings.HasPrefix(e, "GIT_CONFIG_COUNT=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(e, "GIT_CONFIG_COUNT=")); err == nil {
				existing = n
			}
		}
	}

	idx := strconv.Itoa(existing)
	return append(base,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT="+strconv.Itoa(existing+1),
		"GIT_CONFIG_KEY_"+idx+"=safe.directory",
		"GIT_CONFIG_VALUE_"+idx+"=*",
	)
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

func newGitCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = gitEnv()
	return cmd
}

func runGitCombinedOutput(args ...string) ([]byte, error) {
	return runGitCombinedOutputContext(context.Background(), args...)
}

func runGitCombinedOutputContext(ctx context.Context, args ...string) ([]byte, error) {
	return runGitCombinedOutputWithTimeoutContext(ctx, repoCacheGitTimeout, args...)
}

func runGitCombinedOutputWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	return runGitCombinedOutputWithTimeoutContext(context.Background(), timeout, args...)
}

func runGitCombinedOutputWithTimeoutContext(parent context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := newGitCommand(args...)
	out, err := processtree.CombinedOutput(ctx, cmd, 5*time.Second)
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("git command timed out after %s: %w", timeout, ctx.Err())
	}
	return out, err
}

func runGitOutput(args ...string) ([]byte, error) {
	return runGitOutputContext(context.Background(), args...)
}

func runGitOutputContext(ctx context.Context, args ...string) ([]byte, error) {
	return runGitOutputWithTimeoutContext(ctx, repoCacheGitTimeout, args...)
}

func runGitOutputWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	return runGitOutputWithTimeoutContext(context.Background(), timeout, args...)
}

func runGitOutputWithTimeoutContext(parent context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := newGitCommand(args...)
	out, err := processtree.Output(ctx, cmd, 5*time.Second)
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("git command timed out after %s: %w", timeout, ctx.Err())
	}
	return out, err
}

func runGit(args ...string) error {
	return runGitContext(context.Background(), args...)
}

func runGitContext(ctx context.Context, args ...string) error {
	return runGitWithTimeoutContext(ctx, repoCacheGitTimeout, args...)
}

func runGitWithTimeout(timeout time.Duration, args ...string) error {
	return runGitWithTimeoutContext(context.Background(), timeout, args...)
}

func runGitWithTimeoutContext(parent context.Context, timeout time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := newGitCommand(args...)
	err := processtree.Run(ctx, cmd, 5*time.Second)
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("git command timed out after %s: %w", timeout, ctx.Err())
	}
	return err
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

// Cache manages bare git clones for workspace repositories.
type Cache struct {
	root   string // base directory for all caches (e.g. ~/multica_workspaces/.repos)
	logger *slog.Logger
	// repoLocks maps bare repo path → dedicated mutex. Any mutating operation
	// on a given bare repo (clone, fetch, worktree add, ref update) must
	// hold its lock — git's own lockfiles (packed-refs.lock, config.lock,
	// worktree admin dirs) don't tolerate parallel mutations on the same
	// repo. Separate repos are independent and run concurrently.
	repoLocks sync.Map // barePath -> *repoLock
}

// ErrRepoBusy means a foreground checkout could not acquire its repository
// within the caller's bounded wait. Callers that advertised retry support can
// turn this into a retryable HTTP response instead of waiting until their
// transport deadline expires.
var ErrRepoBusy = errors.New("repository is busy")

// Activity is the path-free repository coordination state exposed through the
// daemon health endpoint. It is diagnostic only.
type Activity struct {
	MaintenanceActive int
	ForegroundWaiters int
}

// repoLock is a foreground-priority mutex. Ordinary cache mutations serialize
// exactly as they did with sync.Mutex. Low-priority maintenance is different:
// it only starts on an idle repository and receives a context that is cancelled
// as soon as a foreground operation queues. The maintenance holder remains
// responsible for stopping its Git process tree before unlocking.
type repoLock struct {
	mu                sync.Mutex
	held              bool
	maintenance       bool
	maintenanceCancel context.CancelCauseFunc
	foregroundWaiters int
	changed           chan struct{}
}

// ErrMaintenancePreempted is the cancellation cause delivered to a running
// maintenance callback when foreground work needs the same repository.
var ErrMaintenancePreempted = errors.New("repository maintenance preempted by foreground work")

func newRepoLock() *repoLock {
	return &repoLock{changed: make(chan struct{})}
}

func (l *repoLock) LockContext(ctx context.Context) error {
	l.mu.Lock()
	l.foregroundWaiters++
	for {
		if err := ctx.Err(); err != nil {
			l.foregroundWaiters--
			l.signalLocked()
			l.mu.Unlock()
			return context.Cause(ctx)
		}
		if l.maintenanceCancel != nil {
			l.maintenanceCancel(ErrMaintenancePreempted)
		}
		if !l.held {
			l.held = true
			l.maintenance = false
			l.foregroundWaiters--
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		l.mu.Lock()
	}
}

func (l *repoLock) Lock() {
	_ = l.LockContext(context.Background())
}

func (l *repoLock) Unlock() {
	l.mu.Lock()
	if !l.held {
		l.mu.Unlock()
		panic("repocache: unlock of unlocked repository")
	}
	if l.maintenanceCancel != nil {
		l.maintenanceCancel(nil)
		l.maintenanceCancel = nil
	}
	l.held = false
	l.maintenance = false
	l.signalLocked()
	l.mu.Unlock()
}

func (l *repoLock) tryLockMaintenance(parent context.Context) (context.Context, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if parent.Err() != nil || l.held || l.foregroundWaiters > 0 {
		return nil, false
	}
	ctx, cancel := context.WithCancelCause(parent)
	l.held = true
	l.maintenance = true
	l.maintenanceCancel = cancel
	return ctx, true
}

func (l *repoLock) signalLocked() {
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *repoLock) activity() (maintenance bool, waiters int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held && l.maintenance, l.foregroundWaiters
}

func (l *repoLock) cancelMaintenanceAndWait() {
	l.mu.Lock()
	for l.maintenance {
		if l.maintenanceCancel != nil {
			l.maintenanceCancel(ErrMaintenancePreempted)
		}
		changed := l.changed
		l.mu.Unlock()
		<-changed
		l.mu.Lock()
	}
	l.mu.Unlock()
}

// New creates a new repo cache rooted at the given directory.
func New(root string, logger *slog.Logger) *Cache {
	return &Cache{root: root, logger: logger}
}

// lockForRepo returns the mutex dedicated to the given bare repo path. See
// the Cache.repoLocks field comment for semantics.
func (c *Cache) lockForRepo(barePath string) *repoLock {
	if l, ok := c.repoLocks.Load(barePath); ok {
		return l.(*repoLock)
	}
	newLock := newRepoLock()
	actual, _ := c.repoLocks.LoadOrStore(barePath, newLock)
	return actual.(*repoLock)
}

// Activity reports aggregate repository coordination without exposing cache
// paths. It is safe to call while operations are starting or finishing.
func (c *Cache) Activity() Activity {
	var activity Activity
	c.repoLocks.Range(func(_, value any) bool {
		maintenance, waiters := value.(*repoLock).activity()
		if maintenance {
			activity.MaintenanceActive++
		}
		activity.ForegroundWaiters += waiters
		return true
	})
	return activity
}

// CancelMaintenance stops every active low-priority repository operation and
// waits for it to release repository ownership. Task dispatch uses this as a
// barrier before launching an agent because a task may reuse an existing
// worktree and never pass through CreateWorktree's gate. Waiting also ensures
// interrupted-maintenance lock cleanup completes before direct agent Git work
// can create its own lock files.
func (c *Cache) CancelMaintenance() {
	c.repoLocks.Range(func(_, value any) bool {
		value.(*repoLock).cancelMaintenanceAndWait()
		return true
	})
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

// SyncContext is Sync with cancellation propagated through repo lock waits,
// clone, fetch, and ref-layout migration.
func (c *Cache) SyncContext(ctx context.Context, workspaceID string, repos []RepoInfo) error {
	wsDir := filepath.Join(c.root, workspaceID)
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return fmt.Errorf("create workspace cache dir: %w", err)
	}

	var firstErr error
	for _, repo := range repos {
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		if repo.URL == "" {
			continue
		}
		barePath := filepath.Join(wsDir, bareDirName(repo.URL))

		repoLock := c.lockForRepo(barePath)
		if err := repoLock.LockContext(ctx); err != nil {
			return err
		}
		if isBareRepo(barePath) {
			// Already cached — fetch latest.
			c.logger.Info("repo cache: fetching", "url", repo.URL, "path", barePath)
			if err := gitFetchContext(ctx, barePath); err != nil {
				c.logger.Warn("repo cache: fetch failed", "url", repo.URL, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		} else {
			// Not cached — bare clone.
			c.logger.Info("repo cache: cloning", "url", repo.URL, "path", barePath)
			if err := gitCloneBareContext(ctx, repo.URL, barePath); err != nil {
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
	barePath := c.BarePath(workspaceID, url)
	if isBareRepo(barePath) {
		return barePath
	}
	return ""
}

// BarePath returns where a repo's bare cache lives, whether or not it exists
// yet. Lookup is the "is it cached?" question; this is the "where would it be?"
// question, which the GC needs to map a set of live repo URLs onto the
// directories it is about to consider evicting.
func (c *Cache) BarePath(workspaceID, url string) string {
	return filepath.Join(c.root, workspaceID, bareDirName(url))
}

// lastUsedFile records the last time a task asked for a worktree from this
// bare repo. It lives inside the bare repo so it is removed with it.
//
// Directory mtime cannot answer this question. Every daemon restart re-syncs
// each registered workspace's full repo list, and that path fetches every
// cached repo (see Sync), refreshing the mtime of repos no task has checked
// out in months. atime is worse: noatime is common on Linux and Windows
// disables it by default. So the signal has to be written explicitly, at the
// one place that means a repo was really used — CreateWorktree.
const lastUsedFile = ".multica_last_used"

// MarkUsed records that this bare repo was just used for a checkout. Callers
// must already hold the repo lock. Best-effort: a failed stamp only risks the
// repo looking idle later, and the GC's own missing-stamp grace period
// (see LastUsed) absorbs that.
func MarkUsed(barePath string, logger *slog.Logger) {
	if barePath == "" {
		return
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if err := os.WriteFile(filepath.Join(barePath, lastUsedFile), []byte(stamp), 0o644); err != nil && logger != nil {
		logger.Warn("repo cache: write last-used stamp failed", "repo", barePath, "error", err)
	}
}

// LastUsed reports when this bare repo was last used for a checkout, and
// whether a stamp existed at all.
//
// ok=false means "unknown", never "ancient". Every cache created before this
// stamp existed reports unknown, so treating it as infinitely old would make
// the first GC cycle after an upgrade wipe every repo cache on the machine and
// force a full re-clone of each. Callers must stamp an unknown repo and let it
// age from now.
func LastUsed(barePath string) (time.Time, bool) {
	data, err := os.ReadFile(filepath.Join(barePath, lastUsedFile))
	if err != nil {
		return time.Time{}, false
	}
	stamp, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}, false
	}
	return stamp, true
}

// WithRepoLock serializes caller-supplied mutations on a bare repo against all
// other same-repo operations that use the cache's lock (Sync, Fetch,
// CreateWorktree, and daemon GC maintenance).
func (c *Cache) WithRepoLock(barePath string, fn func() error) error {
	return c.WithRepoLockContext(context.Background(), barePath, fn)
}

// WithRepoLockContext is the cancellable form of WithRepoLock.
func (c *Cache) WithRepoLockContext(ctx context.Context, barePath string, fn func() error) error {
	repoLock := c.lockForRepo(barePath)
	if err := repoLock.LockContext(ctx); err != nil {
		return err
	}
	defer repoLock.Unlock()
	return fn()
}

// WithRepoMaintenance runs fn only when the repository is idle. A foreground
// waiter cancels the context passed to fn, then waits for fn to stop its process
// tree and release the repository. ran=false means maintenance was skipped
// because foreground work already owned or was waiting for the repository.
func (c *Cache) WithRepoMaintenance(ctx context.Context, barePath string, fn func(context.Context) error) (ran bool, err error) {
	repoLock := c.lockForRepo(barePath)
	maintenanceCtx, ok := repoLock.tryLockMaintenance(ctx)
	if !ok {
		return false, nil
	}
	defer repoLock.Unlock()
	return true, fn(maintenanceCtx)
}

// Fetch runs `git fetch origin` on a cached bare clone to get latest refs.
func (c *Cache) Fetch(barePath string) error {
	return c.WithRepoLock(barePath, func() error {
		return gitFetch(barePath)
	})
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
		name = "repo.git"
	}
	return name
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
	return gitCloneBareContext(context.Background(), url, dest)
}

func gitCloneBareContext(ctx context.Context, url, dest string) error {
	if out, err := runGitCombinedOutputContext(ctx, "clone", "--bare", url, dest); err != nil {
		// Clean up partial clone.
		os.RemoveAll(dest)
		return fmt.Errorf("git clone --bare: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// `git clone --bare` populates refs/heads/* as a snapshot and defaults to
	// a mirror-style fetch refspec. Convert the bare repo to the standard
	// remote-tracking layout immediately so subsequent fetches write to
	// refs/remotes/origin/* and can't conflict with worktree-locked heads.
	if err := ensureRemoteTrackingLayoutContext(ctx, dest); err != nil {
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
	return gitFetchContext(context.Background(), barePath)
}

func gitFetchContext(ctx context.Context, barePath string) error {
	if err := ensureRemoteTrackingLayoutContext(ctx, barePath); err != nil {
		return fmt.Errorf("ensure refspec: %w", err)
	}
	if err := runGitFetchContext(ctx, barePath); err != nil {
		return err
	}
	// Refresh refs/remotes/origin/HEAD after every successful fetch.
	// set-head --auto is lightweight (a single ls-remote HEAD round-trip)
	// and non-fatal: if it fails we still have the step 2-5 fallbacks in
	// getRemoteDefaultBranch, but the modern-cache default-branch-change
	// path (the only path that can't be recovered any other way) relies
	// on this call.
	_ = runGitContext(ctx, "-C", barePath, "remote", "set-head", "origin", "--auto")
	return nil
}

// runGitFetch is the raw `git fetch origin` wrapper. Callers should go through
// gitFetch, which migrates legacy caches first.
func runGitFetch(barePath string) error {
	return runGitFetchContext(context.Background(), barePath)
}

func runGitFetchContext(ctx context.Context, barePath string) error {
	if out, err := runGitCombinedOutputContext(ctx, "-C", barePath, "fetch", "origin"); err != nil {
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
	return ensureRemoteTrackingLayoutContext(context.Background(), barePath)
}

func ensureRemoteTrackingLayoutContext(ctx context.Context, barePath string) error {
	cur, err := readFetchRefspecContext(ctx, barePath)
	if err != nil {
		return err
	}
	if cur == modernFetchRefspec || cur == strings.TrimPrefix(modernFetchRefspec, "+") {
		return nil // already modern
	}
	if err := setFetchRefspecContext(ctx, barePath, modernFetchRefspec); err != nil {
		return err
	}
	// Backfill refs/remotes/origin/* by fetching with the new refspec. This
	// writes to the origin/* namespace, so even worktree-locked refs/heads/*
	// branches can't collide.
	if err := runGitFetchContext(ctx, barePath); err != nil {
		return fmt.Errorf("backfill fetch after refspec migration: %w", err)
	}
	// Set refs/remotes/origin/HEAD so getRemoteDefaultBranch can read it.
	// Non-fatal: if this fails we fall back to origin/main, origin/master.
	_ = runGitContext(ctx, "-C", barePath, "remote", "set-head", "origin", "--auto")
	return nil
}

// readFetchRefspec returns the current remote.origin.fetch config value, or
// the empty string if it's not set. Distinguishes "missing" (exit 1) from
// real git errors.
func readFetchRefspec(barePath string) (string, error) {
	return readFetchRefspecContext(context.Background(), barePath)
}

func readFetchRefspecContext(ctx context.Context, barePath string) (string, error) {
	out, err := runGitOutputContext(ctx, "-C", barePath, "config", "--get", "remote.origin.fetch")
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "", nil // key missing, not an error
		}
		return "", fmt.Errorf("read remote.origin.fetch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func setFetchRefspec(barePath, refspec string) error {
	return setFetchRefspecContext(context.Background(), barePath, refspec)
}

func setFetchRefspecContext(ctx context.Context, barePath, refspec string) error {
	out, err := runGitCombinedOutputContext(ctx, "-C", barePath, "config", "remote.origin.fetch", refspec)
	if err != nil {
		return fmt.Errorf("set remote.origin.fetch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
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
	// LockWaitTimeout bounds only the wait for another same-repository
	// operation. Zero preserves the historical unbounded wait for internal and
	// older callers; retry-aware HTTP checkout requests set a finite value.
	LockWaitTimeout time.Duration
	// IsolatedGitMetadata creates a local clone whose .git directory lives
	// inside WorkDir instead of a linked worktree whose gitdir lives under the
	// shared cache. Codex tasks need this because workspace-write keeps a
	// resolved external worktree gitdir read-only even when it is explicitly
	// listed as a writable root — on Linux (multica-ai/multica#2925) and on the
	// Windows native sandbox (multica-ai/multica#6449).
	IsolatedGitMetadata bool
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

// CreateWorktreeContext is CreateWorktree with cancellation propagated through
// lock acquisition and every Git subprocess. Git work begins only after the
// lock is held, so a client that times out behind maintenance cannot leave a
// late, unwanted checkout.
func (c *Cache) CreateWorktreeContext(ctx context.Context, params WorktreeParams) (*WorktreeResult, error) {
	barePath := c.Lookup(params.WorkspaceID, params.RepoURL)
	if barePath == "" {
		return nil, fmt.Errorf("repo not found in cache: %s (workspace: %s)", params.RepoURL, params.WorkspaceID)
	}

	// Serialize concurrent CreateWorktree calls on the same bare repo. Git's
	// own lockfiles (packed-refs.lock, config.lock, worktree admin dirs)
	// can't tolerate parallel fetch + worktree mutations on the same repo.
	repoLock := c.lockForRepo(barePath)
	lockCtx := ctx
	cancel := func() {}
	if params.LockWaitTimeout > 0 {
		lockCtx, cancel = context.WithTimeout(ctx, params.LockWaitTimeout)
	}
	err := repoLock.LockContext(lockCtx)
	cancel()
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %s", ErrRepoBusy, params.RepoURL)
		}
		return nil, err
	}
	defer repoLock.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}

	// Stamp before doing the work, not after: a task asking for a worktree is
	// what "this cache is still wanted" means, whether or not the checkout
	// ultimately succeeds. Stamping only on success would let a repo whose
	// checkouts keep failing age out from under the tasks still trying to use it.
	MarkUsed(barePath, c.logger)

	// Fetch latest from origin. This also migrates the bare cache's refspec
	// to the modern remote-tracking layout on first run, so subsequent fetches
	// never collide with the refs/heads/agent/* branches that worktree creation
	// locks in this same bare repo.
	if err := gitFetchContext(ctx, barePath); err != nil {
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		// Non-fatal: preserve cached state and continue, but make the warning
		// loud enough that it's findable in the daemon log. The agent will
		// receive an older snapshot than the remote head.
		c.logger.Warn("repo checkout: fetch failed, agent will see possibly stale code",
			"url", params.RepoURL,
			"error", err,
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}

	// Determine the ref to base the worktree on. By default this is the remote's
	// default branch (resolved internally via getRemoteDefaultBranch, which walks
	// origin/HEAD → origin/main, origin/master → bare-HEAD hint into origin/<same>
	// → single-entry scan of origin/* → bare HEAD when origin/* is empty).
	// Callers may request a specific branch, tag, or commit so review/QA agents
	// can inspect the exact revision without trying to mutate the daemon-owned
	// worktree metadata themselves.
	baseRef, err := resolveBaseRefContext(ctx, barePath, params.Ref)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
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

	// Derive directory name from repo URL.
	dirName := repoNameFromURL(params.RepoURL)
	worktreePath := filepath.Join(params.WorkDir, dirName)

	// Once a workdir has moved to isolated metadata, keep using that safer
	// shape even if a later task comes from an older CLI or a different runtime
	// that omits the mode hint. This also makes provider transitions on a reused
	// workdir backward compatible.
	if params.IsolatedGitMetadata || isIsolatedCheckoutContext(ctx, worktreePath) {
		actualBranch, err := c.createOrUpdateIsolatedCheckoutContext(
			ctx,
			barePath,
			params.RepoURL,
			worktreePath,
			branchName,
			baseRef,
		)
		if err != nil {
			return nil, fmt.Errorf("create isolated checkout: %w", err)
		}

		for _, pattern := range agentGitExcludePatterns {
			_ = excludeFromGitContext(ctx, worktreePath, pattern)
		}
		if params.CoAuthoredByEnabled {
			if err := installCoAuthoredByHookContext(ctx, worktreePath); err != nil {
				c.logger.Warn("repo checkout: install co-authored-by hook failed (non-fatal)", "error", err)
			}
		} else {
			if err := removeCoAuthoredByHookContext(ctx, worktreePath); err != nil {
				c.logger.Warn("repo checkout: remove co-authored-by hook failed (non-fatal)", "error", err)
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, context.Cause(ctx)
		}

		c.logger.Info("repo checkout: isolated checkout ready",
			"url", params.RepoURL,
			"path", worktreePath,
			"branch", actualBranch,
			"base", baseRef,
		)
		return &WorktreeResult{Path: worktreePath, BranchName: actualBranch}, nil
	}

	// If worktree already exists (reused environment from a prior task),
	// update it to the latest remote code instead of creating a new one.
	if isGitWorktree(worktreePath) {
		actualBranch, err := updateExistingWorktreeContext(ctx, worktreePath, branchName, baseRef)
		if err != nil {
			return nil, fmt.Errorf("update existing worktree: %w", err)
		}

		for _, pattern := range agentGitExcludePatterns {
			_ = excludeFromGitContext(ctx, worktreePath, pattern)
		}

		// Install or remove the Co-authored-by hook based on the workspace
		// setting. The hook lives in the bare repo's shared hooks dir, so we
		// must actively remove it when disabled — otherwise a previously
		// installed hook keeps appending the trailer to every commit even
		// after the user toggles the setting off.
		if params.CoAuthoredByEnabled {
			if err := installCoAuthoredByHookContext(ctx, worktreePath); err != nil {
				c.logger.Warn("repo checkout: install co-authored-by hook failed (non-fatal)", "error", err)
			}
		} else {
			if err := removeCoAuthoredByHookContext(ctx, worktreePath); err != nil {
				c.logger.Warn("repo checkout: remove co-authored-by hook failed (non-fatal)", "error", err)
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, context.Cause(ctx)
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
	actualBranch, err := createWorktreeContext(ctx, barePath, worktreePath, branchName, baseRef)
	if err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}

	// Exclude agent context files from git tracking.
	for _, pattern := range agentGitExcludePatterns {
		_ = excludeFromGitContext(ctx, worktreePath, pattern)
	}

	// Install or remove the Co-authored-by hook based on the workspace
	// setting. See the existing-worktree branch above for why removal is
	// required when the setting is disabled.
	if params.CoAuthoredByEnabled {
		if err := installCoAuthoredByHookContext(ctx, worktreePath); err != nil {
			c.logger.Warn("repo checkout: install co-authored-by hook failed (non-fatal)", "error", err)
		}
	} else {
		if err := removeCoAuthoredByHookContext(ctx, worktreePath); err != nil {
			c.logger.Warn("repo checkout: remove co-authored-by hook failed (non-fatal)", "error", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
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

const (
	isolatedCheckoutConfigKey   = "multica.checkout-mode"
	isolatedCheckoutConfigValue = "isolated"
	isolatedCacheRemoteName     = "multica-cache"
)

// createOrUpdateIsolatedCheckout keeps Git metadata inside the task workdir.
// The fresh path uses a local clone, so immutable Git objects are hard-linked
// (copied on Windows, see localCloneArgs) from the daemon's cache while refs,
// index, logs, config, and new objects remain private to the task checkout.
// The temporary cache remote is then replaced with the real repository URL so
// an agent's normal fetch / push commands still target GitHub rather than the
// daemon-owned bare cache.
func (c *Cache) createOrUpdateIsolatedCheckout(barePath, repoURL, checkoutPath, branchName, baseRef string) (string, error) {
	return c.createOrUpdateIsolatedCheckoutContext(context.Background(), barePath, repoURL, checkoutPath, branchName, baseRef)
}

func (c *Cache) createOrUpdateIsolatedCheckoutContext(ctx context.Context, barePath, repoURL, checkoutPath, branchName, baseRef string) (string, error) {
	baseCommit, err := resolveCommitContext(ctx, barePath, baseRef)
	if err != nil {
		return "", err
	}

	if isIsolatedCheckoutContext(ctx, checkoutPath) {
		if err := setIsolatedCheckoutOriginContext(ctx, checkoutPath, repoURL); err != nil {
			return "", err
		}
		// Idempotent, and required for a workdir that was first created while
		// the cache was still a full clone: without it, a checkout backed by a
		// blobless cache resolves missing blobs to nothing instead of fetching.
		if isPartialCloneContext(ctx, barePath) {
			if err := configurePromisorRemoteContext(ctx, checkoutPath); err != nil {
				return "", err
			}
		}
		if err := syncIsolatedCheckoutRefsContext(ctx, barePath, checkoutPath, baseRef); err != nil {
			return "", err
		}
		actualBranch, err := updateExistingWorktreeContext(ctx, checkoutPath, branchName, baseCommit)
		if err != nil {
			return "", err
		}
		// Drop earlier tasks' agent/* heads so a reused workdir doesn't grow a
		// new local branch on every checkout. Non-fatal: leftover branches are
		// harmless clutter and must never fail the checkout.
		if err := deleteStaleAgentBranchesContext(ctx, checkoutPath, actualBranch); err != nil {
			c.logger.Warn("repo checkout: prune stale branches failed (non-fatal)", "error", err)
		}
		return actualBranch, nil
	}
	// A daemon upgrade can resume a pre-fix Codex workdir that still has a
	// linked worktree. Remove it through Git (so the shared admin record is
	// cleaned too), then recreate the same checkout path with local metadata.
	if isGitWorktree(checkoutPath) {
		if err := removeLinkedWorktreeContext(ctx, barePath, checkoutPath); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(checkoutPath); err == nil {
		return "", fmt.Errorf("checkout path already exists and is not a Multica isolated checkout: %s", checkoutPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat checkout path: %w", err)
	}

	return createIsolatedCheckoutContext(ctx, barePath, repoURL, checkoutPath, branchName, baseRef, baseCommit)
}

func removeLinkedWorktree(barePath, checkoutPath string) error {
	return removeLinkedWorktreeContext(context.Background(), barePath, checkoutPath)
}

func removeLinkedWorktreeContext(ctx context.Context, barePath, checkoutPath string) error {
	out, err := runGitOutputContext(ctx, "-C", checkoutPath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve linked worktree common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(checkoutPath, commonDir)
	}
	if !sameResolvedPath(commonDir, barePath) {
		return fmt.Errorf("linked worktree common dir %s does not match cache %s", commonDir, barePath)
	}
	if out, err := runGitCombinedOutputContext(ctx, "-C", barePath, "worktree", "remove", "--force", checkoutPath); err != nil {
		return fmt.Errorf("remove linked worktree: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// sameResolvedPath reports whether a and b denote the same location after
// resolving to absolute, symlink-free, cleaned form. It is a path-equality
// check (not a same-device check): the migration guard uses it to confirm a
// linked worktree's git-common-dir really points at this cache bare repo
// before removing the worktree.
func sameResolvedPath(a, b string) bool {
	clean := func(path string) string {
		abs, err := filepath.Abs(path)
		if err == nil {
			path = abs
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		return filepath.Clean(path)
	}
	return clean(a) == clean(b)
}

// localCloneArgs builds the `git clone --local` invocation that seeds a fresh
// isolated checkout from the shared bare cache.
//
// On Windows the clone also passes --no-hardlinks. A local clone hardlinks
// .git/objects whenever it can, but an NTFS hard link only exists within a
// single volume and every link shares one underlying file *and* one security
// descriptor. Copying the objects instead keeps a cache and workdir that live
// on different drives working, and stops a task checkout — whose tree the
// sandbox makes writable — from re-permissioning the daemon-owned cache's
// object files. The cost is extra disk and a slower first checkout on Windows.
func localCloneArgs(goos, barePath, checkoutPath string) []string {
	args := []string{"clone", "--local", "--no-checkout", "--no-tags"}
	if goos == "windows" {
		args = append(args, "--no-hardlinks")
	}
	return append(args, "--origin", isolatedCacheRemoteName, barePath, checkoutPath)
}

func createIsolatedCheckout(barePath, repoURL, checkoutPath, branchName, baseRef, baseCommit string) (_ string, retErr error) {
	return createIsolatedCheckoutContext(context.Background(), barePath, repoURL, checkoutPath, branchName, baseRef, baseCommit)
}

func createIsolatedCheckoutContext(ctx context.Context, barePath, repoURL, checkoutPath, branchName, baseRef, baseCommit string) (_ string, retErr error) {
	if out, err := runGitCombinedOutputContext(
		ctx,
		localCloneArgs(runtime.GOOS, barePath, checkoutPath)...,
	); err != nil {
		// Do not remove checkoutPath here. A different repository with the
		// same basename could have won the path race after our pre-check; Git
		// then fails safely, and deleting the path would destroy its checkout.
		return "", fmt.Errorf("git clone --local: %s: %w", strings.TrimSpace(string(out)), err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(checkoutPath)
		}
	}()

	// The origin swap has to happen before the first checkout when the cache
	// is a partial clone. `git clone --local` hardlinks the objects it can see
	// and does NOT inherit the promisor configuration, so a blobless cache
	// yields a checkout whose blobs are unreachable — and git reports that as
	// success with every file "deleted" rather than as an error. Pointing
	// origin at the real remote and restoring the promisor config first lets
	// the checkout below lazily fetch what it needs.
	if out, err := runGitCombinedOutputContext(ctx, "-C", checkoutPath, "remote", "remove", isolatedCacheRemoteName); err != nil {
		return "", fmt.Errorf("remove cache remote: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := runGitCombinedOutputContext(ctx, "-C", checkoutPath, "remote", "add", "origin", repoURL); err != nil {
		return "", fmt.Errorf("add origin remote: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if isPartialCloneContext(ctx, barePath) {
		if err := configurePromisorRemoteContext(ctx, checkoutPath); err != nil {
			return "", err
		}
	}

	if out, err := runGitCombinedOutputContext(ctx, "-C", checkoutPath, "checkout", "--detach", baseCommit); err != nil {
		return "", fmt.Errorf("git checkout --detach: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if err := deleteAllLocalBranchesContext(ctx, checkoutPath); err != nil {
		return "", err
	}
	if err := syncIsolatedCheckoutRefsContext(ctx, barePath, checkoutPath, baseRef); err != nil {
		return "", err
	}
	if out, err := runGitCombinedOutputContext(ctx, "-C", checkoutPath, "config", isolatedCheckoutConfigKey, isolatedCheckoutConfigValue); err != nil {
		return "", fmt.Errorf("mark isolated checkout: %s: %w", strings.TrimSpace(string(out)), err)
	}

	actualBranch, err := checkoutNewBranchContext(ctx, checkoutPath, branchName, baseCommit)
	if err != nil {
		return "", err
	}
	cleanup = false
	return actualBranch, nil
}

func resolveCommit(repoPath, ref string) (string, error) {
	return resolveCommitContext(context.Background(), repoPath, ref)
}

func resolveCommitContext(ctx context.Context, repoPath, ref string) (string, error) {
	out, err := runGitOutputContext(ctx, "-C", repoPath, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve checkout base %q: %w", ref, err)
	}
	commit := strings.TrimSpace(string(out))
	if commit == "" {
		return "", fmt.Errorf("resolve checkout base %q: empty commit", ref)
	}
	return commit, nil
}

func isIsolatedCheckout(path string) bool {
	return isIsolatedCheckoutContext(context.Background(), path)
}

func isIsolatedCheckoutContext(ctx context.Context, path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil || !info.IsDir() {
		return false
	}
	out, err := runGitOutputContext(ctx, "-C", path, "config", "--get", isolatedCheckoutConfigKey)
	return err == nil && strings.TrimSpace(string(out)) == isolatedCheckoutConfigValue
}

// partialCloneFilter is the object filter a blobless partial clone is created
// with, and the value that has to be restored on any repository that inherits
// such a clone's incomplete object store.
const partialCloneFilter = "blob:none"

// isPartialClone reports whether a repository was created as a partial clone,
// i.e. whether git will lazily fetch missing objects from its promisor remote.
func isPartialClone(repoPath string) bool {
	return isPartialCloneContext(context.Background(), repoPath)
}

func isPartialCloneContext(ctx context.Context, repoPath string) bool {
	out, err := runGitOutputWithTimeoutContext(ctx, 30*time.Second, "-C", repoPath, "config", "--get", "remote.origin.promisor")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// configurePromisorRemote marks origin as the promisor remote for a repository
// whose object store is incomplete, so git lazily fetches missing blobs from
// the real remote instead of failing. It mirrors the two config keys
// `git clone --filter=blob:none` writes; `git clone --local` does not copy
// them across, which is why they have to be restored by hand.
func configurePromisorRemote(repoPath string) error {
	return configurePromisorRemoteContext(context.Background(), repoPath)
}

func configurePromisorRemoteContext(ctx context.Context, repoPath string) error {
	settings := [][2]string{
		{"remote.origin.promisor", "true"},
		{"remote.origin.partialclonefilter", partialCloneFilter},
	}
	for _, kv := range settings {
		if out, err := runGitCombinedOutputContext(ctx, "-C", repoPath, "config", kv[0], kv[1]); err != nil {
			return fmt.Errorf("set %s: %s: %w", kv[0], strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

func setIsolatedCheckoutOrigin(path, repoURL string) error {
	return setIsolatedCheckoutOriginContext(context.Background(), path, repoURL)
}

func setIsolatedCheckoutOriginContext(ctx context.Context, path, repoURL string) error {
	out, err := runGitCombinedOutputContext(ctx, "-C", path, "remote", "set-url", "origin", repoURL)
	if err != nil {
		return fmt.Errorf("set origin remote: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// syncIsolatedCheckoutRefs mirrors the cache's real origin/* and tag refs into
// the task-local repository. It also fetches the selected base ref directly so
// a reused checkout can move to a newly-fetched commit without depending on
// the cache after this function returns.
func syncIsolatedCheckoutRefs(barePath, checkoutPath, baseRef string) error {
	return syncIsolatedCheckoutRefsContext(context.Background(), barePath, checkoutPath, baseRef)
}

func syncIsolatedCheckoutRefsContext(ctx context.Context, barePath, checkoutPath, baseRef string) error {
	refspecs := []string{
		"+refs/remotes/origin/*:refs/remotes/origin/*",
		"+refs/tags/*:refs/tags/*",
	}
	args := []string{"-C", checkoutPath, "fetch", "--force", "--no-tags", barePath}
	args = append(args, refspecs...)
	if out, err := runGitCombinedOutputContext(ctx, args...); err != nil {
		return fmt.Errorf("sync cache refs: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := runGitCombinedOutputContext(ctx, "-C", checkoutPath, "fetch", "--force", "--no-tags", barePath, baseRef); err != nil {
		return fmt.Errorf("fetch checkout base: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// deleteAllLocalBranches removes the heads copied from the bare cache into a
// fresh local clone. The clone is detached and has no task-created branches to
// preserve yet.
func deleteAllLocalBranches(repoPath string) error {
	return deleteAllLocalBranchesContext(context.Background(), repoPath)
}

func deleteAllLocalBranchesContext(ctx context.Context, repoPath string) error {
	return deleteLocalBranchesUnderContext(ctx, repoPath, "refs/heads/", "")
}

// deleteStaleAgentBranches prunes branches left by earlier Multica tasks while
// preserving the current task branch and every user-created local branch.
func deleteStaleAgentBranches(repoPath, keepBranch string) error {
	return deleteStaleAgentBranchesContext(context.Background(), repoPath, keepBranch)
}

func deleteStaleAgentBranchesContext(ctx context.Context, repoPath, keepBranch string) error {
	return deleteLocalBranchesUnderContext(ctx, repoPath, "refs/heads/agent/", "refs/heads/"+keepBranch)
}

func deleteLocalBranchesUnder(repoPath, namespace, keepRef string) error {
	return deleteLocalBranchesUnderContext(context.Background(), repoPath, namespace, keepRef)
}

func deleteLocalBranchesUnderContext(ctx context.Context, repoPath, namespace, keepRef string) error {
	out, err := runGitOutputContext(ctx, "-C", repoPath, "for-each-ref", "--format=%(refname)", namespace)
	if err != nil {
		return fmt.Errorf("list local branches: %w", err)
	}
	for _, ref := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" || ref == keepRef {
			continue
		}
		if out, err := runGitCombinedOutputContext(ctx, "-C", repoPath, "update-ref", "-d", ref); err != nil {
			return fmt.Errorf("delete local branch %s: %s: %w", ref, strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

func checkoutNewBranch(repoPath, branchName, baseRef string) (string, error) {
	return checkoutNewBranchContext(context.Background(), repoPath, branchName, baseRef)
}

func checkoutNewBranchContext(ctx context.Context, repoPath, branchName, baseRef string) (string, error) {
	out, err := runGitCombinedOutputContext(ctx, "-C", repoPath, "checkout", "-b", branchName, baseRef)
	if err == nil {
		return branchName, nil
	}
	wrapped := fmt.Errorf("git checkout -b: %s: %w", strings.TrimSpace(string(out)), err)
	if !isBranchCollisionError(wrapped) {
		return "", wrapped
	}
	branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
	if out2, err2 := runGitCombinedOutputContext(ctx, "-C", repoPath, "checkout", "-b", branchName, baseRef); err2 != nil {
		return "", fmt.Errorf("git checkout -b (retry): %s: %w", strings.TrimSpace(string(out2)), err2)
	}
	return branchName, nil
}

func resolveBaseRef(barePath, requestedRef string) (string, error) {
	return resolveBaseRefContext(context.Background(), barePath, requestedRef)
}

func resolveBaseRefContext(ctx context.Context, barePath, requestedRef string) (string, error) {
	ref := strings.TrimSpace(requestedRef)
	if ref == "" {
		return getRemoteDefaultBranchContext(ctx, barePath), nil
	}

	// Prefer remote-tracking branches for human branch names. Then allow full
	// local refs, tags, and raw commits that exist in the fetched bare cache.
	candidates := []string{
		"refs/remotes/origin/" + ref,
		"refs/tags/" + ref,
		ref,
	}
	for _, candidate := range candidates {
		if gitRefExistsContext(ctx, barePath, candidate+"^{commit}") {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot resolve requested ref %q in repo cache at %s", ref, barePath)
}

func gitRefExists(repoPath, ref string) bool {
	return gitRefExistsContext(context.Background(), repoPath, ref)
}

func gitRefExistsContext(ctx context.Context, repoPath, ref string) bool {
	return runGitContext(ctx, "-C", repoPath, "rev-parse", "--verify", "--quiet", ref) == nil
}

// createWorktree creates a git worktree at the given path with a new branch.
// Returns the actual branch name used — which may differ from the requested
// branchName if a collision was resolved by appending a timestamp suffix.
func createWorktree(gitRoot, worktreePath, branchName, baseRef string) (string, error) {
	return createWorktreeContext(context.Background(), gitRoot, worktreePath, branchName, baseRef)
}

func createWorktreeContext(ctx context.Context, gitRoot, worktreePath, branchName, baseRef string) (string, error) {
	// Pre-check: if the worktree path already exists we would get a confusing
	// "already exists" error from `git worktree add` — which used to be
	// misclassified as a branch collision, causing the retry to leak branches
	// into the bare repo. Fail cleanly here instead. The caller is expected
	// to route reused workdirs through updateExistingWorktree via isGitWorktree.
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree path already exists and is not a valid git worktree: %s", worktreePath)
	}

	err := runWorktreeAddContext(ctx, gitRoot, worktreePath, branchName, baseRef)
	if err != nil && isBranchCollisionError(err) {
		// Branch name collision: append timestamp and retry once.
		branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
		err = runWorktreeAddContext(ctx, gitRoot, worktreePath, branchName, baseRef)
	}
	if err != nil {
		return "", err
	}
	return branchName, nil
}

func runWorktreeAdd(gitRoot, worktreePath, branchName, baseRef string) error {
	return runWorktreeAddContext(context.Background(), gitRoot, worktreePath, branchName, baseRef)
}

func runWorktreeAddContext(ctx context.Context, gitRoot, worktreePath, branchName, baseRef string) error {
	if out, err := runGitCombinedOutputContext(ctx, "-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath, baseRef); err != nil {
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
	return updateExistingWorktreeContext(context.Background(), worktreePath, branchName, baseRef)
}

func updateExistingWorktreeContext(ctx context.Context, worktreePath, branchName, baseRef string) (string, error) {
	// Discard any leftover uncommitted changes from the previous task.
	if out, err := runGitCombinedOutputContext(ctx, "-C", worktreePath, "reset", "--hard"); err != nil {
		return "", fmt.Errorf("git reset --hard: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Clean untracked files (e.g. build artifacts from previous task).
	if out, err := runGitCombinedOutputContext(ctx, "-C", worktreePath, "clean", "-fd"); err != nil {
		return "", fmt.Errorf("git clean -fd: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Create a new branch from the resolved default-branch ref and switch to
	// it. baseRef is a ref path returned by getRemoteDefaultBranch — usually
	// "refs/remotes/origin/<branch>" but may be "refs/heads/<branch>" on a
	// legacy/migration-pending cache. Either form is valid as a checkout
	// startpoint.
	out, err := runGitCombinedOutputContext(ctx, "-C", worktreePath, "checkout", "-b", branchName, baseRef)
	if err == nil {
		return branchName, nil
	}
	wrapped := fmt.Errorf("git checkout -b: %s: %w", strings.TrimSpace(string(out)), err)
	if !isBranchCollisionError(wrapped) {
		return "", wrapped
	}
	// Branch name collision: append timestamp and retry once.
	branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
	if out2, err2 := runGitCombinedOutputContext(ctx, "-C", worktreePath, "checkout", "-b", branchName, baseRef); err2 != nil {
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
	return getRemoteDefaultBranchContext(context.Background(), barePath)
}

func getRemoteDefaultBranchContext(ctx context.Context, barePath string) string {
	// 1) Primary: refs/remotes/origin/HEAD set by `git remote set-head
	//    origin --auto` during ensureRemoteTrackingLayout. Verify the
	//    target actually exists — a partial set-head or a manually-broken
	//    repo can leave a symref pointing at a deleted ref, and returning
	//    it here would later fail in `git worktree add` with a confusing
	//    "invalid reference" error.
	if out, err := runGitOutputContext(ctx, "-C", barePath, "symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(string(out))
		if ref != "" {
			if err := runGitContext(ctx, "-C", barePath, "rev-parse", "--verify", ref); err == nil {
				return ref
			}
		}
	}
	// 2) Common default branch names under the origin namespace.
	for _, candidate := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master"} {
		if err := runGitContext(ctx, "-C", barePath, "rev-parse", "--verify", candidate); err == nil {
			return candidate
		}
	}
	// 3) Use the bare repo's own HEAD as a hint. `git clone --bare` sets HEAD
	//    to the remote's default branch, so this reliably identifies custom
	//    default branch names (trunk, develop, ...) when set-head --auto
	//    didn't populate refs/remotes/origin/HEAD. We only return when the
	//    matching origin/<name> exists, so we still pick up up-to-date code
	//    rather than a stale local head.
	bareRef := bareHeadBranchContext(ctx, barePath)
	if bareRef != "" {
		originRef := "refs/remotes/origin/" + strings.TrimPrefix(bareRef, "refs/heads/")
		if err := runGitContext(ctx, "-C", barePath, "rev-parse", "--verify", originRef); err == nil {
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
	if out, err := runGitOutputContext(ctx, "-C", barePath, "for-each-ref", "--format=%(refname)", "refs/remotes/origin/"); err == nil {
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
	return bareHeadBranchContext(context.Background(), barePath)
}

func bareHeadBranchContext(ctx context.Context, barePath string) string {
	out, err := runGitOutputContext(ctx, "-C", barePath, "symbolic-ref", "HEAD")
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(out))
	if ref == "" {
		return ""
	}
	if err := runGitContext(ctx, "-C", barePath, "rev-parse", "--verify", ref); err != nil {
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
	return installCoAuthoredByHookContext(context.Background(), worktreePath)
}

func installCoAuthoredByHookContext(ctx context.Context, worktreePath string) error {
	out, err := runGitOutputContext(ctx, "-C", worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}

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
	return removeCoAuthoredByHookContext(context.Background(), worktreePath)
}

func removeCoAuthoredByHookContext(ctx context.Context, worktreePath string) error {
	out, err := runGitOutputContext(ctx, "-C", worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}

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
	return excludeFromGitContext(context.Background(), worktreePath, pattern)
}

func excludeFromGitContext(ctx context.Context, worktreePath, pattern string) error {
	out, err := runGitOutputContext(ctx, "-C", worktreePath, "rev-parse", "--git-dir")
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
