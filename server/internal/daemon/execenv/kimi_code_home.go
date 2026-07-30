package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Kimi Code CLI's entire data root lives under KIMI_CODE_HOME (default
// ~/.kimi-code): config.toml (provider credentials), credentials/ (OAuth
// tokens), mcp.json (user-level MCP servers), skills/ (its User-tier skill
// scan location — see skills docs at
// https://www.kimi.com/code/docs/en/kimi-code-cli/customization/skills.html),
// plugins/, session_index.jsonl, tasks/, cron/, logs/, bin/. Kimi does not
// read provider API keys from process env at all — only from config.toml —
// so redirecting KIMI_CODE_HOME to a fresh empty per-task directory without
// carrying over the shared config/credentials would silently break
// authentication for every kimi task.

// kimiSymlinkedFiles/dirs are the entries symlinked from the shared home into
// the per-task KIMI_CODE_HOME, mirroring how codexSymlinkedFiles shares
// auth.json: changes (e.g. an OAuth token refresh) propagate automatically,
// and the task keeps working with the user's real credentials and MCP
// servers. Everything else (skills/, plugins/, sessions, logs) is left
// task-local, created fresh/lazily by the CLI.
var kimiSymlinkedFiles = []string{
	"config.toml",
	"credentials",
	"mcp.json",
}

// resolveSharedKimiCodeHome mirrors resolveSharedCodexHome: an explicit
// KIMI_CODE_HOME env wins, else the CLI's documented default ~/.kimi-code.
func resolveSharedKimiCodeHome() string {
	if v := os.Getenv("KIMI_CODE_HOME"); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return abs
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".kimi-code") // last resort fallback
	}
	return filepath.Join(home, ".kimi-code")
}

// prepareKimiCodeHome builds the per-task KIMI_CODE_HOME directory: symlinks
// the shared config/credentials/mcp.json in (so auth and MCP servers keep
// working) and leaves everything else for the CLI to create lazily. Unlike
// Codex, Kimi needs no per-task config.toml edits (no sandbox policy
// injection), so config.toml is symlinked rather than copied.
func prepareKimiCodeHome(kimiHome string, logger *slog.Logger) error {
	sharedHome := resolveSharedKimiCodeHome()

	if err := os.MkdirAll(kimiHome, 0o755); err != nil {
		return fmt.Errorf("create kimi-code-home dir: %w", err)
	}

	for _, name := range kimiSymlinkedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(kimiHome, name)
		if err := ensureKimiSymlink(src, dst); err != nil {
			logger.Warn("execenv: kimi-code-home symlink failed", "file", name, "error", err)
		}
	}
	return nil
}

// hydrateKimiSkills rebuilds the task-local KIMI_CODE_HOME/skills dir from
// scratch so a skill removed since the last run can't linger, then writes
// only the Multica-bound skills. Placing them at KIMI_CODE_HOME/skills (the
// CLI's own User-tier scan location) is what makes discovery work by
// construction — no .git ancestor requirement, no reliance on the CLI
// trusting the daemon's cwd.
func hydrateKimiSkills(kimiHome string, workspaceSkills []SkillContextForEnv, logger *slog.Logger) error {
	skillsDir := filepath.Join(kimiHome, "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		return fmt.Errorf("clear kimi skills dir: %w", err)
	}
	if len(workspaceSkills) == 0 {
		return os.MkdirAll(skillsDir, 0o755)
	}
	// Skills live under env.RootDir/kimi-code-home, which the GC loop
	// (cloud) or env teardown (local_directory) wipes wholesale — no
	// sidecar manifest.
	return writeSkillFiles(skillsDir, workspaceSkills, nil)
}

// ensureKimiSymlink ensures dst tracks src, choosing a dir or file link based
// on src's kind (credentials/ is a directory; config.toml/mcp.json are
// files), mirroring linkSharedHermesEntry. Idempotent across Reuse: a link
// already pointing at src is left alone; a missing/dangling src is skipped,
// not failed, so a host with no prior Kimi home yet still gets a usable (if
// credential-less) environment.
func ensureKimiSymlink(src, dst string) error {
	info, err := os.Stat(src) // follow the link to decide dir vs file
	if err != nil {
		if os.IsNotExist(err) {
			return nil // source doesn't exist — skip
		}
		return fmt.Errorf("stat %s: %w", src, err)
	}

	if fi, lerr := os.Lstat(dst); lerr == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, rerr := os.Readlink(dst); rerr == nil && target == src {
				return nil // symlink already points at src
			}
		}
		// Wrong-target symlink, broken symlink, stale regular file/dir —
		// drop it so we can re-link current src.
		if rerr := os.RemoveAll(dst); rerr != nil {
			return fmt.Errorf("remove stale dst %s: %w", dst, rerr)
		}
	}

	if info.IsDir() {
		return createDirLink(src, dst)
	}
	return createFileLink(src, dst)
}
