package execenv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Files to symlink from the shared ~/.codex/ into the per-task CODEX_HOME.
// Symlinks share state (e.g. auth tokens) so changes propagate automatically.
var codexSymlinkedFiles = []string{
	"auth.json",
}

// Files to copy from the shared ~/.codex/ into the per-task CODEX_HOME.
// Copies are isolated — task-local config and cache refreshes don't mutate
// the shared home.
var codexCopiedFiles = []string{
	"config.json",
	"config.toml",
	"instructions.md",
}

const (
	codexModelsCacheFile        = "models_cache.json"
	codexModelsCacheBindingFile = ".models_cache_config.sha256"
)

// Files whose contents select the model provider/catalog used by Codex. The
// task-local models cache is only reusable while this source configuration
// remains unchanged. A model_catalog_json referenced by config.toml is folded
// into the binding separately by codexModelsCacheConfigFingerprint.
var codexModelsCacheConfigFiles = []string{
	"config.json",
	"config.toml",
}

// CodexHomeOptions carries optional inputs for prepareCodexHomeWithOpts that
// affect the generated per-task config.toml.
type CodexHomeOptions struct {
	// CodexVersion is the detected Codex CLI version (e.g. "0.121.0"). Empty
	// means unknown; on macOS, unknown is treated as "probably broken" so the
	// daemon falls back to danger-full-access for network access. See
	// codex_sandbox.go for details.
	CodexVersion string
	// GOOS overrides the target platform when deciding the sandbox policy.
	// Empty means use runtime.GOOS. Primarily exists so tests can exercise
	// both macOS and Linux paths deterministically.
	GOOS string
	// ResumeSessionID is the Codex thread/session ID this run intends to
	// resume, when any. It is consulted when populating the per-issue session
	// store (local_directory tasks) or migrating a legacy per-task home whose
	// sessions/ still symlinks the shared ~/.codex/sessions: the single rollout
	// for this ID is exposed so thread/resume can find it without pulling the
	// whole shared history back in. Empty means a fresh thread (no rollout to
	// expose). See prepareCodexSessionsDir (MUL-4424).
	ResumeSessionID string
	// IsLocalDirectory marks a local_directory task — one running in the user's
	// own project directory. These tasks get a fresh codex-home per task ID (the
	// daemon never reuses their workdir), so their sessions/ is pointed at the
	// per-issue store (SessionStoreKey) that survives across task IDs and holds
	// ONLY this issue's rollouts — never the machine's whole ~/.codex/sessions.
	// See prepareCodexSessionsDir (MUL-4424).
	IsLocalDirectory bool
	// SessionStoreKey is a stable, per-(agent, issue) relative path segment that
	// identifies this task's persistent Codex sessions store. It survives across
	// task IDs (unlike the task-scoped envRoot the GC reclaims) so a follow-up
	// run resumes the same thread. Empty when no stable key is available (e.g. a
	// task with no issue), in which case sessions/ stays task-local. See
	// codexSessionStoreDir and prepareCodexSessionsDir (MUL-4424).
	SessionStoreKey string
	// WritableRoots are extra absolute paths written into the config.toml
	// `[sandbox_workspace_write] writable_roots` so the workspace-write sandbox
	// (Linux) can write outside the task workdir — the per-task writable HOME.
	// Only meaningful when the policy resolves to workspace-write; ignored on
	// darwin danger-full-access. See task_home.go and MUL-4856.
	WritableRoots []string
	// CodexCustomArgs are the effective Codex CLI args this task will launch
	// with (daemon defaults + profile-fixed + per-agent custom_args). Only the
	// Windows sandbox decision reads them, to honor a `-c windows.sandbox=...`
	// override that never lands in config.toml. See resolveWindowsSandboxState
	// and MUL-4957.
	CodexCustomArgs []string
}

// prepareCodexHome is a thin wrapper around prepareCodexHomeWithOpts kept for
// tests that don't care about platform-aware sandbox configuration. It
// assumes a Linux-like environment where workspace-write + network_access
// works correctly.
func prepareCodexHome(codexHome string, logger *slog.Logger) error {
	return prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{GOOS: "linux"}, logger)
}

// sharedConfigPresence is the tri-state existence of the shared
// ~/.codex/config.toml copy source. It is three-valued so a stat that fails for
// a reason other than "not found" (permission/IO) never masquerades as a
// confident "the user has no config" — which would let the daemon loosen to
// danger-full-access on doubt. See resolveWindowsSandboxState (MUL-4957).
type sharedConfigPresence int

const (
	// sharedConfigAbsent: the shared config.toml is confidently not present
	// (os.IsNotExist), so an absent per-task copy is a genuine "unconfigured".
	sharedConfigAbsent sharedConfigPresence = iota
	// sharedConfigPresent: the shared config.toml exists.
	sharedConfigPresent
	// sharedConfigUndecidable: the stat failed for a reason other than
	// not-found; the daemon cannot tell whether the user has a config.
	sharedConfigUndecidable
)

// statSharedCodexConfig classifies the shared ~/.codex/config.toml (the copy
// source) into the tri-state above, distinguishing a genuine absence from a
// stat that could not complete.
func statSharedCodexConfig(sharedHome string) sharedConfigPresence {
	if sharedHome == "" {
		return sharedConfigAbsent
	}
	_, err := os.Stat(filepath.Join(sharedHome, "config.toml"))
	switch {
	case err == nil:
		return sharedConfigPresent
	case os.IsNotExist(err):
		return sharedConfigAbsent
	default:
		return sharedConfigUndecidable
	}
}

// resolveWindowsSandboxState determines, for a Windows task, whether a native
// Codex sandbox is configured — across the per-task config.toml and the
// effective custom args — failing closed (Undecidable) when it cannot tell.
//
// Two signals it does NOT gather itself (the caller does) keep the fail-closed
// logic unit-testable without faulting the filesystem, and close MUL-4957's
// round-3 must-fix where a failed sync could be misread as "unconfigured":
//
//   - configSyncErr: the error (if any) from syncing the shared config.toml
//     into this per-task home. Non-nil means the per-task config.toml is
//     unreliable — stale from a prior run, or never (re)written — so neither its
//     contents nor its absence reflect the user's intent. Fail closed.
//   - sharedPresence: whether the shared config.toml source exists. Only a
//     confident absence lets an absent per-task copy count as genuinely
//     unconfigured; a present-or-undecidable source whose per-task copy is
//     missing means the copy silently did not land, so fail closed.
func resolveWindowsSandboxState(configFile string, configSyncErr error, sharedPresence sharedConfigPresence, customArgs []string, logger *slog.Logger) windowsSandboxConfig {
	configState := classifyPerTaskWindowsSandbox(configFile, configSyncErr, sharedPresence)
	state := resolveWindowsSandbox(configState, windowsSandboxFromCustomArgs(customArgs))
	if state == windowsSandboxUndecidable && logger != nil {
		logger.Error("codex sandbox: cannot determine Windows native sandbox config; keeping workspace-write and refusing to loosen to danger-full-access",
			"config_file", configFile)
	}
	return state
}

// classifyPerTaskWindowsSandbox inspects the per-task config.toml given the
// outcome of syncing it from the shared source, failing closed whenever the
// file cannot be trusted or read.
func classifyPerTaskWindowsSandbox(configFile string, configSyncErr error, sharedPresence sharedConfigPresence) windowsSandboxConfig {
	// A failed shared→per-task sync leaves config.toml stale or missing; neither
	// its contents nor its absence reflect the user's intent. Fail closed.
	if configSyncErr != nil {
		return windowsSandboxUndecidable
	}
	data, err := os.ReadFile(configFile)
	switch {
	case err == nil:
		return windowsSandboxFromConfig(string(data))
	case os.IsNotExist(err):
		// Sync succeeded and the per-task config is absent. That is a genuine
		// "no config" only when the shared source is confidently absent too; a
		// present or undecidable source whose copy is missing means the copy
		// did not land → fail closed rather than loosen.
		if sharedPresence == sharedConfigAbsent {
			return windowsSandboxAbsent
		}
		return windowsSandboxUndecidable
	default:
		// A read error (permission/IO) on a file the daemon just wrote.
		return windowsSandboxUndecidable
	}
}

// prepareCodexHomeWithOpts creates a per-task CODEX_HOME directory and seeds
// it with config from the shared ~/.codex/ home. Auth is symlinked (shared),
// config files are copied (isolated). The per-task config.toml gets a
// daemon-managed sandbox block picked by codexSandboxPolicyFor.
func prepareCodexHomeWithOpts(codexHome string, opts CodexHomeOptions, logger *slog.Logger) error {
	sharedHome := resolveSharedCodexHome()
	freshHome := false
	if _, err := os.Lstat(codexHome); os.IsNotExist(err) {
		freshHome = true
	}

	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		return fmt.Errorf("create codex-home dir: %w", err)
	}

	// Give the task its own local sessions/ directory instead of symlinking the
	// shared ~/.codex/sessions in — a huge shared history would otherwise stall
	// Codex's `initialize` state backfill (MUL-4424). See prepareCodexSessionsDir.
	if err := prepareCodexSessionsDir(codexHome, sharedHome, opts, logger); err != nil {
		logger.Warn("execenv: codex-home sessions dir prepare failed", "error", err)
	}

	// Symlink shared files (auth).
	for _, name := range codexSymlinkedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := ensureSymlink(src, dst); err != nil {
			logger.Warn("execenv: codex-home symlink failed", "file", name, "error", err)
		}
	}

	// Surface the resulting auth.json state (file kind only, never contents)
	// so operators diagnosing token-refresh failures can tell whether the
	// per-task home is tracking the shared ~/.codex/auth.json or has drifted
	// into a stale local copy.
	logCodexAuthState(filepath.Join(codexHome, "auth.json"), logger)

	// Sync isolated files from the shared source. Track the config.toml sync
	// outcome specifically: on Windows a failed sync makes the per-task config
	// untrustworthy, so the sandbox decision must fail closed rather than read a
	// stale or absent copy as "unconfigured" and loosen (MUL-4957).
	var configSyncErr error
	for _, name := range codexCopiedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := syncCopiedFile(src, dst); err != nil {
			logger.Warn("execenv: codex-home sync failed", "file", name, "error", err)
			if name == "config.toml" {
				configSyncErr = err
			}
		}
	}
	// Drop `[[skills.config]]` entries inherited from the user's
	// ~/.codex/config.toml. Codex Desktop writes plugin-backed skills with a
	// `name` and no `path`, which the CLI's stricter TOML parser rejects with
	// `missing field path` and bails out of `thread/start`. Multica writes the
	// agent's active skills directly to `codex-home/skills/`, so the
	// user-level registry is redundant here. See codex_skill_strip.go.
	if err := sanitizeCopiedCodexConfig(filepath.Join(codexHome, "config.toml")); err != nil {
		logger.Warn("execenv: codex-home sanitize config failed", "error", err)
	}

	if err := syncCodexModelCatalog(codexHome, sharedHome); err != nil {
		return fmt.Errorf("sync codex model_catalog_json: %w", err)
	}

	// Seed the shared model cache only for a fresh task home. On reuse, keep a
	// task-local cache that Codex may have refreshed, but only while the source
	// provider/catalog configuration is still the one that cache was bound to.
	// If binding fails, discard the optional cache so Codex refreshes it instead
	// of potentially using models from the wrong provider.
	if err := syncCodexModelsCache(codexHome, sharedHome, freshHome); err != nil {
		logger.Warn("execenv: codex-home models cache sync failed; discarding cache", "error", err)
		if removeErr := os.RemoveAll(filepath.Join(codexHome, codexModelsCacheFile)); removeErr != nil {
			return fmt.Errorf("sync codex models cache: %v; discard unsafe cache: %w", err, removeErr)
		}
	}

	if err := exposeSharedCodexPluginCache(codexHome, sharedHome); err != nil {
		logger.Warn("execenv: codex-home plugin cache exposure failed", "error", err)
	}

	// Write a daemon-managed sandbox block into config.toml. On macOS we may
	// need to fall back to danger-full-access because of openai/codex#10390,
	// and on Windows the daemon defaults to danger-full-access unless the user
	// opted into a native windows.sandbox; see codex_sandbox.go for the full
	// rationale. On Windows, resolve the native-sandbox state across the copied
	// config and the effective custom args so an explicit user opt-in is honored
	// and an undecidable config fails closed instead of loosening.
	configFile := filepath.Join(codexHome, "config.toml")
	winState := windowsSandboxAbsent
	if resolveGOOS(opts.GOOS) == "windows" {
		winState = resolveWindowsSandboxState(configFile, configSyncErr, statSharedCodexConfig(sharedHome), opts.CodexCustomArgs, logger)
	}
	policy := codexSandboxPolicyForConfig(opts.GOOS, opts.CodexVersion, winState)
	policy.WritableRoots = opts.WritableRoots
	if err := ensureCodexSandboxConfig(configFile, policy, opts.CodexVersion, logger); err != nil {
		// The managed block is the authoritative on-disk sandbox policy. If it
		// can't be written, config.toml keeps whatever it already had — on a
		// reused home that may be a stale danger-full-access from a prior run —
		// so the fail-closed policy just computed above would only exist in
		// memory while the effective config silently stays loose. Abort rather
		// than launch Codex with an unenforced sandbox: on fresh Prepare this
		// fails the task; on Reuse the caller leaves env.CodexHome unset, which
		// configureCodexTaskShellEnvironment then refuses to start (MUL-4957).
		return fmt.Errorf("ensure codex sandbox config: %w", err)
	}

	// Disable Codex native multi-agent inside daemon-managed task sessions
	// so the parent thread's `turn/completed` is not interpreted as task
	// completion while spawned subagents are still running. See
	// codex_multi_agent.go for the full rationale and escape hatch.
	if err := ensureCodexMultiAgentConfig(filepath.Join(codexHome, "config.toml"), logger); err != nil {
		logger.Warn("execenv: codex-home ensure multi-agent config failed", "error", err)
	}

	// Disable Codex native auto-memory inside daemon-managed task sessions
	// so cross-task and cross-workspace context leaks (multica#3130) cannot
	// happen via `codex-home/memories/` or `~/.codex/memories/`. See
	// codex_memory.go for the full rationale and escape hatch.
	if err := ensureCodexMemoryConfig(filepath.Join(codexHome, "config.toml"), logger); err != nil {
		logger.Warn("execenv: codex-home ensure memory config failed", "error", err)
	}

	return nil
}

// resolveSharedCodexHome returns the path to the user's shared Codex home.
// Checks $CODEX_HOME first, falls back to ~/.codex.
func resolveSharedCodexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		abs, err := filepath.Abs(v)
		if err == nil {
			return abs
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".codex") // last resort fallback
	}
	return filepath.Join(home, ".codex")
}

// codexSessionStateGlobs are the session-derived SQLite state Codex builds
// inside a CODEX_HOME by indexing everything under sessions/. They are dropped
// during the legacy-symlink migration (prepareCodexSessionsDir) so Codex
// rebuilds them from the now task-local sessions instead of keeping the
// thousands of stale rows it backfilled from the shared ~/.codex/sessions
// history. Everything matched here is a rebuildable derived index — never
// authoritative data.
//
// Deliberately NOT listed: session_index.jsonl, which Codex 0.144.x uses as the
// authoritative store for thread-id → user-set thread name (name edits land in
// SQLite AND this file, never back in the rollout), so it cannot be rebuilt from
// rollouts; and sibling per-task DBs with different prefixes (goals_*, logs_*,
// memories_*) which are not session-derived. All are left intact.
var codexSessionStateGlobs = []string{
	"state_*.sqlite",
	"state_*.sqlite-shm",
	"state_*.sqlite-wal",
}

// codexSessionStoreRoot is the directory under the shared Codex home that holds
// the per-issue session stores. It sits beside the user's own `sessions/` so it
// shares that volume (making resume-rollout hard links zero-copy) but is never
// enumerated by a plain `codex` run, keeping Multica task history out of the
// user's own thread list.
const codexSessionStoreRoot = "multica-sessions"

// codexSessionStoreDir returns the persistent, per-(agent, issue) Codex sessions
// store for key, rooted on the shared Codex home's volume. It survives across
// task IDs (unlike the task-scoped envRoot the GC reclaims) and holds only that
// issue's rollouts. Empty key → "" (caller keeps sessions/ task-local).
func codexSessionStoreDir(sharedHome, key string) string {
	if key == "" {
		return ""
	}
	return filepath.Join(sharedHome, codexSessionStoreRoot, key)
}

// codexSessionStoreNamespace maps a daemon's profile to the directory segment
// that isolates its session stores from another profile-daemon's when several
// run on the same machine sharing one ~/.codex (profiles get separate daemon
// state but the same Codex home). Each daemon writes under, and only ever
// reclaims, its own namespace, so a staging daemon's GC can never delete a
// production task's live store and vice versa.
//
// The map MUST be collision-free (distinct profiles are distinct daemons and
// must never share a namespace) AND fixed-length (a profile can be as long as a
// filesystem segment allows, ~255 bytes, so any length-expanding encoding would
// overflow the 255-byte limit and fail to create the store dir). A lossy "drop
// unsafe characters" scheme collides ("" vs "default", "staging.prod" vs
// "stagingprod"); a full hex encoding doubles the length and overflows. So the
// empty (default) profile gets a reserved bare literal, and every named profile
// is the hex of its SHA-256 — a constant 64 hex chars, filesystem-safe and
// collision-resistant — under a "p_" prefix the bare literal can never collide
// with (MUL-4424).
func codexSessionStoreNamespace(profile string) string {
	if profile == "" {
		return "default"
	}
	sum := sha256.Sum256([]byte(profile))
	return "p_" + hex.EncodeToString(sum[:])
}

// codexSessionStoreKey builds the per-(profile, agent, issue) key for a task's
// persistent Codex sessions store. The agent/issue IDs are server-issued UUIDs;
// all three segments are sanitized to bare path segments defensively so a
// malformed value can never escape the store root. Returns "" when there is no
// issue to key on (the store is issue-scoped), leaving sessions/ task-local.
func codexSessionStoreKey(profile, agentID, issueID string) string {
	issue := sanitizeCodexPathSegment(issueID)
	if issue == "" {
		return ""
	}
	agent := sanitizeCodexPathSegment(agentID)
	if agent == "" {
		agent = "_"
	}
	return filepath.Join(codexSessionStoreNamespace(profile), agent, issue)
}

// sanitizeCodexPathSegment reduces s to the characters a UUID uses (hex plus
// dashes/underscores), dropping everything else so the result is always a single
// safe path segment — no separators, no "..", no drive letters.
func sanitizeCodexPathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PruneCodexSessionStores reclaims per-issue Codex session stores under the
// shared home's multica-sessions root that have not been touched within
// retention, bounding the lifetime of the conversation history each one holds.
//
// The stores deliberately live outside the task-scoped envRoot the task GC
// reclaims (so resume survives across task IDs), which means without this they
// would accumulate forever — a done or abandoned issue's prompts and full
// rollouts (one reporter saw a single 1.5 GiB rollout) would never be freed, and
// deleting the issue/agent/workspace would not remove them. A store's newest
// mtime is its last activity: Codex writes/extends a rollout as the thread
// advances, so an active or recently-resumed task keeps its store fresh and is
// never reclaimed; a store idle past retention is removed, giving deleted issues
// an eventual-reclamation guarantee. retention <= 0 disables pruning entirely.
//
// It scans ONLY the caller profile's namespace, so a daemon never reclaims a
// store owned by another profile-daemon sharing the same ~/.codex — the
// in-process reservation guard cannot span processes, and the namespace makes
// their store trees disjoint so it does not need to (MUL-4424).
//
// reserve (may be nil) atomically claims a store for deletion: it returns
// ok=false when a live task holds the store — leaving it — and otherwise returns
// a commit to run once removal finishes. Because the caller's reservation and a
// task's mark-active go through one lock in the same process, a store a task is
// about to mount is never removed out from under it — the confirm-inactive and
// the remove are effectively atomic, closing the stat->remove race a plain
// point-in-time active check leaves open. nil disables the guard (tests): every
// idle store is removed.
func PruneCodexSessionStores(profile string, retention time.Duration, now time.Time, reserve func(storeDir string) (commit func(), ok bool), logger *slog.Logger) (removed int, bytesFreed int64) {
	if retention <= 0 {
		return 0, 0
	}
	root := filepath.Join(resolveSharedCodexHome(), codexSessionStoreRoot, codexSessionStoreNamespace(profile))
	agents, err := os.ReadDir(root)
	if err != nil {
		return 0, 0 // not created yet, or unreadable — nothing to prune
	}
	for _, a := range agents {
		if !a.IsDir() {
			continue
		}
		agentDir := filepath.Join(root, a.Name())
		issues, err := os.ReadDir(agentDir)
		if err != nil {
			continue
		}
		kept := 0
		for _, is := range issues {
			if !is.IsDir() {
				continue
			}
			storeDir := filepath.Join(agentDir, is.Name())
			newest, size := codexStoreStat(storeDir)
			if newest.IsZero() || now.Sub(newest) <= retention {
				kept++
				continue
			}
			// Atomically reserve the store before removing it. A live task holds
			// it (or is mounting it right now) → skip; otherwise the reservation
			// blocks any task from claiming it until the removal + commit finish.
			var commit func()
			if reserve != nil {
				c, ok := reserve(storeDir)
				if !ok {
					kept++
					continue
				}
				commit = c
			}
			err := os.RemoveAll(storeDir)
			if commit != nil {
				commit()
			}
			if err != nil {
				logger.Warn("execenv: prune codex session store failed", "store", storeDir, "error", err)
				kept++
				continue
			}
			removed++
			bytesFreed += size
		}
		// Remove the agent dir once its last issue store is gone, so the tree
		// does not leave empty <agent>/ shells behind.
		if kept == 0 {
			_ = os.Remove(agentDir)
		}
	}
	return removed, bytesFreed
}

// codexStoreStat walks dir once, returning the newest modification time seen
// (the store's last activity) and its total byte size (for GC accounting).
func codexStoreStat(dir string) (newest time.Time, size int64) {
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if !d.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return newest, size
}

// prepareCodexSessionsDir points codex-home/sessions at a sessions store that
// holds ONLY this task's own history, never the machine's whole
// ~/.codex/sessions.
//
// Background (MUL-4424): Codex 0.143+ backfills a per-home session-state DB by
// enumerating every rollout visible under sessions/ during `initialize`. When
// the per-task home symlinked the shared sessions dir in, a machine that had
// accumulated thousands of rollouts (one reporter hit ~2000 files / ~22 GiB)
// stalled `initialize` for tens of seconds — the app-server started but the
// task produced no output before it was cancelled. So we scope sessions/ to a
// single task/issue:
//
//   - local_directory task: envRoot (and thus codex-home) is fresh per task ID
//     and never reused, so sessions/ links to the per-issue store on the shared
//     Codex volume — stable across task IDs, GC-safe, and holding only this
//     issue's rollouts. See linkCodexSessionsToStore.
//   - Fresh managed task: sessions/ is absent — create an empty local dir so
//     backfill is trivial; the reused envRoot carries it to the next run.
//   - Reused managed task whose sessions/ is already a real dir: it is
//     authoritative — the prior run's rollout already lives here — leave it.
//   - Reused managed task still holding a legacy symlink into the shared
//     sessions (older build): migrate. With a resume, route it through the
//     per-issue store (cross-volume-safe); without one, replace the symlink
//     with an empty local dir. Either way drop the stale session-state DB so
//     Codex rebuilds it from the scoped sessions.
func prepareCodexSessionsDir(codexHome, sharedHome string, opts CodexHomeOptions, logger *slog.Logger) error {
	dst := filepath.Join(codexHome, "sessions")
	sharedSessions := filepath.Join(sharedHome, "sessions")
	storeDir := codexSessionStoreDir(sharedHome, opts.SessionStoreKey)

	// local_directory tasks have no reusable envRoot, so their history can only
	// persist across task IDs in the per-issue store. The daemon still verifies
	// the specific rollout is present before claiming a resume (see
	// CodexResumeRolloutPresent), so a missing one no longer masquerades as one.
	if opts.IsLocalDirectory {
		if storeDir == "" {
			// No stable per-issue key (e.g. a non-issue task). Fall back to an
			// empty local dir rather than re-exposing the whole shared history.
			return os.MkdirAll(dst, 0o755)
		}
		return linkCodexSessionsToStore(dst, storeDir, sharedSessions, opts.ResumeSessionID, logger)
	}

	fi, err := os.Lstat(dst)
	switch {
	case os.IsNotExist(err):
		return os.MkdirAll(dst, 0o755) // fresh managed task — empty local dir
	case err != nil:
		return fmt.Errorf("stat sessions dir %s: %w", dst, err)
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		// Already a real directory (task-local, authoritative). Ensure it
		// exists (no-op) and leave its contents alone.
		return os.MkdirAll(dst, 0o755)
	}

	// A symlink/junction. If it already points at this issue's store (a home
	// migrated with a resume on a prior reuse), it is authoritative — re-ensure
	// the store link and the resume rollout, then leave it.
	if storeDir != "" {
		if target, rlErr := os.Readlink(dst); rlErr == nil && sameCodexPath(target, storeDir) {
			return linkCodexSessionsToStore(dst, storeDir, sharedSessions, opts.ResumeSessionID, logger)
		}
	}

	// Legacy symlink into the shared ~/.codex/sessions — migrate it. Drop the
	// session-derived state so Codex re-indexes the scoped sessions instead of
	// the stale rows it built from the whole shared home.
	if err := os.Remove(dst); err != nil {
		return fmt.Errorf("remove legacy sessions symlink %s: %w", dst, err)
	}
	resetCodexSessionState(codexHome, logger)

	// With a resume, route through the per-issue store so the rollout is exposed
	// cross-volume-safely (hard link within the shared volume + a directory link
	// into the task home). Without a resume — or with no stable key — an empty
	// local dir is all a fresh thread needs.
	if opts.ResumeSessionID != "" && storeDir != "" {
		logger.Info("execenv: migrated codex-home sessions from shared symlink to per-issue store",
			"codex_home", codexHome, "resume_session", true)
		return linkCodexSessionsToStore(dst, storeDir, sharedSessions, opts.ResumeSessionID, logger)
	}
	logger.Info("execenv: migrated codex-home sessions from shared symlink to task-local dir",
		"codex_home", codexHome, "resume_session", false)
	return os.MkdirAll(dst, 0o755)
}

// linkCodexSessionsToStore points codex-home/sessions (dst) at the per-issue
// store (storeDir) via an idempotent directory link — a symlink on Unix, a
// junction on Windows — both of which cross filesystem volumes without special
// privilege. The store lives on the shared Codex home's volume, so linking the
// directory (rather than copying rollout files into the task home) is what makes
// resume exposure safe when WorkspacesRoot sits on a different disk than
// ~/.codex (MUL-4424, Windows cross-volume).
//
// When resuming and the store does not yet hold the rollout — e.g. the first run
// after upgrading from the old whole-shared-sessions layout, where the history
// still lives only under ~/.codex/sessions — it hard-links that rollout into the
// store. Both paths are on the shared volume, so the link is zero-copy and never
// puts a (possibly gigabyte) rollout on initialize's critical path.
func linkCodexSessionsToStore(dst, storeDir, sharedSessions, resumeID string, logger *slog.Logger) error {
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return fmt.Errorf("create codex session store %s: %w", storeDir, err)
	}
	if resumeID != "" && len(findCodexRollouts(storeDir, resumeID)) == 0 {
		if err := exposeResumeRollout(sharedSessions, storeDir, resumeID, logger); err != nil {
			logger.Warn("execenv: bootstrap resume rollout into session store failed; task will fall back to a fresh thread",
				"session_id", resumeID, "error", err)
		}
	}
	if err := ensureCodexSessionsLink(dst, storeDir); err != nil {
		return err
	}
	// Stamp the store as just-used. Mounting it (MkdirAll, rollout lookup, link)
	// does not touch its mtime, so without this the GC's idle check would still
	// see a >TTL-old store and could reclaim it before the resumed turn writes its
	// first rollout — reopening a long-idle issue must not lose context. This is
	// the activity refresh; the daemon's in-process active-store guard closes the
	// remaining stat→remove race (MUL-4424).
	touchCodexSessionStore(storeDir, logger)
	return nil
}

// touchCodexSessionStore refreshes storeDir's modification time to now — the
// signal codexStoreStat reads as the store's last activity. Best-effort: a
// failed touch only risks an over-eager prune, which the active-store guard
// still prevents.
func touchCodexSessionStore(storeDir string, logger *slog.Logger) {
	now := time.Now()
	if err := os.Chtimes(storeDir, now, now); err != nil {
		logger.Warn("execenv: refresh codex session store activity failed", "store", storeDir, "error", err)
	}
}

// CodexSessionStorePath returns the per-issue Codex session store directory for
// (profile, agentID, issueID) on the shared home, or "" when there is no stable
// key. The daemon marks this path in-use for the duration of a task so
// PruneCodexSessionStores never reclaims a store mid-mount, closing the
// stat→remove race the mtime refresh alone cannot (MUL-4424).
func CodexSessionStorePath(profile, agentID, issueID string) string {
	key := codexSessionStoreKey(profile, agentID, issueID)
	if key == "" {
		return ""
	}
	return codexSessionStoreDir(resolveSharedCodexHome(), key)
}

// sameCodexPath reports whether two filesystem paths refer to the same location,
// tolerating separator/cleanliness differences. Used to detect a sessions link
// that already points at the per-issue store so a reused home is not re-migrated.
func sameCodexPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// resetCodexSessionState removes the rebuildable, session-derived Codex state
// files from a per-task CODEX_HOME so the next `initialize` re-derives them from
// the task-local sessions. Only session-derived indexes are touched; unrelated
// per-task DBs (goals_*, logs_*, memories_*) are left intact.
func resetCodexSessionState(codexHome string, logger *slog.Logger) {
	for _, pattern := range codexSessionStateGlobs {
		matches, err := filepath.Glob(filepath.Join(codexHome, pattern))
		if err != nil {
			continue
		}
		for _, m := range matches {
			if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
				logger.Warn("execenv: codex-home reset session state failed", "path", m, "error", err)
			}
		}
	}
}

// ensureCodexSessionsLink points codex-home/sessions (dst) at the per-issue
// session store (src) via a directory link, creating the store if needed.
// Idempotent: a link already pointing at src is left as-is; anything else at dst
// (a real dir, a stale link, a legacy shared-sessions symlink) is replaced. The
// link crosses volumes without privilege (symlink on Unix, junction on Windows),
// so the store can live on the shared Codex volume while the task home lives
// under WorkspacesRoot (see linkCodexSessionsToStore).
func ensureCodexSessionsLink(dst, src string) error {
	if err := os.MkdirAll(src, 0o755); err != nil {
		return fmt.Errorf("create codex session store %s: %w", src, err)
	}
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, rlErr := os.Readlink(dst); rlErr == nil && sameCodexPath(target, src) {
				return nil
			}
		}
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("remove stale sessions path %s: %w", dst, err)
		}
	}
	return createDirLink(src, dst)
}

// codexRolloutGlobs returns the glob patterns that match a session's rollout
// under a Codex sessions directory. It covers the layouts Codex 0.14x writes:
// date-nested (sessions/YYYY/MM/DD/) and flat (directly under sessions/), each
// as a plain .jsonl or a background-compressed .jsonl.zst.
func codexRolloutGlobs(sessionsDir, sessionID string) []string {
	name := "rollout-*-" + sessionID + ".jsonl*" // .jsonl and .jsonl.zst
	return []string{
		filepath.Join(sessionsDir, name),
		filepath.Join(sessionsDir, "*", "*", "*", name),
	}
}

// findCodexRollouts returns every rollout file for sessionID under sessionsDir,
// across the supported layouts.
func findCodexRollouts(sessionsDir, sessionID string) []string {
	if sessionsDir == "" || sessionID == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, pattern := range codexRolloutGlobs(sessionsDir, sessionID) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// CodexResumeRolloutPresent reports whether sessionID's rollout is present in
// the task's codex-home sessions dir. The daemon uses this after preparing the
// environment to avoid claiming a resume Codex would silently restart from
// scratch — the rollout may be absent when a legacy home's migration could not
// locate it, or when a local_directory task's shared history has been pruned
// (MUL-4424).
func CodexResumeRolloutPresent(codexHome, sessionID string) bool {
	if codexHome == "" || sessionID == "" {
		return false
	}
	return len(findCodexRollouts(filepath.Join(codexHome, "sessions"), sessionID)) > 0
}

// exposeResumeRollout links sessionID's rollout(s) out of the shared sessions
// history into the task-local sessions dir, preserving the relative layout so
// thread/resume can find it. Covers plain and compressed rollouts in both the
// nested and flat layouts.
//
// It links rather than copies: a rollout can be large (one reporter saw a
// single 1.5 GiB file) and this runs on `initialize`'s critical path, so an
// unbounded copy would reintroduce the very stall we are fixing. See
// linkCodexRollout for the hard-link-then-symlink strategy; if neither works
// the caller treats the resume as unavailable and falls back to a fresh thread.
func exposeResumeRollout(sharedSessions, localSessions, sessionID string, logger *slog.Logger) error {
	matches := findCodexRollouts(sharedSessions, sessionID)
	if len(matches) == 0 {
		return fmt.Errorf("no rollout found for session %s under %s", sessionID, sharedSessions)
	}
	linked := 0
	for _, src := range matches {
		rel, err := filepath.Rel(sharedSessions, src)
		if err != nil {
			rel = filepath.Base(src)
		}
		dst := filepath.Join(localSessions, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create rollout dir %s: %w", filepath.Dir(dst), err)
		}
		if err := linkCodexRollout(src, dst); err != nil {
			return fmt.Errorf("link rollout %s: %w", src, err)
		}
		linked++
	}
	logger.Info("execenv: exposed resume rollout into task-local sessions", "session_id", sessionID, "files", linked)
	return nil
}

// linkCodexRollout materialises src at dst without copying its bytes: a hard
// link first (zero-copy, needs no special privilege and works on Windows within
// a volume), falling back to a symlink across filesystems. It never copies — a
// rollout can be gigabytes and this runs on initialize's critical path, so a
// copy would reintroduce the stall MUL-4424 fixes.
func linkCodexRollout(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return os.Symlink(src, dst)
}

func syncCodexModelCatalog(codexHome, sharedHome string) error {
	configPath := filepath.Join(codexHome, "config.toml")
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", configPath, err)
	}

	var cfg struct {
		ModelCatalogJSON string `toml:"model_catalog_json"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", configPath, err)
	}
	catalogPath := strings.TrimSpace(cfg.ModelCatalogJSON)
	if catalogPath == "" {
		return nil
	}

	src, err := resolveCodexConfigPath(catalogPath, sharedHome)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("model_catalog_json %q resolved to missing file %s: %w", catalogPath, src, err)
	}

	if filepath.IsAbs(catalogPath) || strings.HasPrefix(catalogPath, "~") {
		return nil
	}
	cleanCatalogPath := filepath.Clean(catalogPath)
	if !filepath.IsLocal(cleanCatalogPath) {
		return fmt.Errorf("model_catalog_json %q must be a local relative path or an absolute path", catalogPath)
	}
	dst := filepath.Join(codexHome, cleanCatalogPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create model catalog directory %s: %w", filepath.Dir(dst), err)
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("remove stale model catalog %s: %w", dst, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat model catalog %s: %w", dst, err)
	}
	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("copy model_catalog_json %s to %s: %w", src, dst, err)
	}
	return nil
}

// syncCodexModelsCache seeds models_cache.json once for a fresh task home and
// binds it to the shared provider/catalog configuration. Codex can replace the
// task-local cache after startup, so an unchanged binding preserves whatever
// the task last wrote rather than restoring a potentially stale shared copy.
//
// A changed or missing binding makes an existing cache unsafe: Codex's cache
// format (as of 0.144.x) records the client version and fetch time but not the
// provider identity. Reusing that cache after config.toml switches providers
// can therefore pair provider B with provider A's model catalog. Drop it and
// let Codex fetch a catalog for the new effective configuration. We
// deliberately do not seed the shared cache in this case because it carries
// the same provider-identity ambiguity.
func syncCodexModelsCache(codexHome, sharedHome string, freshHome bool) error {
	fingerprint, err := codexModelsCacheConfigFingerprint(sharedHome)
	if err != nil {
		return err
	}

	bindingPath := filepath.Join(codexHome, codexModelsCacheBindingFile)
	previous, bound, err := readCodexModelsCacheBinding(bindingPath)
	if err != nil {
		return err
	}

	cachePath := filepath.Join(codexHome, codexModelsCacheFile)
	cacheInfo, cacheErr := os.Lstat(cachePath)
	cacheExists := cacheErr == nil
	if cacheErr != nil && !os.IsNotExist(cacheErr) {
		return fmt.Errorf("stat codex models cache %s: %w", cachePath, cacheErr)
	}

	if bound && previous == fingerprint {
		// The cache belongs to the current config. Preserve both an existing
		// task-refreshed cache and an intentional absence after a failed fetch;
		// seeding on reuse could reintroduce an unbound shared snapshot.
		if cacheExists && !cacheInfo.Mode().IsRegular() {
			if err := os.RemoveAll(cachePath); err != nil {
				return fmt.Errorf("remove non-regular codex models cache %s: %w", cachePath, err)
			}
		}
		return nil
	}

	if cacheExists {
		if err := os.RemoveAll(cachePath); err != nil {
			return fmt.Errorf("remove unbound codex models cache %s: %w", cachePath, err)
		}
	}

	if freshHome && !bound && !cacheExists {
		// A shared snapshot is useful on the one path where the task home itself
		// did not exist yet; subsequent task-local refreshes stay isolated. An
		// existing legacy home without a binding never seeds because its prior
		// effective configuration is unknown even when its cache is absent.
		if err := seedCopiedFile(filepath.Join(sharedHome, codexModelsCacheFile), cachePath); err != nil {
			return fmt.Errorf("seed codex models cache: %w", err)
		}
	}

	if err := writeCodexModelsCacheBinding(bindingPath, fingerprint); err != nil {
		return err
	}
	return nil
}

// codexModelsCacheConfigFingerprint hashes the shared config files plus the
// contents of any model_catalog_json they reference. The digest is stored in
// the isolated task home; no config contents or credentials are persisted.
func codexModelsCacheConfigFingerprint(sharedHome string) (string, error) {
	h := sha256.New()
	var configTOML []byte

	for _, name := range codexModelsCacheConfigFiles {
		path := filepath.Join(sharedHome, name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			fmt.Fprintf(h, "%s\x00missing\x00", name)
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read codex model cache config %s: %w", path, err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
		_, _ = h.Write(data)
		if name == "config.toml" {
			configTOML = data
		}
	}

	if len(configTOML) > 0 {
		var cfg struct {
			ModelCatalogJSON string `toml:"model_catalog_json"`
		}
		if err := toml.Unmarshal(configTOML, &cfg); err != nil {
			return "", fmt.Errorf("parse codex model cache config %s: %w", filepath.Join(sharedHome, "config.toml"), err)
		}
		catalogPath := strings.TrimSpace(cfg.ModelCatalogJSON)
		if catalogPath != "" {
			resolved, err := resolveCodexConfigPath(catalogPath, sharedHome)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return "", fmt.Errorf("read model_catalog_json %s: %w", resolved, err)
			}
			fmt.Fprintf(h, "model_catalog_json\x00%d\x00", len(data))
			_, _ = h.Write(data)
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// readCodexModelsCacheBinding returns bound=false for a missing or non-regular
// marker. Non-regular paths are removed so a reused task cannot redirect the
// later binding write outside its isolated CODEX_HOME.
func readCodexModelsCacheBinding(path string) (fingerprint string, bound bool, err error) {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("stat codex models cache binding %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		if err := os.RemoveAll(path); err != nil {
			return "", false, fmt.Errorf("remove non-regular codex models cache binding %s: %w", path, err)
		}
		return "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read codex models cache binding %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), true, nil
}

func writeCodexModelsCacheBinding(path, fingerprint string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove prior codex models cache binding %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create codex models cache binding %s: %w", path, err)
	}
	if _, err := io.WriteString(f, fingerprint+"\n"); err != nil {
		_ = f.Close()
		return fmt.Errorf("write codex models cache binding %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close codex models cache binding %s: %w", path, err)
	}
	return nil
}

func resolveCodexConfigPath(configPath, sharedHome string) (string, error) {
	if filepath.IsAbs(configPath) {
		return filepath.Clean(configPath), nil
	}
	if strings.HasPrefix(configPath, "~/") || strings.HasPrefix(configPath, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve model_catalog_json %q: user home: %w", configPath, err)
		}
		return filepath.Join(home, configPath[2:]), nil
	}
	if strings.HasPrefix(configPath, "~") {
		return "", fmt.Errorf("model_catalog_json %q uses unsupported ~user expansion", configPath)
	}
	return filepath.Join(sharedHome, filepath.Clean(configPath)), nil
}

func exposeSharedCodexPluginCache(codexHome, sharedHome string) error {
	src := filepath.Join(sharedHome, "plugins", "cache")
	dst := filepath.Join(codexHome, "plugins", "cache")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return fmt.Errorf("create shared plugin cache dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create codex plugin dir: %w", err)
	}

	if fi, err := os.Lstat(dst); err == nil {
		isLink := fi.Mode()&os.ModeSymlink != 0
		if isLink {
			if target, readlinkErr := os.Readlink(dst); readlinkErr == nil && target == src {
				return nil
			}
			if err := os.Remove(dst); err != nil {
				return fmt.Errorf("remove stale plugin cache link: %w", err)
			}
		} else {
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("remove stale plugin cache path: %w", err)
			}
		}
	}

	if err := createDirLink(src, dst); err != nil {
		return fmt.Errorf("expose shared plugin cache: %w", err)
	}
	return nil
}

// ensureSymlink ensures dst tracks src. If src doesn't exist, it's a no-op.
// If dst is already a symlink pointing at src, it's a no-op. Otherwise — a
// wrong-target symlink, a broken symlink, or a regular file left over from a
// prior createFileLink copy fallback — dst is removed and recreated via
// createFileLink so the per-task home doesn't drift from the shared source.
//
// The "regular file" branch matters on Windows: when os.Symlink fails (no
// Developer Mode / not elevated), createFileLink falls back to copying the
// file. Without this re-creation step, a once-stale auth.json would never
// pick up token refreshes from the shared ~/.codex/auth.json, leaving Codex
// stuck on a revoked refresh token across env reuses (issue #2081).
func ensureSymlink(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // source doesn't exist — skip
	}

	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(dst); err == nil && target == src {
				return nil // symlink already points to src
			}
		}
		// Wrong-target symlink, broken symlink, or stale regular file —
		// drop it so createFileLink can re-link/re-copy from the current src.
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("remove stale dst %s: %w", dst, err)
		}
	}

	return createFileLink(src, dst)
}

// logCodexAuthState records the kind of auth.json the per-task CODEX_HOME
// ended up with — symlink (with target), regular file (with size + mtime),
// or missing — so an operator chasing refresh_token_reused / token_expired
// reports can immediately tell whether the per-task home is tracking the
// shared ~/.codex/auth.json or has drifted into a stale local copy.
//
// Never logs the file contents.
func logCodexAuthState(authPath string, logger *slog.Logger) {
	fi, err := os.Lstat(authPath)
	if err != nil {
		logger.Info("execenv: codex auth.json absent", "path", authPath, "error", err)
		return
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(authPath)
		logger.Info("execenv: codex auth.json is symlink", "path", authPath, "target", target)
		return
	}
	logger.Info("execenv: codex auth.json is regular file",
		"path", authPath,
		"size", fi.Size(),
		"mtime", fi.ModTime().UTC(),
	)
}

// (The daemon used to write a minimal inline config here; the authoritative
// sandbox/network directives now live in a managed block rendered by
// codex_sandbox.go's ensureCodexSandboxConfig so they can be updated
// idempotently without touching user-managed keys.)

// syncCopiedFile mirrors a per-task dst onto the current state of the shared
// src so the per-task copy tracks the shared source across Reuse() runs:
//
//   - src present, dst absent:  copy src → dst
//   - src present, dst present: drop dst and re-copy src → dst (refresh)
//   - src absent,  dst present: drop dst (the shared source has been removed,
//     so the per-task stale copy must not linger)
//   - src absent,  dst absent:  no-op
//
// Regression for MUL-2646: the prior "don't overwrite" guard left per-task
// config.toml / config.json / instructions.md stuck on whatever snapshot they
// were seeded with at first Prepare. A user who edited ~/.codex/config.toml
// between runs — switching the active [model_providers.X] base_url, pointing
// env_key at a freshly rotated API key, or removing the file outright to
// drop a provider — kept hitting the stale per-task copy on session resume,
// with Codex calling the new URL using the old key (or replaying a provider
// the user had since deleted from the shared config).
//
// For config.toml the subsequent ensureCodex{Sandbox,MultiAgent,Memory}Config
// passes recreate the file from scratch when the shared source is gone, so
// the per-task home keeps the daemon-managed defaults but loses every
// user-managed [model_providers.X] / model_provider line that no longer
// exists in the shared config. For config.json / instructions.md there is
// no daemon-managed default, so they simply disappear in lockstep with the
// shared source.
func syncCopiedFile(src, dst string) error {
	_, srcErr := os.Stat(src)
	srcMissing := os.IsNotExist(srcErr)
	if srcErr != nil && !srcMissing {
		return fmt.Errorf("stat src %s: %w", src, srcErr)
	}

	if _, err := os.Lstat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("remove stale dst %s: %w", dst, err)
		}
	}

	if srcMissing {
		return nil
	}
	return copyFile(src, dst)
}

// seedCopiedFile copies src only when dst has no task-local regular file.
// Unlike syncCopiedFile, it never overwrites or removes a cache refreshed by a
// prior run. Non-regular destinations are removed defensively so a reused task
// cannot turn the cache path into a link outside its isolated CODEX_HOME.
func seedCopiedFile(src, dst string) error {
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode().IsRegular() {
			return nil
		}
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("remove non-regular dst %s: %w", dst, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat dst %s: %w", dst, err)
	}

	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat src %s: %w", src, err)
	}
	return copyFile(src, dst)
}

// copyFile copies src to dst unconditionally.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	return nil
}
