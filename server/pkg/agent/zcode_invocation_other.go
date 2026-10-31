//go:build !windows

package agent

import "log/slog"

// platformZcodeInvocation is a no-op on non-Windows platforms: the community
// zcode binstub invokes node directly via shebang and Go's os/exec can pass
// argv unchanged.
func platformZcodeInvocation(_ string, _ []string, _ *slog.Logger) (string, []string, bool) {
	return "", nil, false
}
