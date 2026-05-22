package execenv

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// localPathRef mirrors the JSON structure stored in project_resource.resource_ref
// when resource_type is "local_path".
type localPathRef struct {
	Path     string `json:"path"`
	DaemonID string `json:"daemon_id"`
}

// symlinkLocalPaths creates symlinks in the agent's workdir for each
// "local_path" project resource whose daemon_id matches this daemon.
// Symlinks are placed at workdir/local/<basename> → the absolute path on disk.
//
// Failures are logged but non-fatal: a missing or inaccessible local path
// should not block agent spawning. The agent will still see the resource
// listed in resources.json and can report a clearer error to the user.
func symlinkLocalPaths(workDir, daemonID string, resources []ProjectResourceForEnv, logger *slog.Logger) {
	if daemonID == "" || len(resources) == 0 {
		return
	}

	localDir := filepath.Join(workDir, "local")
	for _, res := range resources {
		if res.ResourceType != "local_path" {
			continue
		}
		var ref localPathRef
		if err := json.Unmarshal(res.ResourceRef, &ref); err != nil {
			logger.Warn("execenv: skipping malformed local_path resource", "resource_id", res.ID, "error", err)
			continue
		}
		if strings.TrimSpace(ref.DaemonID) != daemonID {
			continue
		}
		ref.Path = strings.TrimSpace(ref.Path)
		if ref.Path == "" {
			continue
		}

		// Verify the target path exists on disk before symlinking.
		if _, err := os.Stat(ref.Path); err != nil {
			logger.Warn("execenv: local_path target does not exist", "path", ref.Path, "error", err)
			continue
		}

		// Ensure local/ directory exists.
		if err := os.MkdirAll(localDir, 0o755); err != nil {
			logger.Warn("execenv: failed to create local dir", "path", localDir, "error", err)
			return
		}

		linkName := filepath.Join(localDir, filepath.Base(ref.Path))

		// Remove stale symlink if it exists.
		_ = os.Remove(linkName)

		if err := os.Symlink(ref.Path, linkName); err != nil {
			logger.Warn("execenv: failed to symlink local_path", "src", ref.Path, "dst", linkName, "error", err)
			continue
		}
		logger.Info("execenv: symlinked local_path", "src", ref.Path, "dst", linkName)
	}
}