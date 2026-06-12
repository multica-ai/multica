//go:build !windows

package agent

import "log/slog"

// platformPiInvocation is a no-op on non-Windows platforms: Pi's binstub
// invokes node directly via shebang and Go's os/exec can pass argv
// unchanged.
func platformPiInvocation(_ string, _ []string, _ *slog.Logger) (string, []string, bool) {
	return "", nil, false
}
