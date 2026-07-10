package execenv

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Directories to symlink from the shared ~/.codex/ into the per-task CODEX_HOME.
// The shared directory is created if it doesn't exist, ensuring Codex session
// logs are always written to the global home where users can find them.
var codexSymlinkedDirs = []string{
	"sessions",
}

// Optional directories to symlink from the shared ~/.codex/ into the per-task
// CODEX_HOME. Unlike codexSymlinkedDirs, a missing source is NOT created —
// environments without Codex hooks must not sprout empty hook directories.
// A missing or non-directory source instead clears any stale per-task residue
// so a removed ~/.codex/hooks/ does not survive workspace reuse.
var codexOptionalSymlinkedDirs = []string{
	"hooks",
}

// Files to symlink from the shared ~/.codex/ into the per-task CODEX_HOME.
// Symlinks share state (e.g. auth tokens) so changes propagate automatically.
var codexSymlinkedFiles = []string{
	"auth.json",
}

// Optional files to symlink from the shared ~/.codex/ into the per-task
// CODEX_HOME. A missing or non-regular source clears any stale per-task
// copy/link so removed hook configuration does not linger across workspace
// reuse and get loaded by a later Codex session.
var codexOptionalSymlinkedFiles = []string{
	"hooks.json",
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

	// Symlink shared directories (sessions) so logs stay in the global home.
	for _, name := range codexSymlinkedDirs {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := ensureDirSymlink(src, dst); err != nil {
			logger.Warn("execenv: codex-home dir symlink failed", "dir", name, "error", err)
		}
	}

	// Expose optional shared directories (hooks/) only when the source is a
	// valid directory; otherwise clear stale per-task residue. See
	// ensureExistingDirSymlink.
	//
	// Fail-loud (design ②): propagate any error. ensureExistingDirSymlink
	// returns nil for the common no-op — shared source absent and nothing stale
	// to clear — so this does NOT block a task that simply has no hooks. It
	// returns an error only on a genuine problem: the shared source exists (or
	// its state can't be verified) but we failed to expose it, OR stale per-task
	// residue could not be removed. Both must abort task prep: the first would
	// start a Codex session silently lacking the user's hooks ("false success"),
	// the second would let a removed hook survive into the session.
	for _, name := range codexOptionalSymlinkedDirs {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := ensureExistingDirSymlink(src, dst); err != nil {
			return fmt.Errorf("expose codex optional dir %q into per-task home: %w", name, err)
		}
	}

	// Symlink shared files (auth).
	for _, name := range codexSymlinkedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := ensureSymlink(src, dst); err != nil {
			logger.Warn("execenv: codex-home symlink failed", "file", name, "error", err)
		}
	}

	// Expose optional shared files (hooks.json) only when the source is a
	// valid regular file; otherwise clear stale per-task residue. See
	// ensureOptionalFileSymlink. Same fail-loud rule as the optional dirs above
	// (design ②): nil on the no-hooks no-op, error on a real expose/cleanup
	// failure.
	for _, name := range codexOptionalSymlinkedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := ensureOptionalFileSymlink(src, dst); err != nil {
			return fmt.Errorf("expose codex optional file %q into per-task home: %w", name, err)
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

	// Mirror Codex hook trust state LAST, after every other config.toml
	// mutation (sanitize / sandbox / multi-agent / memory) has settled (design
	// D4). Codex keys hook trust by the hooks.json *source* path, but the
	// per-task home loads codex-home/hooks.json — so trust the user accepted
	// for the shared ~/.codex/hooks.json must be re-keyed onto that per-task
	// source ID. Running this last means it reads the final config and cannot
	// be clobbered by, nor clobber, the managed blocks above.
	//
	// The shared/task hooks.json paths are derived via codexHooksSourceID so
	// the re-keyed trust block's source id is byte-identical to what Codex
	// itself computes (design ①).
	hookTrustResult, err := syncCodexHookTrustStateWithResult(
		filepath.Join(sharedHome, "config.toml"),
		filepath.Join(codexHome, "config.toml"),
		codexHooksSourceID(sharedHome),
		codexHooksSourceID(codexHome),
	)
	if err != nil {
		// Fail-loud (design ②): propagate any error. When the shared hooks.json
		// is absent and there is no stale mapped block to clear, this call
		// returns nil, so a hookless task is not blocked. It errors only on a
		// real failure — the user has a shared hooks.json but we could not
		// re-key its trust (Codex would then silently treat the inherited hook
		// as untrusted and skip it — a "false success"), or a stale mapped trust
		// block could not be cleared (it would survive into the session). Both
		// abort task prep.
		return fmt.Errorf("mirror codex hook trust state onto per-task home: %w", err)
	}
	// Log paths and counts only — never hook contents, tokens, or secrets.
	logger.Info("execenv: codex-home hook trust sync",
		"codex_home", codexHome,
		"shared_hooks", hookTrustResult.SharedHooksCount,
		"mapped_hooks", hookTrustResult.MappedHooksCount,
		"stale_hooks", hookTrustResult.StaleHooksCount,
		"changed", hookTrustResult.Changed)

	return nil
}

// codexHooksSourceID returns the absolute hooks.json path that Codex uses as the
// *source id* when keying hook trust state for a hooks.json living directly
// under a Codex home (codexHome/hooks.json). It must be byte-identical to what
// Codex itself computes, or a re-keyed trust block will never be found and the
// inherited hook is silently treated as untrusted.
//
// Codex derives that id as AbsolutePathBuf::from_absolute_path(CODEX_HOME/hooks.json)
// then source_path.display().to_string() (openai/codex
// codex-rs/hooks/src/engine/discovery.rs + codex-rs/utils/absolute-path).
// AbsolutePathBuf is *lexically normalized* (`.` / `..` / redundant separators
// collapsed) but NOT canonicalized: it does not resolve symlinks. filepath.Join
// applies the same lexical normalization (via filepath.Clean) and likewise does
// not resolve symlinks, so it mirrors that semantics.
//
// CRITICAL — do NOT filepath.EvalSymlinks / otherwise resolve this path. The
// per-task hooks.json is itself a symlink into the shared ~/.codex/hooks.json;
// resolving it yields the shared path, which is NOT what Codex keys on, turning
// a currently-matching key into a guaranteed mismatch (silent untrust). The
// real-environment test (hook actually executes) is what confirms the key
// matches; keep this a pure lexical join.
func codexHooksSourceID(codexHome string) string {
	return filepath.Join(codexHome, "hooks.json")
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

// ensureDirSymlink creates a symlink dst → src for a directory.
// Unlike ensureSymlink, it creates the source directory if it doesn't exist,
// so Codex can write to it immediately.
func ensureDirSymlink(src, dst string) error {
	if err := os.MkdirAll(src, 0o755); err != nil {
		return fmt.Errorf("create shared dir %s: %w", src, err)
	}

	// Check if dst already exists.
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(dst)
			if err == nil && target == src {
				return nil // already correct
			}
			os.Remove(dst)
		} else {
			// Regular file/dir exists — don't overwrite.
			return nil
		}
	}

	return createDirLink(src, dst)
}

// ensureExistingDirSymlink links dst → src only when src is an existing
// directory. It is the optional-resource counterpart of ensureDirSymlink: the
// source is NEVER created, and a missing / non-directory / unreadable source
// clears any stale dst residue (design action 1 + D2/D5).
//
//   - src missing (ENOENT):     clear stale dst, no-op.
//   - src stat non-ENOENT error: clear stale dst, then surface the error
//     (D2 fail closed — never leave a stale hooks link loadable when we can't
//     verify the shared source).
//   - src not a directory:      clear stale dst (type mismatch).
//   - src is a directory:       keep a correct dst symlink; otherwise drop the
//     stale dst (via removeOptionalPath, which is junction-safe — D5) and
//     recreate the link.
func ensureExistingDirSymlink(src, dst string) error {
	fi, err := os.Stat(src)
	if os.IsNotExist(err) {
		return removeOptionalPath(dst)
	}
	if err != nil {
		// D2: fail closed. Clear any stale dst first so a removed/unreadable
		// source can never keep an old hook loadable, then surface the error.
		statErr := fmt.Errorf("stat shared dir %s: %w", src, err)
		if rmErr := removeOptionalPath(dst); rmErr != nil {
			return errors.Join(statErr, rmErr)
		}
		return statErr
	}
	if !fi.IsDir() {
		return removeOptionalPath(dst)
	}

	if lfi, err := os.Lstat(dst); err == nil {
		if lfi.Mode()&os.ModeSymlink != 0 {
			if target, rlErr := os.Readlink(dst); rlErr == nil && target == src {
				return nil // already the correct link
			}
		}
		if err := removeOptionalPath(dst); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("lstat optional dir dst %s: %w", dst, err)
	}

	return createDirLink(src, dst)
}

// ensureOptionalFileSymlink links/copies dst → src only when src is an existing
// regular file. Missing / non-regular / unreadable sources clear any stale
// per-task copy/link so workspace reuse reflects the shared home lifecycle
// (design action 1 + D2/D5). Same branch semantics as ensureExistingDirSymlink,
// specialized to a regular file and createFileLink.
func ensureOptionalFileSymlink(src, dst string) error {
	fi, err := os.Stat(src)
	if os.IsNotExist(err) {
		return removeOptionalPath(dst)
	}
	if err != nil {
		// D2: fail closed — clear stale dst then surface the error.
		statErr := fmt.Errorf("stat shared file %s: %w", src, err)
		if rmErr := removeOptionalPath(dst); rmErr != nil {
			return errors.Join(statErr, rmErr)
		}
		return statErr
	}
	if !fi.Mode().IsRegular() {
		return removeOptionalPath(dst)
	}

	if lfi, err := os.Lstat(dst); err == nil {
		if lfi.Mode()&os.ModeSymlink != 0 {
			if target, rlErr := os.Readlink(dst); rlErr == nil && target == src {
				return nil // already the correct link
			}
		}
		if err := removeOptionalPath(dst); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("lstat optional file dst %s: %w", dst, err)
	}

	return createFileLink(src, dst)
}

// removeOptionalPath removes a per-task optional resource (hooks.json or
// hooks/) without EVER following a symlink or a Windows junction into its
// shared target (design D5).
//
// os.Remove drops a symlink, a Windows junction reparse point, a regular file,
// or an empty directory in place — it never recurses through a link into the
// shared ~/.codex/hooks target. os.RemoveAll is reached only as a fallback for
// a genuine, non-empty real directory; the per-task home never builds a real
// hooks directory itself, so that branch is purely defensive. This is why we
// must not blindly os.RemoveAll a non-symlink dst as the reference PR did: on
// Windows a junction is not always reported as a symlink, and RemoveAll would
// delete the shared hooks contents through it.
func removeOptionalPath(path string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lstat optional dst %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove optional dst link %s: %w", path, err)
		}
		return nil
	}
	// Regular file, empty dir, or Windows junction: os.Remove handles all of
	// these in place, deleting the entry (or reparse point) without touching a
	// link target. Only a non-empty real directory makes os.Remove fail.
	if err := os.Remove(path); err == nil {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove optional dst path %s: %w", path, err)
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
