//go:build !windows

package agent

import "log/slog"

// platformQwenInvocation is a no-op on non-Windows platforms: Qwen Code's
// binstub invokes node directly via shebang and Go's os/exec can pass argv
// unchanged.
func platformQwenInvocation(_ string, _ []string, _ *slog.Logger) (string, []string, bool) {
	return "", nil, false
}
