package agent

import "log/slog"

// chooseCursorInvocation selects the actual program (argv[0]) and the full
// argv to spawn a cursor-agent run.
//
// Background:
//   - On macOS/Linux, cursor-agent is a real binary and we can pass argv
//     directly via os/exec — no rewriting needed.
//   - On Windows, the official installer ships cursor-agent.cmd whose body is
//     "powershell ... -File cursor-agent.ps1 %*". CreateProcess for a .cmd
//     file goes through cmd.exe, and %* in a .cmd batch file is expanded by
//     re-tokenising the original command line, which mangles arguments that
//     contain newlines or other whitespace (e.g. multi-line `-p` prompts).
//     To stay on the official launch path while avoiding that re-tokenisation,
//     we resolve cursor-agent.ps1 next to the .cmd and invoke PowerShell with
//     `-File <ps1>` directly, letting Go pass each argv as a separate token.
//
// The Windows-specific behaviour is implemented in
// cursor_invocation_windows.go; on other platforms we fall through to a
// passthrough.
func chooseCursorInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	if argv0, full, ok := platformCursorInvocation(lookedUp, args, logger); ok {
		return argv0, full
	}
	return execName, args
}
