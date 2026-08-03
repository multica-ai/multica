package execenv

import (
	"fmt"
	"os"
	"path/filepath"
)

// SpotlightExclusionMarker is the macOS marker that prevents indexing of a
// directory tree. Other platforms safely ignore the empty file.
const SpotlightExclusionMarker = ".metadata_never_index"

// EnsureSpotlightExclusion creates or refreshes the marker at the daemon's
// workspaces root. Callers treat failures as non-fatal because indexing is an
// optimization and must never prevent task execution.
func EnsureSpotlightExclusion(workspacesRoot string) error {
	if workspacesRoot == "" {
		return fmt.Errorf("execenv: workspaces root is required")
	}
	if err := os.MkdirAll(workspacesRoot, 0o755); err != nil {
		return fmt.Errorf("create workspaces root for Spotlight marker: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workspacesRoot, SpotlightExclusionMarker), nil, 0o644); err != nil {
		return fmt.Errorf("write Spotlight exclusion marker: %w", err)
	}
	return nil
}
