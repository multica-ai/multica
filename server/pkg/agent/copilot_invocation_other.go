//go:build !windows

package agent

import "log/slog"

// platformCopilotInvocation is a no-op on non-Windows platforms: Copilot
// CLI's binstub invokes node directly via shebang and Go's os/exec can pass
// argv unchanged.
func platformCopilotInvocation(_ string, _ []string, _ *slog.Logger) (string, []string, bool) {
	return "", nil, false
}
