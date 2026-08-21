package execenv

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// taskTempDirPrefix matches the per-task temp dirs ensureTaskTempDir creates
// under the task temp base (daemon.go). The base is usually a shared /tmp, so
// only entries with this prefix may ever be touched.
const taskTempDirPrefix = "multica-task-"

// PruneTaskTempDirs reclaims orphaned per-task temp dirs (the agent's TMPDIR)
// from the task temp base. Every task gets one, and its only removal path is
// a defer in runTask — a daemon crash or a failed removal on Windows leaves
// the dir forever, unbounded in size.
//
// Liveness comes from the caller's reservation protocol (reserve), not mtime:
// a running task can go tens of minutes without touching the root of its temp
// dir, so an mtime-only rule would delete a live task's TMPDIR. The mtime TTL
// is only the backstop for dirs orphaned by a daemon restart, when the active
// set is gone. retention <= 0 disables the sweep; an unreadable root is a
// no-op. A removal failure is logged and retried on the next cycle.
func PruneTaskTempDirs(root string, retention time.Duration, now time.Time, reserve func(dir string) (commit func(), ok bool), logger *slog.Logger) (removed int, bytesFreed int64) {
	if retention <= 0 {
		return 0, 0
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, 0 // not created yet, or unreadable — nothing to prune
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), taskTempDirPrefix) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		newest, size := codexStoreStat(dir)
		if newest.IsZero() || now.Sub(newest) <= retention {
			continue
		}
		var commit func()
		if reserve != nil {
			c, ok := reserve(dir)
			if !ok {
				continue // a live task holds it
			}
			commit = c
		}
		err := os.RemoveAll(dir)
		if commit != nil {
			commit()
		}
		if err != nil {
			logger.Warn("execenv: prune task temp dir failed", "dir", dir, "error", err)
			continue
		}
		removed++
		bytesFreed += size
	}
	return removed, bytesFreed
}
