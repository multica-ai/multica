package execenv

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Files to symlink from the shared ~/.codex/ into the per-task CODEX_HOME.
// Symlinks share state (e.g. auth tokens) so changes propagate automatically.
var codexSymlinkedFiles = []string{
	"auth.json",
}

// Files to copy from the shared ~/.codex/ into the per-task CODEX_HOME.
// Copies are isolated — changes don't affect the shared home.
var codexCopiedFiles = []string{
	"config.json",
	"config.toml",
	"instructions.md",
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
	// resume, when any. It is only consulted while migrating a legacy per-task
	// home whose sessions/ still symlinks the shared ~/.codex/sessions: the
	// single rollout for this ID is exposed into the new task-local sessions
	// dir so thread/resume can still find it without pulling the whole shared
	// history back in. Empty means a fresh thread (no rollout to expose). See
	// prepareCodexSessionsDir (MUL-4424).
	ResumeSessionID string
}

// prepareCodexHome is a thin wrapper around prepareCodexHomeWithOpts kept for
// tests that don't care about platform-aware sandbox configuration. It
// assumes a Linux-like environment where workspace-write + network_access
// works correctly.
func prepareCodexHome(codexHome string, logger *slog.Logger) error {
	return prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{GOOS: "linux"}, logger)
}

// prepareCodexHomeWithOpts creates a per-task CODEX_HOME directory and seeds
// it with config from the shared ~/.codex/ home. Auth is symlinked (shared),
// config files are copied (isolated). The per-task config.toml gets a
// daemon-managed sandbox block picked by codexSandboxPolicyFor.
func prepareCodexHomeWithOpts(codexHome string, opts CodexHomeOptions, logger *slog.Logger) error {
	sharedHome := resolveSharedCodexHome()

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

	// Sync config files from the shared source (isolated per task).
	for _, name := range codexCopiedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := syncCopiedFile(src, dst); err != nil {
			logger.Warn("execenv: codex-home sync failed", "file", name, "error", err)
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

	if err := exposeSharedCodexPluginCache(codexHome, sharedHome); err != nil {
		logger.Warn("execenv: codex-home plugin cache exposure failed", "error", err)
	}

	// Write a daemon-managed sandbox block into config.toml. On macOS we may
	// need to fall back to danger-full-access because of openai/codex#10390;
	// see codex_sandbox.go for the full rationale.
	policy := codexSandboxPolicyFor(opts.GOOS, opts.CodexVersion)
	if err := ensureCodexSandboxConfig(filepath.Join(codexHome, "config.toml"), policy, opts.CodexVersion, logger); err != nil {
		logger.Warn("execenv: codex-home ensure sandbox config failed", "error", err)
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

// codexSessionStateGlobs are the session-derived state artifacts Codex builds
// inside a CODEX_HOME by indexing everything under sessions/. They are dropped
// during the legacy-symlink migration (prepareCodexSessionsDir) so Codex
// rebuilds them from the now task-local sessions instead of keeping the
// thousands of stale rows it backfilled from the shared ~/.codex/sessions
// history. Everything matched here is a rebuildable derived index — never
// authoritative data. Sibling per-task DBs with different prefixes (goals_*,
// logs_*, memories_*) are NOT session-derived and are deliberately left intact.
var codexSessionStateGlobs = []string{
	"state_*.sqlite",
	"state_*.sqlite-shm",
	"state_*.sqlite-wal",
	"session_index.jsonl",
}

// prepareCodexSessionsDir makes codex-home/sessions a real, task-local
// directory rather than a symlink into the shared ~/.codex/sessions.
//
// Background (MUL-4424): Codex 0.143+ backfills a per-home session-state DB by
// enumerating every rollout visible under sessions/ during `initialize`. When
// the per-task home symlinked the shared sessions dir in, a machine that had
// accumulated thousands of rollouts (one reporter hit ~2000 files / ~22 GiB)
// stalled `initialize` for tens of seconds — the app-server started but the
// task produced no output before it was cancelled. A task's fresh session has
// no business carrying the whole machine's Codex history, so we isolate it:
//
//   - Fresh task: sessions/ is absent — create an empty local dir so backfill
//     is trivial.
//   - Reused task whose sessions/ is already a real dir: it is authoritative —
//     the prior run's rollout already lives here — so leave it untouched.
//   - Reused task still holding a legacy symlink (created by an older build):
//     migrate in place. Replace the symlink with a real dir; when resuming,
//     expose only the single rollout being resumed; and drop the stale
//     session-state DB so Codex rebuilds it from the task-local sessions.
func prepareCodexSessionsDir(codexHome, sharedHome string, opts CodexHomeOptions, logger *slog.Logger) error {
	dst := filepath.Join(codexHome, "sessions")

	fi, err := os.Lstat(dst)
	switch {
	case os.IsNotExist(err):
		return os.MkdirAll(dst, 0o755) // fresh task — empty local dir
	case err != nil:
		return fmt.Errorf("stat sessions dir %s: %w", dst, err)
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		// Already a real directory (task-local, authoritative). Ensure it
		// exists (no-op) and leave its contents alone.
		return os.MkdirAll(dst, 0o755)
	}

	// Legacy symlink into the shared sessions dir — migrate it in place.
	sharedSessions, _ := os.Readlink(dst)
	if sharedSessions == "" {
		sharedSessions = filepath.Join(sharedHome, "sessions")
	}
	if err := os.Remove(dst); err != nil {
		return fmt.Errorf("remove legacy sessions symlink %s: %w", dst, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create task-local sessions dir %s: %w", dst, err)
	}

	// Drop the session-derived state so Codex re-indexes the task-local
	// sessions instead of the stale rows it built from the shared home.
	resetCodexSessionState(codexHome, logger)

	// When this run intends to resume a specific session, expose just that one
	// rollout so thread/resume can find it — without pulling the whole shared
	// history back in.
	if opts.ResumeSessionID != "" {
		if err := exposeResumeRollout(sharedSessions, dst, opts.ResumeSessionID, logger); err != nil {
			logger.Warn("execenv: codex-home expose resume rollout failed; task will fall back to a fresh thread",
				"session_id", opts.ResumeSessionID, "error", err)
		}
	}

	logger.Info("execenv: migrated codex-home sessions from shared symlink to task-local dir",
		"codex_home", codexHome, "resume_session", opts.ResumeSessionID != "")
	return nil
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

// exposeResumeRollout links the single rollout file for sessionID out of the
// shared sessions history into the task-local sessions dir, preserving Codex's
// sessions/YYYY/MM/DD layout so thread/resume can find it.
//
// It symlinks rather than copies: a rollout can be large (one reporter saw a
// single 1.5 GiB file), and this runs on `initialize`'s critical path, so an
// unbounded copy would reintroduce the very stall we are fixing. os.Symlink is
// used directly (no copy fallback) — if the link can't be created (e.g. Windows
// without the privilege) the resume is skipped and the caller falls back to a
// fresh thread rather than copying gigabytes.
func exposeResumeRollout(sharedSessions, localSessions, sessionID string, logger *slog.Logger) error {
	if sharedSessions == "" || sessionID == "" {
		return nil
	}
	// Rollout files are named rollout-<timestamp>-<sessionID>.jsonl under
	// sessions/YYYY/MM/DD/. Match by the trailing session ID.
	pattern := filepath.Join(sharedSessions, "*", "*", "*", "rollout-*-"+sessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob rollout for %s: %w", sessionID, err)
	}
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
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("link rollout %s: %w", src, err)
		}
		linked++
	}
	logger.Info("execenv: exposed resume rollout into task-local sessions", "session_id", sessionID, "files", linked)
	return nil
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
