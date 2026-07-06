package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// WorktreePoolManager allocates per-task `git worktree` checkouts under a
// pool root so multiple agent tasks bound to the same local_directory can
// run in parallel instead of serialising on the shared working tree.
//
// Contract (MUL-3483 MVP):
//
//   - The pool is opt-in via `local_directory.mode = "worktree_pool"`.
//     When the ref stays on the default in_place mode the manager is
//     unused and the daemon retains the historical single-tree semantics.
//   - Every `git worktree add/remove/prune/submodule` invocation for a
//     given base repo is serialised through a per-repo mutex so
//     concurrent worker tasks never race on `.git/config.lock`
//     (Anthropic hit the same class of bug — claude-code#34645 — and
//     also settled on an in-process queue as the fix).
//   - Slots are counted by live leases rather than by inspecting
//     `git worktree list`, so a task that crashes without releasing does
//     not leak a slot forever: the daemon-crash case is handled by
//     `git worktree prune` on the next allocation attempt, and the
//     in-memory count is authoritative for the running daemon.
//   - Repos that ship initialised submodules are refused up front. The
//     BUGS section of git-worktree(1) is explicit that multi-checkout
//     support for superprojects is incomplete, and the shared
//     `$GIT_COMMON_DIR/config` writes from `git submodule` would still
//     bottleneck the pool anyway. Refusing early is friendlier than
//     surfacing a torn worktree once the agent has started running.
//   - Dirty worktrees are never `--force` removed. If the agent left
//     uncommitted changes behind we keep the directory (marking the
//     lease with the path so callers can surface it to the user) and
//     let the next `git worktree prune` reap the metadata once the
//     directory is manually deleted. Losing user code because of an
//     over-eager cleanup is the exact failure mode Anthropic reported
//     in claude-code#55724, and it must never regress here.
type WorktreePoolManager struct {
	logger *slog.Logger

	// repoMu is the per-repo git-metadata mutex. The key is the
	// symlink-resolved base path (canonical form the underlying
	// `.git/worktrees` shares) so two ref rows pointing at the same tree
	// through different symlink paths collapse to one lock.
	repoMu     sync.Mutex
	repoMutex  map[string]*sync.Mutex
	repoLeases map[string]*repoPoolState

	// clock is injected for tests; production always uses time.Now.
	clock func() time.Time
}

// repoPoolState tracks the leases outstanding for a single base repo. The
// tasks slice is a debug aid — the count is what enforces MaxParallel.
// A slice is fine at pool sizes users would actually configure (single
// digits); if we ever grow to hundreds of concurrent leases per pool we'd
// switch to a set, but the linear scan is simpler here and shows up on
// hot paths only during acquire.
type repoPoolState struct {
	leases []poolLease
}

// poolLease is the live view of a single allocated worktree. It survives
// only inside the manager's map — the caller receives the Path via the
// return of Acquire and a release func that removes the lease on unlock.
type poolLease struct {
	TaskID   string
	Path     string // canonical worktree path (used for `git worktree remove`)
	RealPath string // symlink-resolved form; matches what LocalPathLocker keys on
	Branch   string
	Acquired time.Time
}

// PoolAllocation is what Acquire hands back to the daemon. WorkDir is the
// path the agent should run inside; RealPath is what the daemon must feed
// to LocalPathLocker so two paths that alias the same on-disk tree still
// serialise. Release must be invoked exactly once (deferred is safe).
type PoolAllocation struct {
	WorkDir  string
	RealPath string
	Branch   string
	Release  func()
}

// errPoolSaturated is a sentinel returned by Acquire when the base repo
// already has MaxParallel leases outstanding. Callers unwrap it to build
// the wait_reason hint (holders list). The variable form is used with
// errors.Is; the concrete PoolSaturatedError carries the holder metadata.
var errPoolSaturated = errors.New("worktree pool saturated")

// PoolSaturatedError is returned when a request would exceed MaxParallel.
// The Holders slice is a snapshot of the live lease tasks so the caller
// can render a "waiting for tasks A, B, C" hint in the UI without another
// round trip.
type PoolSaturatedError struct {
	BasePath    string
	MaxParallel int
	Holders     []string
}

func (e *PoolSaturatedError) Error() string {
	return fmt.Sprintf("worktree pool saturated (%d/%d) for %s", len(e.Holders), e.MaxParallel, e.BasePath)
}
func (e *PoolSaturatedError) Unwrap() error { return errPoolSaturated }

// NewWorktreePoolManager returns a ready-to-use manager. Safe for
// concurrent use across every daemon slot.
func NewWorktreePoolManager(logger *slog.Logger) *WorktreePoolManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &WorktreePoolManager{
		logger:     logger.With("component", "worktree_pool"),
		repoMutex:  make(map[string]*sync.Mutex),
		repoLeases: make(map[string]*repoPoolState),
		clock:      time.Now,
	}
}

// Acquire allocates a fresh worktree for taskID under the base repo's pool
// root. It runs entirely under the per-repo git-metadata mutex so callers
// never race on `.git/config.lock` or on the in-memory lease count.
//
// The flow:
//
//  1. Take the per-repo mutex.
//  2. Refuse repos that ship an initialised submodule (see the
//     WorktreePoolManager doc for why).
//  3. `git worktree prune` — reap any stale metadata from a previous
//     daemon lifetime so a lease count doesn't include ghosts.
//  4. Enforce MaxParallel against the in-memory lease list. When
//     saturated we return PoolSaturatedError with the current holders
//     so the caller can post a structured wait_reason.
//  5. `git worktree add --lock --reason ... -b multica/<uuid>
//     <pool_root>/<uuid> HEAD` — task UUID is used both as directory
//     and branch suffix so a re-run of the same task can never collide
//     with an in-flight one. `--lock` prevents `git worktree prune`
//     from clearing the tree mid-task.
//  6. Register the lease and return a release closure that reverses the
//     add (best-effort remove + prune) under the same per-repo mutex.
func (m *WorktreePoolManager) Acquire(ctx context.Context, base, poolRoot string, maxParallel int, taskID string) (*PoolAllocation, error) {
	if base == "" {
		return nil, errors.New("worktree pool: base path required")
	}
	if taskID == "" {
		return nil, errors.New("worktree pool: taskID required")
	}
	if poolRoot == "" {
		return nil, errors.New("worktree pool: pool_root required")
	}
	if maxParallel <= 0 {
		maxParallel = defaultWorktreePoolMaxParallel
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("worktree pool: resolve base %q: %w", base, err)
	}
	baseReal, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		// Fall back to the cleaned absolute form so callers see a
		// consistent key even when the symlink chain is currently
		// broken. Validation elsewhere surfaces the underlying error
		// with better context.
		baseReal = filepath.Clean(baseAbs)
	}
	baseReal = filepath.Clean(baseReal)

	poolRootAbs, err := filepath.Abs(poolRoot)
	if err != nil {
		return nil, fmt.Errorf("worktree pool: resolve pool_root %q: %w", poolRoot, err)
	}

	mu := m.repoLock(baseReal)
	mu.Lock()
	defer mu.Unlock()

	log := m.logger.With("base", baseReal, "task", shortID(taskID))

	if err := m.rejectSubmoduleRepo(ctx, baseReal); err != nil {
		return nil, err
	}

	// Best-effort: silently ignore prune failures because the returned
	// error is almost always "repo has never had a worktree yet". Failing
	// hard here would block every fresh install.
	m.pruneLocked(ctx, baseReal, log)

	state := m.repoLeases[baseReal]
	if state == nil {
		state = &repoPoolState{}
		m.repoLeases[baseReal] = state
	}
	if len(state.leases) >= maxParallel {
		holders := make([]string, 0, len(state.leases))
		for _, l := range state.leases {
			holders = append(holders, l.TaskID)
		}
		return nil, &PoolSaturatedError{BasePath: baseReal, MaxParallel: maxParallel, Holders: holders}
	}

	// Ensure the pool root exists before git worktree adds under it —
	// git refuses when the parent is missing.
	if err := os.MkdirAll(poolRootAbs, 0o755); err != nil {
		return nil, fmt.Errorf("worktree pool: create pool_root %q: %w", poolRootAbs, err)
	}

	// Task UUID is the source of truth for the directory + branch name.
	// A short id would be shorter to read but collides with taskbin
	// (agent) UUIDs shortened the same way, and the MUL-3483 spec
	// specifically calls out that the branch name must not depend on
	// agent id (a single squad agent can hold multiple worker tasks at
	// once and would otherwise collide on branch names).
	wtName := taskID
	wtPath := filepath.Join(poolRootAbs, wtName)
	branch := "multica/" + wtName
	reason := "multica task " + wtName

	// Attempt to remove any pre-existing directory at the target path
	// only when it's an EMPTY leftover from a previous daemon lifetime.
	// If somebody put real files there we bail — refusing to clobber is
	// safer than silently starting the agent in an unknown state.
	if err := ensureWorktreePathAvailable(wtPath); err != nil {
		return nil, err
	}

	addArgs := []string{"-C", baseReal, "worktree", "add", "--lock", "--reason", reason, "-b", branch, wtPath, "HEAD"}
	if out, err := runGit(ctx, addArgs...); err != nil {
		// Surface stderr verbatim so the caller's FailTask message
		// tells the user exactly what git said (missing base ref,
		// disk-full, permission denied). Wrapping alone would hide it.
		return nil, fmt.Errorf("worktree pool: git worktree add failed: %s: %w", strings.TrimSpace(out), err)
	}

	wtRealPath, err := filepath.EvalSymlinks(wtPath)
	if err != nil {
		wtRealPath = filepath.Clean(wtPath)
	}
	wtRealPath = filepath.Clean(wtRealPath)

	lease := poolLease{
		TaskID:   taskID,
		Path:     wtPath,
		RealPath: wtRealPath,
		Branch:   branch,
		Acquired: m.clock(),
	}
	state.leases = append(state.leases, lease)
	log.Info("worktree pool: allocated", "path", wtPath, "branch", branch, "in_use", len(state.leases), "max_parallel", maxParallel)

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			m.release(baseReal, taskID, wtPath, branch)
		})
	}

	return &PoolAllocation{
		WorkDir:  wtPath,
		RealPath: wtRealPath,
		Branch:   branch,
		Release:  release,
	}, nil
}

// release runs the reverse of Acquire under the per-repo mutex: unlock
// the worktree, remove it (only when clean), prune metadata, and drop
// the in-memory lease. Any error from the git side is logged rather
// than surfaced — the caller cannot do anything with a cleanup failure
// beyond what the manual `git worktree prune` step already handles.
func (m *WorktreePoolManager) release(baseReal, taskID, wtPath, branch string) {
	mu := m.repoLock(baseReal)
	mu.Lock()
	defer mu.Unlock()

	log := m.logger.With("base", baseReal, "task", shortID(taskID), "worktree", wtPath)

	// Unlock first — remove refuses --lock'd worktrees. Best-effort.
	if out, err := runGit(context.Background(), "-C", baseReal, "worktree", "unlock", wtPath); err != nil {
		// It's normal for unlock to fail when the worktree has already
		// been removed manually (e.g. during a bench test cleanup); log
		// at debug so users don't see noise.
		log.Debug("worktree pool: unlock failed (best-effort)", "output", strings.TrimSpace(out), "error", err)
	}

	dirty, err := worktreeIsDirty(wtPath)
	switch {
	case err != nil:
		log.Warn("worktree pool: cannot determine cleanliness, leaving worktree in place", "error", err)
	case dirty:
		log.Warn("worktree pool: worktree has uncommitted changes; leaving on disk for user inspection", "path", wtPath)
	default:
		// Clean → tell git to remove it. We deliberately do NOT pass
		// --force: the dirty check above already guaranteed there is
		// nothing to lose, and refusing on any surprise diff is a
		// stronger safeguard than trying to guess intent.
		if out, err := runGit(context.Background(), "-C", baseReal, "worktree", "remove", wtPath); err != nil {
			log.Warn("worktree pool: git worktree remove failed", "output", strings.TrimSpace(out), "error", err)
		}
	}

	// Prune metadata unconditionally so a stale `.git/worktrees/<uuid>`
	// entry never inflates the lease count on the next allocation.
	if out, err := runGit(context.Background(), "-C", baseReal, "worktree", "prune"); err != nil {
		log.Debug("worktree pool: prune failed (best-effort)", "output", strings.TrimSpace(out), "error", err)
	}

	// The branch itself lingers so a follow-up task or a human can
	// inspect the work. MUL-3483 explicitly calls for keeping the branch
	// when the worktree stayed on disk (dirty case); we apply the same
	// rule in the clean case for consistency — costs almost nothing and
	// avoids destroying refs the agent may have pushed.

	// Drop the lease from the in-memory ledger even when git bookkeeping
	// above failed. The slot must return to the pool or the next task
	// hits a spurious PoolSaturatedError.
	if state, ok := m.repoLeases[baseReal]; ok {
		out := state.leases[:0]
		for _, l := range state.leases {
			if l.Path != wtPath {
				out = append(out, l)
			}
		}
		state.leases = out
	}
	log.Info("worktree pool: released", "branch", branch)
}

// repoLock returns the per-repo mutex for baseReal, creating it lazily on
// first access. Guarded by m.repoMu for the map read/write only — the
// returned mutex is what callers actually serialise on.
func (m *WorktreePoolManager) repoLock(baseReal string) *sync.Mutex {
	m.repoMu.Lock()
	defer m.repoMu.Unlock()
	mu, ok := m.repoMutex[baseReal]
	if !ok {
		mu = &sync.Mutex{}
		m.repoMutex[baseReal] = mu
	}
	return mu
}

// rejectSubmoduleRepo returns an error when the base has an initialised
// submodule. We check for the presence of `.gitmodules` first — that's a
// cheap file-existence test — and only shell out to `git submodule status`
// when needed. An unchecked-out submodule prefixed with "-" in the status
// output is not initialised and does not count.
//
// The refusal is intentionally strict rather than a warning: the shared
// `$GIT_COMMON_DIR/config` writes from `git submodule` would still
// bottleneck the pool, and the per-worktree `modules/` directories bloat
// disk usage by pool_size×. Both mitigations are out of scope for the MVP.
func (m *WorktreePoolManager) rejectSubmoduleRepo(ctx context.Context, baseReal string) error {
	if _, err := os.Stat(filepath.Join(baseReal, ".gitmodules")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("worktree pool: stat .gitmodules for %q: %w", baseReal, err)
	}
	out, err := runGit(ctx, "-C", baseReal, "submodule", "status")
	if err != nil {
		return fmt.Errorf("worktree pool: git submodule status failed: %s: %w", strings.TrimSpace(out), err)
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// `git submodule status` prefixes each line with one of:
		//   ' ' (space) initialised and up-to-date
		//   '+'          initialised but with different commit
		//   'U'          merge conflict
		//   '-'          not initialised → safe to ignore
		// We refuse on anything but '-'.
		prefix := line[0]
		if prefix == '-' {
			continue
		}
		return fmt.Errorf("worktree pool: repository %q has initialised submodules — worktree_pool mode does not yet support submodule superprojects (MUL-3483 MVP scope); either uninitialise submodules or keep this resource on mode=in_place", baseReal)
	}
	return nil
}

// pruneLocked runs `git worktree prune` and swallows the error. Callers
// hold the per-repo mutex so parallel invocations are already impossible;
// this is best-effort housekeeping to reclaim leases from a previous
// daemon lifetime before we hand out a new slot.
func (m *WorktreePoolManager) pruneLocked(ctx context.Context, baseReal string, log *slog.Logger) {
	if out, err := runGit(ctx, "-C", baseReal, "worktree", "prune"); err != nil {
		log.Debug("worktree pool: prune failed (best-effort)", "output", strings.TrimSpace(out), "error", err)
	}
}

// ensureWorktreePathAvailable is a defensive check for the case where a
// prior release couldn't remove the worktree (e.g. the daemon crashed
// mid-cleanup) and a re-run picks the same task UUID. Empty directories
// are safe to `os.Remove` (git will recreate); a directory that still
// holds files is a signal that the previous run left work behind and we
// refuse the allocation so the user can inspect.
func ensureWorktreePathAvailable(wtPath string) error {
	entries, err := os.ReadDir(wtPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("worktree pool: probe %q: %w", wtPath, err)
	}
	if len(entries) == 0 {
		if err := os.Remove(wtPath); err != nil {
			return fmt.Errorf("worktree pool: clear empty leftover %q: %w", wtPath, err)
		}
		return nil
	}
	return fmt.Errorf("worktree pool: target path %q already exists and is non-empty; remove it before retrying", wtPath)
}

// worktreeIsDirty reports whether the worktree at path has uncommitted
// changes OR untracked files. Returning err != nil means we couldn't
// tell — the caller treats that the same as dirty (better to leak a
// directory than lose work).
//
// We include untracked files (`--untracked-files=normal`) because the
// whole point of the dirty check is "did the agent leave anything the
// user might want to inspect?", and a fresh file the agent created but
// hasn't committed is exactly that. Excluding it here would let us log
// "clean → remove", then have `git worktree remove` refuse the removal
// because untracked files exist (git itself has always refused this) —
// the outcome (directory preserved) is correct, but the log path is
// misleading and future maintainers seeing "worktree removed" would
// assume the disk was actually reclaimed. Including untracked keeps
// the observability honest with what actually happens on disk.
//
// Porcelain=v1 is the historically-stable format; -z avoids
// escape-quoting weirdness; any non-empty content means dirty.
func worktreeIsDirty(path string) (bool, error) {
	cmd := exec.Command("git", "-C", path, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	out, err := cmd.Output()
	if err != nil {
		if runtime.GOOS == "windows" {
			// On Windows a permission-denied on the working tree is
			// common after antivirus scans; treating that as dirty
			// keeps our safety guarantee even though we couldn't
			// confirm the state.
			return true, err
		}
		return true, err
	}
	// -z gives NUL-terminated entries; any non-empty content means dirty.
	return len(strings.Trim(string(out), "\x00")) > 0, nil
}

// runGit runs a git subcommand and returns its combined stdout+stderr.
// Failures include the process error verbatim so callers can wrap it
// with additional context.
func runGit(ctx context.Context, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
