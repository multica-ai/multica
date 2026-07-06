//go:build !windows

package agent

import (
	"log/slog"
)

// acquireCodexLaunchLock is a no-op on non-Windows platforms.
func acquireCodexLaunchLock(logger *slog.Logger) func() {
	return func() {}
}
