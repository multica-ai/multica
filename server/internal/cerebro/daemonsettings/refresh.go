// Package daemonsettings keeps daemon workspace settings fresh without
// expanding upstream-zone daemon patches.
package daemonsettings

import (
	"context"
	"log/slog"
)

// RefreshFunc refreshes a workspace's server-owned daemon settings cache.
type RefreshFunc func(context.Context, string) error

// RefreshExistingWorkspace refreshes settings for a workspace that is already
// tracked by the daemon. Failures are intentionally non-fatal: stale settings
// are preferable to dropping runtime registration recovery for the workspace.
func RefreshExistingWorkspace(ctx context.Context, workspaceID string, refresh RefreshFunc, logger *slog.Logger) {
	if refresh == nil {
		return
	}
	if err := refresh(ctx, workspaceID); err != nil && logger != nil {
		logger.Debug("workspace sync: refresh settings failed", "workspace_id", workspaceID, "error", err)
	}
}
