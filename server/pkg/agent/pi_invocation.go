package agent

import (
	"log/slog"
	"strings"
)

// piPowerShellCommandArgs builds the argv for the -Command route, which is the
// only PowerShell route that leaves stdin bytes untouched (#7355).
func piPowerShellCommandArgs(ps1 string, args []string) []string {
	quotedPS1 := strings.ReplaceAll(ps1, "'", "''")
	full := make([]string, 0, 6+len(args))
	full = append(full, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "& '"+quotedPS1+"' @args")
	return append(full, args...)
}

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
//     (#3306). We resolve pi.ps1 next to the .cmd and invoke PowerShell
//     with `-Command & 'pi.ps1' @args`. The prompt moved to stdin in
//     #6457, and -File re-encodes stdin under the console ANSI codepage,
//     destroying every non-ASCII byte (#7355) — so every Pi invocation
//     takes the -Command route, which forwards stdin untouched. The
//     residual #3306 exposure is a multi-line custom option value, which
//     @args still forwards as one token.
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
