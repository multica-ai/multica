//go:build !windows

package agent

import "log/slog"

// platformDshInvocation is a no-op off Windows: npm installs dsh as a symlink
// to lib/bin.js with a node shebang, so execve hands argv to node unchanged.
func platformDshInvocation(execPath string, args []string, logger *slog.Logger) (string, []string, bool) {
	return "", nil, false
}
