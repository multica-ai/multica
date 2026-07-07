//go:build !windows

package agent

import (
	"context"
	"log/slog"
)

// acquireCodexLaunchLock is a no-op on non-Windows platforms.
func acquireCodexLaunchLock(ctx context.Context, logger *slog.Logger) (func(), error) {
	return func() {}, nil
}
