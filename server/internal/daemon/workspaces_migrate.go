package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// MigrateLegacyWorkspacesRoot relocates per-profile workspace dirs (the
// pre-change layout `~/multica_workspaces_<profile>/`) into the unified
// `~/multica_workspaces/` directory, so Desktop, Web, and CLI all see the
// same local workspace files for the same workspace_id.
//
// The migration is a one-time best-effort op invoked at daemon startup. It is
// silently skipped when:
//   - cfg.Profile is empty (default profile already used the unified path);
//   - MULTICA_WORKSPACES_ROOT is set (user opted out of the default layout —
//     local-dev worktrees rely on this);
//   - cfg.WorkspacesRoot is not the default `~/multica_workspaces` (user
//     pointed it elsewhere via Overrides);
//   - the legacy directory does not exist.
//
// Failures (permission, EXDEV, etc.) are logged at warn level but never block
// daemon startup — the daemon falls back to the default unified path and the
// stale legacy dir stays in place.
func MigrateLegacyWorkspacesRoot(cfg Config, logger *slog.Logger) {
	if cfg.Profile == "" {
		return
	}
	if strings.TrimSpace(os.Getenv("MULTICA_WORKSPACES_ROOT")) != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	defaultRoot, err := filepath.Abs(filepath.Join(home, "multica_workspaces"))
	if err != nil {
		return
	}
	if cfg.WorkspacesRoot != defaultRoot {
		return
	}
	legacyRoot := filepath.Join(home, "multica_workspaces_"+cfg.Profile)
	info, err := os.Stat(legacyRoot)
	if err != nil || !info.IsDir() {
		return
	}

	if err := migrateWorkspacesRoot(legacyRoot, defaultRoot); err != nil {
		logger.Warn("migrate legacy workspaces root failed; legacy dir kept in place",
			"legacy", legacyRoot, "target", defaultRoot, "error", err)
		return
	}
	logger.Info("migrated legacy workspaces root",
		"legacy", legacyRoot, "target", defaultRoot)
}

// migrateWorkspacesRoot moves every immediate child of legacyRoot into
// targetRoot. Children whose name already exists at the target are left
// untouched in legacyRoot (we never overwrite). If, after the move pass,
// legacyRoot is empty, it is removed; otherwise it is kept so the user can
// reconcile the leftovers manually.
func migrateWorkspacesRoot(legacyRoot, targetRoot string) error {
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return fmt.Errorf("ensure target root: %w", err)
	}

	entries, err := os.ReadDir(legacyRoot)
	if err != nil {
		return fmt.Errorf("read legacy root: %w", err)
	}

	var firstErr error
	for _, e := range entries {
		src := filepath.Join(legacyRoot, e.Name())
		dst := filepath.Join(targetRoot, e.Name())

		if _, err := os.Lstat(dst); err == nil {
			// Target already exists — leave src in place to avoid clobbering.
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			if firstErr == nil {
				firstErr = fmt.Errorf("stat target %s: %w", dst, err)
			}
			continue
		}

		if err := os.Rename(src, dst); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("rename %s -> %s: %w", src, dst, err)
			}
		}
	}

	if firstErr != nil {
		return firstErr
	}

	// If everything was relocated cleanly, prune the empty legacy root.
	remaining, err := os.ReadDir(legacyRoot)
	if err == nil && len(remaining) == 0 {
		_ = os.Remove(legacyRoot)
	}
	return nil
}
