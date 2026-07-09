//go:build windows

package agent

import "log/slog"

// platformCopilotInvocation rewrites copilot.cmd → PowerShell -File
// copilot.ps1 on Windows to avoid cmd.exe %* re-tokenisation mangling
// the multi-line -p prompt built by buildCopilotArgs.
// powerShellLookup and rewriteCmdToPS1 are defined in cursor_invocation_windows.go.
func platformCopilotInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("copilot", lookedUp, args, logger)
}
