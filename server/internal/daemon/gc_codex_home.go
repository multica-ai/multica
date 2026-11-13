package daemon

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/util"
)

func (d *Daemon) localCodexHomeEligible(taskDir string, meta *execenv.GCMeta) bool {
	return meta.LocalDirectory && d.cfg.GCCompletedTaskTTL > 0 &&
		!meta.CompletedAt.IsZero() && time.Since(meta.CompletedAt) > d.cfg.GCCompletedTaskTTL &&
		localCodexHomeTarget(taskDir) != ""
}

func localCodexHomeTarget(taskDir string) string {
	root, err := filepath.Abs(taskDir)
	if err != nil {
		return ""
	}
	home, ok := managedArtifactTarget(root, "codex-home")
	if !ok {
		return ""
	}
	resolvedRoot, err := util.ResolveSymlinks(root)
	if err != nil {
		return ""
	}
	// Current local-directory tasks link their rollouts to a conversation
	// store outside the env root. Older homes can still own the only copy;
	// retain those homes instead of silently losing resume history.
	for _, name := range []string{"sessions", "archived_sessions"} {
		path := filepath.Join(home, name)
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&linkedDirModes == 0 {
			return ""
		}
		target, err := util.ResolveSymlinks(path)
		if errors.Is(err, fs.ErrNotExist) {
			// The independent session-store TTL can leave a dangling link.
			// No transcript remains behind that link for home GC to destroy.
			continue
		}
		if err != nil {
			return ""
		}
		if isPathWithin(resolvedRoot, target) {
			return ""
		}
	}
	return home
}

// cleanLocalCodexHome runs under the same env-root reservation as other GC
// mutations. Re-read completion in case a task finished during the API lookup.
func (d *Daemon) cleanLocalCodexHome(taskDir string, stats *gcStats) {
	meta, err := execenv.ReadGCMeta(taskDir)
	if err != nil || !d.localCodexHomeEligible(taskDir, meta) {
		return
	}
	home := localCodexHomeTarget(taskDir)
	if home == "" {
		return
	}
	size := dirSize(home)
	if err := os.RemoveAll(home); err != nil {
		d.logger.Warn("gc: remove local Codex home failed", "dir", taskDir, "error", err)
		return
	}
	stats.codexHomesReclaimed++
	stats.bytesReclaimed += size
	d.logger.Info("gc: local Codex home removed", "dir", taskDir,
		"reason", "completed task retention expired; daemon logs and output retained",
		"completed_at", meta.CompletedAt.Format(time.RFC3339),
		"completed_task_ttl", d.cfg.GCCompletedTaskTTL, "bytes_reclaimed", size)
}
