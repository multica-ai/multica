package agent

import "log/slog"

// chooseCopilotInvocation selects the actual program (argv[0]) and the full
// argv to spawn a Copilot CLI run.
//
// On macOS/Linux the npm binstub is a shebang script that execs node directly,
// so argv passes through unchanged. On Windows the npm installer ships
// copilot.cmd, which routes through cmd.exe; cmd.exe re-tokenises the raw
// command line via %*, mangling arguments that contain newlines or whitespace
// (e.g. the multi-line -p prompt). To avoid that, we find the sibling
// copilot.ps1 and invoke PowerShell with -File <ps1> directly, so Go passes
// each argument as a discrete token.
//
// The Windows rewrite lives in copilot_invocation_windows.go.
func chooseCopilotInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	if argv0, full, ok := platformCopilotInvocation(lookedUp, args, logger); ok {
		return argv0, full
	}
	return execName, args
}
