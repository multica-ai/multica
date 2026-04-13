package execenv

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Directories to symlink from the shared ~/.codex/ into the per-task CODEX_HOME.
// The shared directory is created if it doesn't exist, ensuring Codex session
// logs are always written to the global home where users can find them.
var codexSymlinkedDirs = []string{
	"sessions",
}

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

// prepareCodexHome creates a per-task CODEX_HOME directory and seeds it with
// config from the shared ~/.codex/ home. Auth is symlinked (shared), config
// files are copied (isolated).
func prepareCodexHome(codexHome string, logger *slog.Logger) error {
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

	// Symlink shared files (auth).
	for _, name := range codexSymlinkedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := ensureSymlink(src, dst); err != nil {
			logger.Warn("execenv: codex-home symlink failed", "file", name, "error", err)
		}
	}

	// Copy config files (isolated per task).
	for _, name := range codexCopiedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := copyFileIfExists(src, dst); err != nil {
			logger.Warn("execenv: codex-home copy failed", "file", name, "error", err)
		}
	}

	// Ensure config.toml has workspace-write sandbox with network access enabled.
	// Codex needs network access to reach the Multica API (api.multica.ai).
	if err := ensureCodexNetworkAccess(filepath.Join(codexHome, "config.toml")); err != nil {
		logger.Warn("execenv: codex-home ensure network access failed", "error", err)
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
		return filepath.Join("/tmp", ".codex") // last resort fallback
	}
	return filepath.Join(home, ".codex")
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

	return os.Symlink(src, dst)
}

// ensureSymlink creates a symlink dst → src. If src doesn't exist, it's a no-op.
// If dst already exists as a correct symlink, it's a no-op. If dst is a broken
// symlink, it's replaced.
func ensureSymlink(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // source doesn't exist — skip
	}

	// Check if dst already exists.
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			// It's a symlink — check if it points to the right place.
			target, err := os.Readlink(dst)
			if err == nil && target == src {
				return nil // already correct
			}
			// Wrong target — remove and recreate.
			os.Remove(dst)
		} else {
			// Regular file exists — don't overwrite.
			return nil
		}
	}

	return os.Symlink(src, dst)
}

// defaultCodexConfig is the minimal config.toml for Codex tasks.
// It sets workspace-write sandbox mode with network access enabled so the
// Multica CLI can reach api.multica.ai.
const defaultCodexConfig = `sandbox_mode = "workspace-write"

[sandbox_workspace_write]
network_access = true
`

// ensureCodexNetworkAccess ensures that config.toml exists and contains the
// sandbox_workspace_write section with network_access = true. If the file
// doesn't exist, it creates one with defaults. If it exists but lacks the
// network_access setting, the section is appended.
func ensureCodexNetworkAccess(configPath string) error {
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		// No config.toml — create with defaults.
		return os.WriteFile(configPath, []byte(defaultCodexConfig), 0o644)
	}
	if err != nil {
		return fmt.Errorf("read config.toml: %w", err)
	}

	content := string(data)

	// If the file already has network_access configured under sandbox_workspace_write, leave it alone.
	if strings.Contains(content, "[sandbox_workspace_write]") && strings.Contains(content, "network_access") {
		return nil
	}

	// Append the section. If sandbox_mode is already set, only append the section block.
	var appendStr string
	if strings.Contains(content, "[sandbox_workspace_write]") {
		// Section exists but missing network_access — append the key under it.
		content = strings.Replace(content, "[sandbox_workspace_write]", "[sandbox_workspace_write]\nnetwork_access = true", 1)
		return os.WriteFile(configPath, []byte(content), 0o644)
	}

	// Section doesn't exist — append both sandbox_mode (if missing) and the section.
	appendStr = "\n"
	if !strings.Contains(content, "sandbox_mode") {
		appendStr += "sandbox_mode = \"workspace-write\"\n"
	}
	appendStr += "\n[sandbox_workspace_write]\nnetwork_access = true\n"

	return os.WriteFile(configPath, append(data, []byte(appendStr)...), 0o644)
}

// copyFileIfExists copies src to dst. If src doesn't exist, it's a no-op.
// If dst already exists, it's not overwritten.
func copyFileIfExists(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}

	// Don't overwrite existing file.
	if _, err := os.Stat(dst); err == nil {
		return nil
	}

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
