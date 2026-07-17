package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// Background (MUL-4856)
//
// On Linux the Codex sandbox runs in `workspace-write` mode (Landlock): every
// path outside the task workdir (the sandbox cwd) is read-only. The agent's
// real $HOME is therefore read-only, which breaks the standard tools that write
// under `~` by default:
//
//   - npm writes `~/.npm/_cacache` → EROFS on `npm install`.
//   - Prisma hardcodes `os.homedir()/.cache/prisma` (it ignores XDG) → EROFS on
//     `prisma generate`. Only redirecting HOME can fix Prisma.
//
// Reads are unrestricted under workspace-write — only writes are sandboxed — so
// the fix is to (1) give each task a writable HOME under its env root, (2) point
// HOME/XDG/npm_config_cache at it, and (3) add that home to the Codex
// `writable_roots` (it lives outside the cwd, so it needs an explicit grant).
// Because the redirected HOME is otherwise empty, we seed it with symlinks to
// the user's real credential/identity files so private-registry `npm install`
// (and git/gh reads, when git is usable) keep working — reads through the
// symlink resolve the real file, which stays read-only, and those flows only
// read them. This mirrors the existing per-task CODEX_HOME seeding precedent.
//
// Scope: Linux codex only. macOS Codex uses danger-full-access (no filesystem
// sandbox), and Windows has no Landlock sandbox (and unreliable symlink
// permissions), so neither needs — nor should get — a redirected HOME. Other
// providers are not sandboxed either.
//
// `git commit` inside a linked worktree has the same root sandbox constraint:
// Codex resolves the worktree's `.git` pointer and keeps the external gitdir
// read-only even inside a writable_root. The repo checkout path handles that
// separately by giving Linux Codex tasks an isolated, task-local `.git`
// directory instead of widening this writable-root grant (#2925).

// taskHomeSeedEntries are top-level entries symlinked from the user's real home
// into the per-task HOME so credential/identity reads keep resolving after HOME
// is redirected. Best-effort: missing sources are skipped. Writable cache dirs
// (.npm, .cache, .local) are intentionally NOT seeded — tools create them fresh
// inside the writable task home.
var taskHomeSeedEntries = []string{
	".gitconfig",       // git identity, aliases, credential.helper, url rewrites
	".npmrc",           // npm registry + auth token
	".netrc",           // https credentials for git/curl
	".git-credentials", // git credential store helper
	".ssh",             // ssh keys/config for git over ssh (directory)
}

// taskHomeConfigSeedEntries are `~/.config/<name>` subdirectories symlinked into
// the per-task `.config` (XDG_CONFIG_HOME) so config-in-XDG tools keep their
// auth after XDG_CONFIG_HOME is redirected. Best-effort: missing sources are
// skipped.
var taskHomeConfigSeedEntries = []string{
	"gh",  // GitHub CLI auth (hosts.yml) used as a git credential helper
	"git", // XDG git config location
}

// prepareTaskHome creates a writable per-task HOME directory and seeds it with
// symlinks to the user's real credential/identity files so tools that write to
// `~` land in a sandbox-writable location while still reading the user's real
// git/npm/ssh credentials. Best-effort: seeding failures are logged, not fatal —
// the writable home still resolves the EROFS problem for cache writes even if a
// particular credential source is absent.
func prepareTaskHome(taskHome string, logger *slog.Logger) error {
	if err := os.MkdirAll(taskHome, 0o700); err != nil {
		return fmt.Errorf("create task home: %w", err)
	}

	sharedHome, err := os.UserHomeDir()
	if err != nil || sharedHome == "" {
		// No real home to seed from — the writable home still fixes cache
		// writes; git identity / credentials simply aren't seeded.
		if logger != nil {
			logger.Warn("execenv: task home: no user home to seed credentials from", "error", err)
		}
		return nil
	}

	for _, name := range taskHomeSeedEntries {
		src := filepath.Join(sharedHome, name)
		if _, err := os.Lstat(src); err != nil {
			continue // best-effort: skip missing sources
		}
		if err := ensureSymlink(src, filepath.Join(taskHome, name)); err != nil && logger != nil {
			logger.Warn("execenv: task home: seed symlink failed", "entry", name, "error", err)
		}
	}

	// Seed read-shared XDG config subdirs (gh auth, git config) under a real,
	// writable .config so other tools can still create new config there while
	// gh/git resolve the user's existing auth through the symlinks.
	configDir := filepath.Join(taskHome, ".config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		if logger != nil {
			logger.Warn("execenv: task home: create .config failed", "error", err)
		}
		return nil
	}
	sharedConfig := filepath.Join(sharedHome, ".config")
	for _, name := range taskHomeConfigSeedEntries {
		src := filepath.Join(sharedConfig, name)
		if _, err := os.Lstat(src); err != nil {
			continue // best-effort: skip missing sources
		}
		if err := ensureSymlink(src, filepath.Join(configDir, name)); err != nil && logger != nil {
			logger.Warn("execenv: task home: seed .config symlink failed", "entry", name, "error", err)
		}
	}

	return nil
}

// TaskHomeEnv returns the environment overrides that redirect a task's HOME and
// XDG base dirs into the per-task writable home. The daemon layers these onto
// the agent's environment so npm, Prisma, and other tools that default to `~`
// write into the sandbox-writable home instead of the read-only real one.
//
// XDG_* are set explicitly (not left to derive from HOME) so an inherited
// XDG_CACHE_HOME/etc. pointing at the read-only real home cannot win. Likewise
// npm_config_cache is set explicitly to override any inherited value.
func TaskHomeEnv(taskHome string) map[string]string {
	return map[string]string{
		"HOME":             taskHome,
		"XDG_CACHE_HOME":   filepath.Join(taskHome, ".cache"),
		"XDG_CONFIG_HOME":  filepath.Join(taskHome, ".config"),
		"XDG_DATA_HOME":    filepath.Join(taskHome, ".local", "share"),
		"XDG_STATE_HOME":   filepath.Join(taskHome, ".local", "state"),
		"npm_config_cache": filepath.Join(taskHome, ".npm"),
	}
}

// prepareCodexSandboxHome sets up the per-task writable HOME and computes the
// Codex `writable_roots`, but only for Linux codex under the workspace-write
// sandbox. On macOS (danger-full-access) and Windows (no Landlock sandbox) it
// returns an empty home and nil roots, and the caller leaves the real HOME in
// place. goos defaults to runtime.GOOS; it is a parameter so tests can exercise
// each platform deterministically.
//
// It returns the task home path (empty when no redirect is needed) and the
// writable roots to add to the config.toml — just the task home, which lives
// outside the sandbox cwd and is not a git metadata dir, so Codex does not
// re-protect it.
func prepareCodexSandboxHome(envRoot, goos, codexVersion string, logger *slog.Logger) (taskHome string, writableRoots []string, err error) {
	if envRoot == "" {
		// No task env root to anchor the home under (legacy local_directory
		// reuse fallback). Leave the real HOME in place.
		return "", nil, nil
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "linux" || codexSandboxPolicyFor(goos, codexVersion).Mode != "workspace-write" {
		return "", nil, nil
	}

	taskHome = filepath.Join(envRoot, "home")
	if err := prepareTaskHome(taskHome, logger); err != nil {
		return "", nil, err
	}
	return taskHome, []string{taskHome}, nil
}
