package execenv

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

var (
	ensureCodexDirLink      = ensureDirSymlink
	ensureCodexDirJunction  = ensureDirJunction
	ensureCodexFileLink     = ensureSymlink
	ensureCodexFileHardLink = ensureHardLink
)

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
		if err := prepareCodexHomeDir(src, dst, logger); err != nil {
			logger.Warn("execenv: codex-home dir symlink failed", "dir", name, "error", err)
		}
	}

	// Symlink shared files (auth).
	for _, name := range codexSymlinkedFiles {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if err := prepareCodexHomeFile(src, dst, logger); err != nil {
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

	return nil
}

func prepareCodexHomeDir(src, dst string, logger *slog.Logger) error {
	if err := ensureCodexDirLink(src, dst); err == nil {
		return nil
	} else {
		linkErr := err

		if runtime.GOOS == "windows" {
			if err := ensureCodexDirJunction(src, dst); err == nil {
				logger.Info("execenv: codex-home dir fallback linked via junction", "dir", filepath.Base(dst))
				return nil
			}
		}

		if err := os.MkdirAll(dst, 0o755); err == nil {
			logger.Info("execenv: codex-home dir fallback using local directory", "dir", filepath.Base(dst))
			return nil
		} else {
			return errors.Join(linkErr, fmt.Errorf("local dir fallback failed: %w", err))
		}
	}
}

func prepareCodexHomeFile(src, dst string, logger *slog.Logger) error {
	if err := ensureCodexFileLink(src, dst); err == nil {
		return nil
	} else {
		linkErr := err

		if runtime.GOOS == "windows" {
			if err := ensureCodexFileHardLink(src, dst); err == nil {
				logger.Info("execenv: codex-home file fallback linked via hard link", "file", filepath.Base(dst))
				return nil
			}
		}

		if err := copyFileIfExists(src, dst); err == nil {
			logger.Info("execenv: codex-home file fallback copied shared file", "file", filepath.Base(dst))
			return nil
		} else {
			return errors.Join(linkErr, fmt.Errorf("copy fallback failed: %w", err))
		}
	}
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

func ensureDirJunction(src, dst string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("directory junctions are only supported on windows")
	}
	if err := os.MkdirAll(src, 0o755); err != nil {
		return fmt.Errorf("create shared dir %s: %w", src, err)
	}

	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(dst)
			if err == nil && target == src {
				return nil
			}
			_ = os.Remove(dst)
		} else {
			return nil
		}
	}

	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", dst, src)
	if output, err := cmd.CombinedOutput(); err != nil {
		if len(output) > 0 {
			return fmt.Errorf("mklink /J %s -> %s: %w: %s", dst, src, err, string(output))
		}
		return fmt.Errorf("mklink /J %s -> %s: %w", dst, src, err)
	}
	return nil
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

func ensureHardLink(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}

	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(dst)
			if err == nil && target == src {
				return nil
			}
			_ = os.Remove(dst)
		} else {
			return nil
		}
	}

	return os.Link(src, dst)
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
