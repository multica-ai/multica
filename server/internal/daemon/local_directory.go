package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// localDirectoryResourceType is the project_resource discriminator the daemon
// looks for when deciding whether a task should run against an existing
// user directory rather than a fresh git worktree. Mirrors the server-side
// constant — keep in sync if the type string is ever renamed.
const localDirectoryResourceType = "local_directory"

// localDirectoryRef mirrors the server-side ref shape for local_directory
// project resources. Defined locally so the daemon does not have to import
// the server handler package.
type localDirectoryRef struct {
	LocalPath string `json:"local_path"`
	DaemonID  string `json:"daemon_id"`
	Label     string `json:"label,omitempty"`
}

// localDirectoryAssignment is the resolved view of a task's local_directory
// resource: the absolute path the daemon will use as the agent's workdir,
// plus the underlying ref for callers that still need the raw label / daemon
// id (validation log messages, mostly). RealPath is the symlink-resolved
// absolute path; the path mutex keys on it so two different routes to the
// same directory are serialised.
type localDirectoryAssignment struct {
	Ref      localDirectoryRef
	AbsPath  string // user-provided path, cleaned but not symlink-resolved
	RealPath string // canonical key for the path mutex
}

// findLocalDirectoryAssignment scans the task's project resources for one of
// type local_directory whose daemon_id matches this daemon. Returns nil
// (without error) when no such resource exists — the task takes the regular
// github_repo / worktree code path. Returns an error only when the matching
// resource is structurally broken (bad JSON, missing fields) OR when more
// than one resource is pinned to this daemon — that's a server-side
// invariant violation, and silently picking the first match would let the
// agent write into an arbitrary directory the user didn't intend.
//
// Server-side `findLocalDirectoryConflict` enforces a single local_directory
// per (project, daemon), so two matches here means either the constraint
// was bypassed (older API client) or the data was corrupted. Either way,
// fail fast rather than guess.
func findLocalDirectoryAssignment(resources []ProjectResourceData, daemonID string) (*localDirectoryAssignment, error) {
	var match *localDirectoryAssignment
	for _, r := range resources {
		if r.ResourceType != localDirectoryResourceType {
			continue
		}
		var ref localDirectoryRef
		if err := json.Unmarshal(r.ResourceRef, &ref); err != nil {
			return nil, fmt.Errorf("local_directory: parse resource_ref: %w", err)
		}
		ref.DaemonID = strings.TrimSpace(ref.DaemonID)
		if ref.DaemonID == "" {
			return nil, errors.New("local_directory: resource_ref missing daemon_id")
		}
		if ref.DaemonID != daemonID {
			// A different daemon owns this resource. Skip silently; the
			// project may have multiple local_directory resources, one
			// per daemon, and other daemons will resolve their own row.
			continue
		}
		if match != nil {
			// Server-side invariant: at most one local_directory per
			// (project, daemon). Two matches here means the constraint
			// was bypassed by an older API client or by direct DB writes.
			// Either way, refuse to guess which directory the user meant.
			return nil, fmt.Errorf(
				"local_directory: project has multiple local_directory resources for this daemon (%q and %q); remove the extra in project settings",
				match.AbsPath,
				strings.TrimSpace(ref.LocalPath),
			)
		}
		absPath, err := normalizeLocalPath(ref.LocalPath)
		if err != nil {
			return nil, err
		}
		realPath, err := resolveRealPath(absPath)
		if err != nil {
			return nil, err
		}
		match = &localDirectoryAssignment{
			Ref:      ref,
			AbsPath:  absPath,
			RealPath: realPath,
		}
	}
	return match, nil
}

// normalizeLocalPath strips whitespace and resolves the path to an absolute
// cleaned form. It does NOT touch the filesystem (no symlink resolution, no
// existence check) — callers do that separately via validateLocalPath.
func normalizeLocalPath(p string) (string, error) {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return "", errors.New("local_directory: local_path is empty")
	}
	if !filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("local_directory: local_path must be absolute, got %q", trimmed)
	}
	return filepath.Clean(trimmed), nil
}

// resolveRealPath returns the symlink-resolved absolute form of path. The
// path mutex keys on this value so a task on `/Users/u/proj` and another on
// `/private/var/folders/.../proj-symlink → /Users/u/proj` collapse to one
// lock. When EvalSymlinks fails (path is missing or not yet a real link),
// fall back to the cleaned absolute form so callers can still proceed to
// the existence-check stage which surfaces a clearer error.
func resolveRealPath(absPath string) (string, error) {
	real, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// validateLocalPath will surface the underlying error with better
		// context; for the mutex key the cleaned absolute path is a safe
		// fallback (it just slightly weakens the dedup on broken symlinks).
		return absPath, nil
	}
	return real, nil
}

// validateLocalPath enforces the daemon-side preconditions for running an
// agent against a user-supplied directory:
//
//   - the path is absolute and not in the system blacklist (root, $HOME,
//     /Users, /home, the current user's $HOME — picking one of those would
//     scope the agent to the entire account, which is never what the user
//     intended);
//   - the symlink-resolved target is ALSO not in the blacklist — without
//     this a symlink like /Users/me/proj/home -> /Users/me would slip the
//     literal-equality check above while still routing every daemon write
//     into $HOME;
//   - the path exists, is a directory (not a regular file or device);
//   - the daemon process can read and write inside it (the agent will need
//     both — read for context discovery, write for the issue's edits).
//
// Each failure returns a typed error message so the daemon can forward it
// onto the task's fail comment verbatim.
func validateLocalPath(absPath string) error {
	if absPath == "" {
		return errors.New("local_directory: local_path is empty")
	}
	if !filepath.IsAbs(absPath) {
		return fmt.Errorf("local_directory: local_path must be absolute, got %q", absPath)
	}
	if reason, blocked := isBlacklistedLocalPath(absPath); blocked {
		return fmt.Errorf("local_directory: %s (%q)", reason, absPath)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("local_directory: path does not exist: %q", absPath)
		}
		return fmt.Errorf("local_directory: stat %q: %w", absPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local_directory: path is not a directory: %q", absPath)
	}
	// Re-check the blacklist after resolving symlinks. Two ways the
	// literal check can be bypassed even when absPath itself is clean:
	//
	//   1. A user-created symlink (or a parent component) routes writes
	//      into a banned target. Example: ~/proj/home-link -> /Users/me.
	//   2. The user directly selects a canonical OS path that aliases a
	//      banned root via an OS-level symlink. Example on macOS: typing
	//      /private/tmp slips past the /tmp entry because the literal
	//      strings don't match, and EvalSymlinks is a no-op since the
	//      input is already canonical. This must be checked
	//      unconditionally — not gated on realPath != absPath — or the
	//      direct-canonical case is silently allowed.
	//
	// EvalSymlinks walks intermediate components too, so a non-symlink
	// absPath whose parent is a symlink also fails closed.
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("local_directory: resolve symlinks for %q: %w", absPath, err)
	}
	realPath = filepath.Clean(realPath)
	if reason, blocked := isBlacklistedRealPath(realPath); blocked {
		if realPath != filepath.Clean(absPath) {
			return fmt.Errorf("local_directory: %s (symlink target of %q is %q)", reason, absPath, realPath)
		}
		return fmt.Errorf("local_directory: %s (canonical path %q)", reason, absPath)
	}
	if err := checkDirReadWrite(absPath); err != nil {
		return fmt.Errorf("local_directory: %w", err)
	}
	return nil
}

// isBlacklistedLocalPath rejects paths that map to the whole machine or an
// entire user profile. The intent is to keep the daemon from accidentally
// stamping context files (.agent_context/, .claude/skills/, .multica/) at
// the root of a user's account or the OS — a misconfiguration on the UI
// side should fail fast rather than litter the user's home.
//
// The check is by literal equality after Clean(), not prefix containment:
// a legitimate project under /Users/<user>/code/proj should pass.
func isBlacklistedLocalPath(absPath string) (reason string, blocked bool) {
	cleaned := filepath.Clean(absPath)
	if isDriveRoot(cleaned) {
		return fmt.Sprintf("path is a drive root %q", cleaned), true
	}
	for _, banned := range systemRootBlacklist() {
		if cleaned == banned {
			return fmt.Sprintf("path is a protected system root %q", banned), true
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if cleaned == filepath.Clean(home) {
			return "path is the user's home directory", true
		}
	}
	return "", false
}

// isBlacklistedRealPath is the canonical-aware variant of
// isBlacklistedLocalPath. It compares the symlink-resolved realPath against
// the symlink-resolved form of each blacklist entry so OS-level redirects
// (notably macOS's /etc -> /private/etc, /tmp -> /private/tmp, /var ->
// /private/var) cannot be used to slip a candidate past the literal
// blacklist — whether the redirect is reached via a user-created symlink
// (~/proj/home-link -> /Users/me) or by directly typing the canonical form
// (/private/tmp), which is identical to the OS view of /tmp.
func isBlacklistedRealPath(realPath string) (reason string, blocked bool) {
	realClean := filepath.Clean(realPath)
	if isDriveRoot(realClean) {
		return fmt.Sprintf("path is a drive root %q", realClean), true
	}
	for _, banned := range systemRootBlacklist() {
		bannedClean := filepath.Clean(banned)
		if realClean == bannedClean {
			return fmt.Sprintf("path is a protected system root %q", banned), true
		}
		if r, err := filepath.EvalSymlinks(banned); err == nil {
			if filepath.Clean(r) == realClean {
				return fmt.Sprintf("path is a protected system root %q", banned), true
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		homeClean := filepath.Clean(home)
		if realClean == homeClean {
			return "path is the user's home directory", true
		}
		if r, err := filepath.EvalSymlinks(home); err == nil {
			if filepath.Clean(r) == realClean {
				return "path is the user's home directory", true
			}
		}
	}
	return "", false
}

// isDriveRoot reports whether absPath is the root of a Windows volume — any
// of `C:\`, `D:\`, ..., `Z:\`, plus less common cases like `\\server\share`
// (filepath.VolumeName treats UNC roots as volumes too). On non-Windows
// this is always false because POSIX has no concept of drive letters and
// `/` is covered by systemRootBlacklist.
//
// We rely on filepath.VolumeName rather than enumerating drive letters
// statically: removable / network drives can be mounted at any letter
// (`G:\`, `H:\`, ...), and Windows installs are increasingly happy to put
// the user profile on a non-C drive. A static list (C..F) would miss them
// all.
func isDriveRoot(absPath string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	vol := filepath.VolumeName(absPath)
	if vol == "" {
		return false
	}
	// VolumeName returns the volume without trailing separator (`C:` or
	// `\\srv\share`). A drive root is volume + one separator (or, after
	// filepath.Clean, just the volume on bare-volume input).
	rest := absPath[len(vol):]
	return rest == "" || rest == `\` || rest == "/"
}

// systemRootBlacklist returns the per-OS list of paths the daemon never
// allows as a local_directory root. POSIX systems get `/`, `/Users`, `/home`
// (and macOS's `/Users/Shared` for good measure); Windows gets the
// well-known account / shared trees under C:. Drive roots themselves are
// handled by isDriveRoot so we don't have to enumerate G:\, H:\, etc.
// The list is intentionally conservative — it errs on the side of
// rejecting more, since the desktop UI is expected to surface a friendly
// picker that never produces these values.
func systemRootBlacklist() []string {
	if runtime.GOOS == "windows" {
		return []string{`C:\Users`, `C:\ProgramData`, `C:\Program Files`, `C:\Program Files (x86)`, `C:\Windows`}
	}
	return []string{"/", "/Users", "/Users/Shared", "/home", "/root", "/var", "/etc", "/tmp", "/usr", "/opt"}
}

// checkDirReadWrite verifies the daemon process can both read directory
// contents and create/remove a probe file inside dir. The probe filename is
// long, hidden, and unlikely to clash with user files; we delete it
// immediately and ignore the delete error (best-effort cleanup is fine —
// the worst case is leaving a 0-byte file the user can ignore).
func checkDirReadWrite(dir string) error {
	if _, err := os.ReadDir(dir); err != nil {
		return fmt.Errorf("read %q: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".multica-rwcheck-*")
	if err != nil {
		return fmt.Errorf("write %q: %w", dir, err)
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)
	return nil
}

// isGitWorkTree reports whether path is the working tree of a git repo. The
// daemon uses this to skip branch / worktree machinery when the user has
// already pointed the project at their own clone — the agent operates on
// the current branch in place. Returns false on any error (git not on PATH,
// path not in a repo, exec failure) so the caller can treat "not a git
// tree" and "can't tell" the same way: skip the git-specific path.
func isGitWorkTree(ctx context.Context, path string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// LocalPathLocker serialises agent tasks that share the same on-disk path.
// The lock is owned for the entire lifetime of a task (claim → context
// write → agent execution → result report), not just the agent execution
// window, because the context files and skill scratch directories the
// daemon writes at task-prepare time can race with a sibling task on the
// same path.
//
// Implementation: per-key sync.Mutex inside a map guarded by mu. When a
// task can't take the lock immediately, the waiter blocks on the per-key
// Mutex itself — that gives FIFO-ish behaviour from the Go scheduler
// (sufficient for our load; the issue body asks for a wait queue, not a
// strict-priority queue). Holder bookkeeping (current holder task id) is
// surfaced via Holder so callers can build a UI-friendly wait_reason.
type LocalPathLocker struct {
	mu    sync.Mutex
	locks map[string]*pathLockEntry
}

type pathLockEntry struct {
	mu       sync.Mutex // serialises holders for this key
	mu2      sync.Mutex // guards holderID under contention
	holderID string     // current owner, for UI hints; empty when free
}

// NewLocalPathLocker returns an empty locker. Safe for concurrent use.
func NewLocalPathLocker() *LocalPathLocker {
	return &LocalPathLocker{locks: make(map[string]*pathLockEntry)}
}

// Holder returns the task id currently holding the lock for realPath, or
// "" if no task holds it. Used to populate the wait_reason hint the daemon
// posts to the server when it parks a task — the UI then shows "waiting for
// <path> (held by task <short id>)".
func (l *LocalPathLocker) Holder(realPath string) string {
	l.mu.Lock()
	entry, ok := l.locks[realPath]
	l.mu.Unlock()
	if !ok {
		return ""
	}
	entry.mu2.Lock()
	defer entry.mu2.Unlock()
	return entry.holderID
}

// Acquire takes the lock for realPath on behalf of taskID. If the lock is
// already held, onWait is invoked (synchronously, before this goroutine
// blocks) with the current holder id so callers can flip the task into the
// server-side waiting_local_directory state. onWait may be nil for callers
// that don't need the side effect.
//
// Returns a release func that the caller must invoke (typically deferred)
// to free the lock. The release is idempotent.
//
// Acquire is cancellable via ctx. When ctx is cancelled while the goroutine
// is blocked on the lock, Acquire returns ctx.Err() and the lock is NOT
// taken. This is the same contract as sync.Mutex.Lock paired with
// context-aware cancellation — a daemon shutdown won't wedge inside the
// per-path wait queue.
func (l *LocalPathLocker) Acquire(ctx context.Context, realPath, taskID string, onWait func(holder string)) (func(), error) {
	if realPath == "" {
		return nil, errors.New("local_directory: realpath required for lock")
	}
	if taskID == "" {
		return nil, errors.New("local_directory: taskID required for lock")
	}

	l.mu.Lock()
	entry, ok := l.locks[realPath]
	if !ok {
		entry = &pathLockEntry{}
		l.locks[realPath] = entry
	}
	l.mu.Unlock()

	// Try the fast path first — no allocation, no waiter goroutine.
	if entry.mu.TryLock() {
		entry.mu2.Lock()
		entry.holderID = taskID
		entry.mu2.Unlock()
		return l.releaser(realPath, entry), nil
	}

	// Slow path: somebody else holds the lock. Fire onWait once with the
	// current holder so the daemon can stamp the server-side wait state,
	// then block until either we win the lock or ctx is cancelled.
	if onWait != nil {
		entry.mu2.Lock()
		holder := entry.holderID
		entry.mu2.Unlock()
		onWait(holder)
	}

	acquired := make(chan struct{})
	go func() {
		entry.mu.Lock()
		close(acquired)
	}()

	select {
	case <-acquired:
		entry.mu2.Lock()
		entry.holderID = taskID
		entry.mu2.Unlock()
		return l.releaser(realPath, entry), nil
	case <-ctx.Done():
		// We lost the wait — the goroutine above will still complete and
		// take the lock. Spin off a clean-up goroutine that releases it
		// the moment the acquire returns so a future caller isn't stuck
		// behind a phantom holder. The bookkeeping is best-effort: no
		// holder id is set, since this task never owned the lock.
		go func() {
			<-acquired
			entry.mu.Unlock()
		}()
		return nil, ctx.Err()
	}
}

// releaser returns the unlock callback. Idempotent via a once flag so a
// deferred release is safe even when the caller has already explicitly
// released after task completion.
func (l *LocalPathLocker) releaser(realPath string, entry *pathLockEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			entry.mu2.Lock()
			entry.holderID = ""
			entry.mu2.Unlock()
			entry.mu.Unlock()
			// We deliberately keep the entry in the map even when nothing
			// is queued. The cost is one *pathLockEntry per distinct path
			// the daemon has ever served, which is bounded by the number
			// of local_directory project resources a workspace has — tiny
			// in practice. Pruning would race with a sibling caller that
			// just looked up the same entry and is about to TryLock.
			_ = realPath
		})
	}
}
