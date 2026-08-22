//go:build windows

package agent

import "log/slog"

// platformQwenInvocation rewrites qwen.cmd → PowerShell -File qwen.ps1 on
// Windows to avoid cmd.exe %* re-tokenisation of the -p prompt and the
// stream-json protocol flags behind it (see #6082). rewriteCmdToPS1 is defined
// in cursor_invocation_windows.go.
func platformQwenInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("qwen", lookedUp, args, logger)
}
