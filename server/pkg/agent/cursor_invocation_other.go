//go:build !windows

package agent

import "log/slog"

// platformCursorInvocation is a no-op on non-Windows platforms: cursor-agent
// is a native binary and Go's os/exec can pass argv unchanged.
func platformCursorInvocation(_ string, _ []string, _ *slog.Logger) (string, []string, bool) {
	return "", nil, false
}
