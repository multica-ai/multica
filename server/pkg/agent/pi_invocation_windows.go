//go:build windows

package agent

import "log/slog"

// platformPiInvocation rewrites pi.cmd → PowerShell -File pi.ps1 on
// Windows to avoid cmd.exe %* re-tokenisation (see #3306).
// powerShellLookup and rewriteCmdToPS1 are defined in cursor_invocation_windows.go.
func platformPiInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("pi", lookedUp, args, logger)
}
