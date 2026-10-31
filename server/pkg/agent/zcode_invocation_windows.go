//go:build windows

package agent

import "log/slog"

// platformZcodeInvocation rewrites zcode.cmd → PowerShell -File zcode.ps1 on
// Windows to avoid cmd.exe %* re-tokenisation of the managed argv. The task
// prompt still travels on argv (ZCode has no stdin prompt mode — see
// buildZcodeArgs); prompts with embedded double quotes hit the same PowerShell
// re-serialisation limitation documented for qwen. rewriteCmdToPS1 is defined
// in cursor_invocation_windows.go.
func platformZcodeInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("zcode", lookedUp, args, logger)
}
