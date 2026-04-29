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

	if err := migrateWorkspacesRoot(legacyRoot, defaultRoot, cfg.Profile); err != nil {
		logger.Warn("migrate legacy workspaces root failed; legacy dir kept in place",
			"legacy", legacyRoot, "target", defaultRoot, "error", err)
		return
	}
	logger.Info("migrated legacy workspaces root",
		"legacy", legacyRoot, "target", defaultRoot)
}

// migrateWorkspacesRoot moves the contents of legacyRoot into targetRoot.
//
// The on-disk layout is `<root>/<workspace_id>/<task_short>/...`, so when a
// `<workspace_id>` directory already exists at the target (because Web/CLI
// has run a task there) we still want to relocate the per-task subdirs from
// the legacy `<workspace_id>` rather than abandon them. The walk merges one
// level deep for workspace_id-shaped entries.
//
// The legacy `.repos` bare-clone cache gets special treatment: it is renamed
// to `.repos-<profile>` at the target. Two profiled daemons must not share a
// single `.repos/` (repocache.repoLocks is process-local and won't protect
// concurrent git mutations across processes).
//
//   - If the legacy entry name does not exist at the target, the whole entry
//     is renamed in one syscall (fast path; covers fresh workspaces).
//   - For `.repos`, the rename target is always `.repos-<profile>` regardless
//     of whether `.repos` already exists at the target — they belong to
//     different daemons. If `.repos-<profile>` already exists at the target,
//     the legacy `.repos` is left in place (we don't overwrite a previously
//     migrated cache).
//   - Otherwise, if both sides are directories AND the entry name does not
//     start with `.` (i.e. it looks like a workspace_id, not a hidden file),
//     recurse and rename each task_short subdir whose name is free at the
//     target. Conflicting task_short subdirs stay in legacy for manual
//     reconciliation.
//   - Other conflicts (file vs dir, non-`.repos` dotfiles, non-dirs) are
//     left untouched.
//
// If, after the pass, legacyRoot is empty, it is removed; otherwise it stays
// so the user can reconcile leftovers.
func migrateWorkspacesRoot(legacyRoot, targetRoot, profile string) error {
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return fmt.Errorf("ensure target root: %w", err)
	}

	entries, err := os.ReadDir(legacyRoot)
	if err != nil {
		return fmt.Errorf("read legacy root: %w", err)
	}

	var firstErr error
	recordErr := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

	for _, e := range entries {
		src := filepath.Join(legacyRoot, e.Name())

		// Special-case the bare-repo cache: relocate to a per-profile path so
		// the unified workspaces root doesn't put two daemons on the same
		// .git/.
		if e.Name() == ".repos" && profile != "" {
			dst := filepath.Join(targetRoot, ".repos-"+profile)
			if _, err := os.Lstat(dst); err == nil {
				// Already migrated — leave legacy in place.
				continue
			} else if !errors.Is(err, fs.ErrNotExist) {
				recordErr(fmt.Errorf("stat target %s: %w", dst, err))
				continue
			}
			if err := os.Rename(src, dst); err != nil {
				recordErr(fmt.Errorf("rename %s -> %s: %w", src, dst, err))
			}
			continue
		}

		dst := filepath.Join(targetRoot, e.Name())
		dstInfo, err := os.Lstat(dst)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Rename(src, dst); err != nil {
				recordErr(fmt.Errorf("rename %s -> %s: %w", src, dst, err))
			}
			continue
		}
		if err != nil {
			recordErr(fmt.Errorf("stat target %s: %w", dst, err))
			continue
		}

		// Only merge into existing workspace_id-shaped dirs (skip dotfiles
		// to avoid stomping shared cache state).
		if !e.IsDir() || !dstInfo.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}

		if err := mergeTaskDirs(src, dst); err != nil {
			recordErr(err)
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

// mergeTaskDirs renames every task_short subdir in legacyWS into targetWS,
// skipping (leaving in legacyWS) any subdir whose name already exists at the
// target. Removes legacyWS afterwards if it ends up empty.
func mergeTaskDirs(legacyWS, targetWS string) error {
	subs, err := os.ReadDir(legacyWS)
	if err != nil {
		return fmt.Errorf("read legacy workspace dir %s: %w", legacyWS, err)
	}
	var firstErr error
	for _, sub := range subs {
		src := filepath.Join(legacyWS, sub.Name())
		dst := filepath.Join(targetWS, sub.Name())
		if _, err := os.Lstat(dst); err == nil {
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
	if remaining, err := os.ReadDir(legacyWS); err == nil && len(remaining) == 0 {
		_ = os.Remove(legacyWS)
	}
	return firstErr
}
