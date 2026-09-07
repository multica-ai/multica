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
)

// localDirectoryResourceType is the project_resource discriminator the daemon
// looks for when deciding whether a task should run against an existing
// user directory rather than a fresh git worktree. Mirrors the server-side
// constant — keep in sync if the type string is ever renamed.
const localDirectoryResourceType = "local_directory"

// Execution modes for local_directory resources. Mirrors the server-side
// constants in handler/project_resource.go — keep in sync. An absent or empty
// value means in_place, so resources created before worktree mode existed keep
// their original behavior.
const (
	localDirectoryModeInPlace  = "in_place"
	localDirectoryModeWorktree = "worktree"
	localDirectoryModeRunOwned = "run_owned"
)

// localDirectoryRef mirrors the server-side ref shape for local_directory
// project resources. Defined locally so the daemon does not have to import
// the server handler package.
type localDirectoryRef struct {
	LocalPath                    string `json:"local_path"`
	DaemonID                     string `json:"daemon_id"`
	Label                        string `json:"label,omitempty"`
	ExecutionMode                string `json:"execution_mode,omitempty"`
	BaseCommit                   string `json:"base_commit,omitempty"`
	InheritWorkspaceRepositories *bool  `json:"inherit_workspace_repositories,omitempty"`
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

// UsesWorktree reports whether this assignment runs each task in its own git
// worktree instead of in the user's directory. Worktree tasks skip the per-path
// mutex entirely — that is the whole point of the mode — so every caller that
// serialises, cleans up sidecars, or exempts the env root from GC must branch
// on this rather than on "is there a local_directory assignment at all".
func (a *localDirectoryAssignment) UsesWorktree() bool {
	return a != nil && strings.TrimSpace(a.Ref.ExecutionMode) == localDirectoryModeWorktree
}

func (a *localDirectoryAssignment) UsesRunWorkspace() bool {
	return a != nil && a.Ref.ExecutionMode == localDirectoryModeRunOwned
}

// DisplayName is the human-facing name for this directory, safe to render in
// UI. It is deliberately NOT the absolute path: the wait reason built from it
// is stored server-side and pushed to every client on the session, and a chip
// in a chat transcript ends up in screen shares and screenshots. Absolute
// paths carry the account name, which is why handler.relativeWorkDir exists
// for the sibling work_dir chip; this is the same contract for the same reason.
//
// The resource's own label wins when the user set one — that is the name they
// chose for this directory. Otherwise the basename, which is what distinguishes
// sibling checkouts ("NuvioTV" vs "multica") without naming their parent.
func (a *localDirectoryAssignment) DisplayName() string {
	if a == nil {
		return ""
	}
	if label := strings.TrimSpace(a.Ref.Label); label != "" {
		return label
	}
	return filepath.Base(a.AbsPath)
}

// ValidateExecutionMode rejects a mode this daemon does not implement.
//
// Falling back to in_place would be the wrong direction, even though it is the
// older and more conservative code path. execution_mode is how a user asks for
// ISOLATION, not merely for concurrency: silently running in_place instead
// would let the agent edit the working copy the user explicitly asked it to
// stay out of. Losing concurrency is a nuisance; ignoring a request to not
// touch someone's files is a broken promise. So an unrecognised mode fails the
// task with a message naming the version skew.
func (a *localDirectoryAssignment) ValidateExecutionMode() error {
	if a == nil {
		return nil
	}
	switch strings.TrimSpace(a.Ref.ExecutionMode) {
	case "", localDirectoryModeInPlace, localDirectoryModeWorktree, localDirectoryModeRunOwned:
		return nil
	default:
		return fmt.Errorf(
			"local_directory: this daemon does not support execution_mode %q for %q "+
				"(update the daemon, or set the resource's execution mode to %q or %q); "+
				"refusing to run in place, since that would modify a directory the resource asked to isolate",
			a.Ref.ExecutionMode, a.AbsPath, localDirectoryModeInPlace, localDirectoryModeWorktree)
	}
}

// localDirectoryAssignmentForTask returns the local_directory assignment a task
// should execute inside. Squad-leader tasks are coordinators: they may create
// child issues or comments, but should not bind to the user's repo worktree or
// hold the path mutex while downstream workers are ready to write.
//
// This answers WHERE a task runs, and only that. Its result also drives the
// agent's working directory (daemon.runTask plumbs AbsPath into
// execenv.PrepareParams.LocalWorkDir) and the GC-meta stamp that exempts a
// user-owned path from env-root cleanup. Whether the task additionally takes
// the per-path mutex is a SEPARATE question, answered by
// localDirectoryLockExempt — collapsing the two is what made a read-only chat
// turn queue behind a 20-minute build (issue #7344), and answering "no
// assignment" there to free the lock would have silently moved chat out of the
// user's directory as well.
func localDirectoryAssignmentForTask(task Task, daemonID string) (*localDirectoryAssignment, error) {
	if task.IsLeaderTask {
		return nil, nil
	}
	return findLocalDirectoryAssignment(task.ProjectResources, daemonID)
}

// localDirectoryLockExempt reports whether a task may run inside an in_place
// local_directory WITHOUT serialising on the per-path mutex. It is asked only
// after an assignment has been resolved and validated, so an exempt task still
// runs in the user's directory — it just doesn't queue for it.
//
// What the mutex actually protects: two long coding runs interleaving two sets
// of edits into one working tree. It was never a general write barrier and
// cannot become one — the user's own editor, their terminal, and any external
// script write to that same tree unsynchronised, and always have. So the
// question a task must answer to earn the lock is not "could it ever write?"
// (everything could) but "is it a second heavyweight writer?".
//
// A chat turn is not. It is a conversation that reads the tree to answer
// questions and at most saves a file the way the user's own Cmd+S does — the
// risk class the lock already declines to cover. Serialising it bought nothing
// and muted the squad leader for the length of every build (issue #7344).
//
// Keyed on ChatSessionID because that is the daemon's only chat discriminator
// (see Task.ChatSessionID). IsLeaderTask cannot serve here even though the
// comment above it describes chat's semantics exactly: the server never writes
// that column on the chat-task insert path (service.EnqueueChatTask →
// db.CreateChatTaskParams has no such field), so it is false on every chat
// turn ever dispatched.
func localDirectoryLockExempt(task Task) bool {
	return task.ChatSessionID != ""
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
