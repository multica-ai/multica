package agent

import "log/slog"

// choosePiInvocation selects the actual program (argv[0]) and the full
// argv to spawn a Pi run.
//
// Background:
//   - On macOS/Linux, the npm-installed `pi` binstub is a shebang script
//     that execs node directly with the JS entrypoint, so argv passes
//     through unchanged.
//   - On Windows, the npm installer ships `pi.cmd` whose body is
//     "powershell ... -File pi.ps1 %*". CreateProcess for a .cmd file
//     goes through cmd.exe, and %* in a .cmd batch file is expanded by
//     re-tokenising the original command line, which mangles arguments
//     containing newlines or other whitespace — for Pi, that's the
//     multi-line positional prompt passed by buildPiArgs. Symptom: the
//     Pi session JSONL records only the first line of the prompt
//     (#3306). To stay on the official launch path while avoiding that
//     re-tokenisation, we resolve pi.ps1 next to the .cmd and invoke
//     PowerShell with `-File <ps1>` directly, letting Go pass each argv
//     as a separate token.
//
// The Windows-specific behaviour is implemented in
// pi_invocation_windows.go; on other platforms we fall through to a
// passthrough.
func choosePiInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	if argv0, full, ok := platformPiInvocation(lookedUp, args, logger); ok {
		return argv0, full
	}
	return execName, args
}
